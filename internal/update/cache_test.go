package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempCacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func TestSaveLoadCache(t *testing.T) {
	withTempCacheDir(t)

	if got := LoadCache(); got != nil {
		t.Fatalf("LoadCache empty dir = %+v, want nil", got)
	}

	now := time.Date(2026, 5, 4, 12, 30, 0, 0, time.UTC)
	want := CacheEntry{
		CheckedAt: now,
		LatestTag: "v0.4.0",
		LatestURL: "https://github.com/channel-spoonai/ccx/releases/tag/v0.4.0",
	}
	if err := SaveCache(want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	got := LoadCache()
	if got == nil {
		t.Fatal("LoadCache after Save returned nil")
	}
	if !got.CheckedAt.Equal(want.CheckedAt) || got.LatestTag != want.LatestTag || got.LatestURL != want.LatestURL {
		t.Errorf("LoadCache = %+v, want %+v", got, want)
	}

	// 퍼미션 0600 확인 (Windows는 mode가 다르게 보고됨 — Unix만 검증)
	info, err := os.Stat(CachePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("cache file mode = %o, want 0600 (other bits not set)", info.Mode().Perm())
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	withTempCacheDir(t)
	path := CachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadCache(); got != nil {
		t.Errorf("LoadCache corrupt = %+v, want nil", got)
	}
}

func TestFresh(t *testing.T) {
	now := time.Now()
	cases := []struct {
		entry *CacheEntry
		want  bool
	}{
		{nil, false},
		{&CacheEntry{CheckedAt: now}, true},
		{&CacheEntry{CheckedAt: now.Add(-1 * time.Hour)}, true},
		{&CacheEntry{CheckedAt: now.Add(-23 * time.Hour)}, true},
		{&CacheEntry{CheckedAt: now.Add(-25 * time.Hour)}, false},
	}
	for _, c := range cases {
		if got := c.entry.Fresh(now); got != c.want {
			t.Errorf("Fresh(%v) = %v, want %v", c.entry, got, c.want)
		}
	}
}

func TestInvalidateCache(t *testing.T) {
	withTempCacheDir(t)
	// 없을 때도 에러 없음
	if err := InvalidateCache(); err != nil {
		t.Errorf("InvalidateCache empty: %v", err)
	}
	if err := SaveCache(CacheEntry{CheckedAt: time.Now(), LatestTag: "v0.1.0"}); err != nil {
		t.Fatal(err)
	}
	if err := InvalidateCache(); err != nil {
		t.Errorf("InvalidateCache: %v", err)
	}
	if got := LoadCache(); got != nil {
		t.Errorf("after InvalidateCache LoadCache = %+v, want nil", got)
	}
}
