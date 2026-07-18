package codex

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/channel-spoonai/ccx/internal/procutil"
)

// DaemonOptions는 RunDaemon의 입력.
type DaemonOptions struct {
	// ParentPID가 0보다 크면 그 프로세스가 사라지는 즉시 데몬도 종료한다.
	// 보통 ccx 부모가 자기 PID를 넘긴다. unix: ccx → claude로 syscall.Exec할 때 PID가
	// 보존되므로 이 polling이 정확히 claude의 종료를 잡는다. Windows: exec 등가물이 없어
	// 부모=ccx이고 ccx가 cmd.Run()으로 claude를 대기하므로 claude와의 결합은 간접적 —
	// ccx만 비정상 종료하면 살아있는 claude의 프록시가 소멸하는 알려진 갭이 있다.
	ParentPID int

	// SharedSecret은 부모가 만든 랜덤 시크릿을 자식이 받아 서버 인증에 쓰는 값.
	SharedSecret string

	// Upstream은 forwarding 대상. zero value면 ChatGPT OAuth 모드.
	Upstream UpstreamConfig

	// IdleTimeout은 마지막 요청 후 이 시간 동안 추가 요청이 없으면 종료. 0이면 비활성.
	IdleTimeout time.Duration

	// ReadyWriter는 ready 메시지를 보낼 출력. 보통 os.Stdout. nil이면 출력 생략.
	ReadyWriter interface {
		Write([]byte) (int, error)
	}
}

// RunDaemon은 자식 프로세스에서 호출되는 진입점.
// 127.0.0.1:0 으로 listen → "ready PORT\n" 을 ReadyWriter에 출력 → 종료 신호 대기.
//
// 종료 트리거:
//   - SIGINT/SIGTERM
//   - ParentPID 프로세스 사망
//   - IdleTimeout 경과 (옵션)
func RunDaemon(opts DaemonOptions) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("proxy listen failed: %w", err)
	}

	srv, err := Start(ServerOptions{
		Listener:     listener,
		SharedSecret: opts.SharedSecret,
		Upstream:     opts.Upstream,
		IdleTimeout:  opts.IdleTimeout,
	})
	if err != nil {
		_ = listener.Close()
		return err
	}

	if opts.ReadyWriter != nil {
		// Phase 5에서 부모가 이 한 줄을 파싱해 ANTHROPIC_BASE_URL을 세팅한다.
		// 형식: "ready <port>\n"
		_, _ = fmt.Fprintf(opts.ReadyWriter, "ready %d\n", srv.Port())
	}

	// 종료 신호 wiring.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if opts.ParentPID > 0 {
		go watchParent(ctx, opts.ParentPID, cancel)
	}

	select {
	case <-ctx.Done():
	case <-sigCh:
	case <-srv.Done():
		// 서버가 자체 종료(idle timeout 등). 추가 Shutdown은 멱등이라 안전.
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}

// watchParent는 ppid를 polling하며 살아있는지 본다. 죽으면 cancel().
// procutil.Watcher가 시작 시점에 프로세스 핸들을 보유해(Windows) PID 재사용 오판을 막는다.
// unix는 signal 0 판정.
func watchParent(ctx context.Context, ppid int, cancel context.CancelFunc) {
	w := procutil.NewWatcher(ppid)
	defer w.Close()
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !w.Alive() {
				cancel()
				return
			}
		}
	}
}
