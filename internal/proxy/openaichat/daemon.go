package openaichat

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

type DaemonOptions struct {
	ParentPID       int
	SharedSecret    string
	UpstreamBaseURL string
	UpstreamAuth    string
	UpstreamAPIKey  string
	EnableThinking  *bool
	IdleTimeout     time.Duration
	ReadyWriter     interface {
		Write([]byte) (int, error)
	}
}

// RunDaemon은 자식 프로세스에서 호출되는 진입점.
// 127.0.0.1:0 listen → "ready PORT\n" 출력 → 종료 신호 대기.
func RunDaemon(opts DaemonOptions) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("proxy listen failed: %w", err)
	}

	srv, err := Start(ServerOptions{
		Listener:        listener,
		SharedSecret:    opts.SharedSecret,
		UpstreamBaseURL: opts.UpstreamBaseURL,
		UpstreamAuth:    opts.UpstreamAuth,
		UpstreamAPIKey:  opts.UpstreamAPIKey,
		EnableThinking:  opts.EnableThinking,
		IdleTimeout:     opts.IdleTimeout,
	})
	if err != nil {
		_ = listener.Close()
		return err
	}

	if opts.ReadyWriter != nil {
		_, _ = fmt.Fprintf(opts.ReadyWriter, "ready %d\n", srv.Port())
	}

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
