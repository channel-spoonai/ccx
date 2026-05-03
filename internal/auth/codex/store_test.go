package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// withTempHome은 테스트 동안 XDG_CONFIG_HOME을 임시 경로로 돌려놓는다.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestSaveLoadAuth_RoundTrip(t *testing.T) {
	withTempHome(t)
	want := &StoredAuth{
		AccessToken:  "atk",
		RefreshToken: "rtk",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		AccountID:    "acct_1",
	}
	if err := SaveAuth(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("LoadAuth returned nil")
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || got.AccountID != want.AccountID {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt mismatch: got %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestLoadAuth_MissingFileReturnsNil(t *testing.T) {
	withTempHome(t)
	got, err := LoadAuth()
	if err != nil {
		t.Fatalf("missing 파일에 대해 에러 반환: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestSaveAuth_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows는 unix mode 비트를 정확히 지키지 않음")
	}
	withTempHome(t)
	auth := &StoredAuth{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}
	if err := SaveAuth(auth); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(authPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("토큰 파일 퍼미션: got %o, want 0600", info.Mode().Perm())
	}
	dir := filepath.Dir(authPath())
	dinfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dinfo.Mode().Perm() != 0o700 {
		t.Errorf("auth 디렉토리 퍼미션: got %o, want 0700", dinfo.Mode().Perm())
	}
}

func TestClearAuth_RemovesFile(t *testing.T) {
	withTempHome(t)
	auth := &StoredAuth{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}
	if err := SaveAuth(auth); err != nil {
		t.Fatal(err)
	}
	if err := ClearAuth(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(authPath()); !os.IsNotExist(err) {
		t.Errorf("ClearAuth 후 파일이 여전히 존재: %v", err)
	}
}

func TestClearAuth_IgnoresMissing(t *testing.T) {
	withTempHome(t)
	if err := ClearAuth(); err != nil {
		t.Errorf("missing 파일 삭제가 에러: %v", err)
	}
}

func TestStoredAuth_IsExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"한참 전 만료", now.Add(-time.Hour), true},
		{"마진 안쪽 (3분 후 만료)", now.Add(3 * time.Minute), true},
		{"마진 경계 (정확히 5분 후)", now.Add(5 * time.Minute), true},
		{"마진 바깥 (10분 후)", now.Add(10 * time.Minute), false},
		{"한참 뒤", now.Add(time.Hour), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &StoredAuth{ExpiresAt: c.expiresAt}
			if got := a.IsExpired(now); got != c.want {
				t.Errorf("IsExpired = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFromTokenResponse_DefaultsExpiry(t *testing.T) {
	before := time.Now()
	got := fromTokenResponse(TokenResponse{AccessToken: "a", RefreshToken: "r"})
	// expires_in 누락 → 1시간 가정.
	gap := got.ExpiresAt.Sub(before)
	if gap < 59*time.Minute || gap > 61*time.Minute {
		t.Errorf("기본 만료 시간이 1시간 근처가 아님: %v", gap)
	}
}
