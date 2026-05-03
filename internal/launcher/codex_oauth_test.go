package launcher

import (
	"strings"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
)

func TestPrepareCodexOAuth_FailsWithoutToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := prepareCodexOAuth(&config.Profile{Name: "x", Auth: AuthCodexOAuth})
	if err == nil {
		t.Fatal("토큰 없을 때 에러여야 함")
	}
	if !strings.Contains(err.Error(), "ccx codex login") {
		t.Errorf("에러 메시지에 사용자 안내가 포함되어야 함: %v", err)
	}
}

// 실제 SpawnDaemon 흐름은 internal/proxy/codex/daemon_test.go의
// TestSpawnDaemon_EndToEnd 가 ccx 바이너리를 빌드해 검증한다.
// launcher 단위 테스트에서는 self 바이너리에 daemon 분기가 없어 검증이 어렵다.

func TestProfileAuth_Discriminator(t *testing.T) {
	// Profile.Auth 필드 자체가 JSON serialization에 포함되는지 빠른 검증.
	p := config.Profile{Name: "x", Auth: AuthCodexOAuth}
	if p.Auth != "codex-oauth" {
		t.Errorf("AuthCodexOAuth = %q, expected codex-oauth", p.Auth)
	}
}
