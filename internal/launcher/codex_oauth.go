package launcher

import (
	"errors"
	"fmt"
	"time"

	codexauth "github.com/channel-spoonai/ccx/internal/auth/codex"
	"github.com/channel-spoonai/ccx/internal/config"
	proxy "github.com/channel-spoonai/ccx/internal/proxy/codex"
)

// AuthCodexOAuth는 Profile.Auth 필드가 가질 값. config 패키지가 모르는 상수로 두는 대신
// launcher가 단일 진실의 출처가 된다 — 다른 인증 방식이 추가되면 여기 같이 늘어남.
const AuthCodexOAuth = "codex-oauth"

// prepareCodexOAuth는 codex-oauth 프로파일을 위해 백그라운드 프록시를 띄우고
// BaseURL/AuthToken을 자동 주입한 profile copy를 돌려준다.
//
// 토큰이 없으면 사용자에게 `ccx codex login`을 안내하는 명확한 에러를 반환.
// 비대화형(`-xSet`) 환경에서는 자동 OAuth 인터랙티브를 띄우지 않는다 —
// 사용자가 한 번 명시적으로 login 한 뒤 그 이후 호출은 무인 자동화될 수 있어야 하므로.
func prepareCodexOAuth(p *config.Profile) (*config.Profile, error) {
	// 1) 토큰 존재 확인. expires가 만료됐어도 refresh_token이 살아있으면 자동 refresh되므로
	//    여기서는 단순히 파일 존재만 본다.
	stored, err := codexauth.LoadAuth()
	if err != nil {
		return nil, fmt.Errorf("Codex 토큰 파일 읽기 실패: %w", err)
	}
	if stored == nil {
		return nil, errors.New("Codex OAuth 인증이 필요합니다 — `ccx codex login` 을 먼저 실행하세요")
	}

	// 2) 자식 프록시 데몬 spawn. 부모가 syscall.Exec(claude)로 사라져도 자식은 ppid polling으로
	//    claude 종료를 감지해 자체 종료한다.
	sd, err := proxy.SpawnDaemon(5 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("Codex 프록시 spawn 실패: %w", err)
	}

	// 3) profile copy: BaseURL/AuthToken 만 자동 주입하고 나머지는 그대로 둔다.
	//    apiKey가 설정되어 있으면 의도와 충돌하므로 강제로 비운다.
	p2 := *p
	p2.BaseURL = sd.Address()
	p2.AuthToken = sd.SharedSecret
	p2.APIKey = ""
	return &p2, nil
}

// banner 정보 출력용 — Launch에서 호출 후 syscall.Exec 직전.
func printCodexOAuthBanner(addr, accountID string) {
	fmt.Printf("\x1B[36m[ccx]\x1B[0m Codex OAuth 프록시: %s\n", addr)
	if accountID != "" {
		fmt.Printf("\x1B[36m[ccx]\x1B[0m ChatGPT 계정: %s\n", accountID)
	}
}
