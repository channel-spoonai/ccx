package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// --- 테스트 훅 헬퍼 ---

func withResolveSelf(t *testing.T, path string) {
	t.Helper()
	prev := resolveSelf
	resolveSelf = func() (string, error) { return path, nil }
	t.Cleanup(func() { resolveSelf = prev })
}

func withTerminal(t *testing.T, val bool) {
	t.Helper()
	prev := isTerminal
	isTerminal = func() bool { return val }
	t.Cleanup(func() { isTerminal = prev })
}

func withAPIBase(t *testing.T, url string) {
	t.Helper()
	prev := apiBase
	apiBase = url
	t.Cleanup(func() { apiBase = prev })
}

// fakeSelfBinary는 임시 디렉터리에 가짜 "현재 바이너리"를 만들고 resolveSelf를 연결한다.
func fakeSelfBinary(t *testing.T, content []byte) string {
	t.Helper()
	name := "ccx"
	if runtime.GOOS == "windows" {
		name = "ccx.exe"
	}
	target := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(target, content, 0o755); err != nil {
		t.Fatal(err)
	}
	withResolveSelf(t, target)
	return target
}

// makeTestArchive는 현재 GOOS의 릴리즈 아카이브 형식(tar.gz / zip)으로
// ccx 바이너리 payload를 담은 아카이브를 만든다.
func makeTestArchive(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		for _, f := range []struct {
			name string
			body []byte
		}{
			{"README.md", []byte("noise")},
			{"ccx.exe", payload},
		} {
			w, err := zw.Create(f.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(f.body); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"README.md", []byte("noise")},
		{"ccx", payload},
	} {
		hdr := &tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// newAutoUpdateServer는 릴리즈 JSON과 아카이브 자산을 함께 서빙한다.
func newAutoUpdateServer(t *testing.T, tag string, archive []byte) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			rel := Release{
				TagName:     tag,
				PublishedAt: time.Now(),
				Assets: []Asset{{
					Name:        assetName(StripV(tag), runtime.GOOS, runtime.GOARCH),
					DownloadURL: srv.URL + "/asset",
				}},
			}
			_ = json.NewEncoder(w).Encode(rel)
		case r.URL.Path == "/asset":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- 게이팅 ---

func TestAutoUpdateDisabledEnv(t *testing.T) {
	cases := []struct {
		val  string
		want bool // disabled?
	}{
		{"", false},
		{"1", false},
		{"true", false},
		{"anything", false},
		{"0", true},
		{"false", true},
		{"FALSE", true},
		{"off", true},
		{"OFF", true},
	}
	for _, c := range cases {
		t.Setenv(AutoUpdateEnv, c.val)
		if got := autoUpdateDisabled(); got != c.want {
			t.Errorf("autoUpdateDisabled(%q) = %v, want %v", c.val, got, c.want)
		}
	}
}

func TestShouldAutoApply(t *testing.T) {
	const tag = "v9.9.9"

	setup := func(t *testing.T) {
		withTempCacheDir(t)
		withTerminal(t, true)
		fakeSelfBinary(t, []byte("old"))
		// 개발자 머신/CI에 CCX_AUTO_UPDATE=0이 설정돼 있어도 테스트가 hermetic하도록 중립화
		t.Setenv(AutoUpdateEnv, "")
	}

	t.Run("normal", func(t *testing.T) {
		setup(t)
		ok, reason := shouldAutoApply("1.0.0", tag)
		if !ok {
			t.Errorf("shouldAutoApply = false (%s), want true", reason)
		}
	})

	t.Run("dev build", func(t *testing.T) {
		setup(t)
		if ok, _ := shouldAutoApply("dev", tag); ok {
			t.Error("dev build should not auto-apply")
		}
	})

	t.Run("env opt-out", func(t *testing.T) {
		setup(t)
		t.Setenv(AutoUpdateEnv, "0")
		if ok, _ := shouldAutoApply("1.0.0", tag); ok {
			t.Error("opt-out env should not auto-apply")
		}
	})

	t.Run("non-TTY", func(t *testing.T) {
		setup(t)
		withTerminal(t, false)
		if ok, _ := shouldAutoApply("1.0.0", tag); ok {
			t.Error("non-TTY should not auto-apply")
		}
	})

	t.Run("failed marker", func(t *testing.T) {
		setup(t)
		if err := SaveCache(CacheEntry{CheckedAt: time.Now(), LatestTag: tag, AutoUpdateFailedTag: tag}); err != nil {
			t.Fatal(err)
		}
		if ok, _ := shouldAutoApply("1.0.0", tag); ok {
			t.Error("failed marker for same tag should not auto-apply")
		}
		// 다른 태그의 마커는 막지 않음
		if err := SaveCache(CacheEntry{CheckedAt: time.Now(), LatestTag: tag, AutoUpdateFailedTag: "v0.0.9"}); err != nil {
			t.Fatal(err)
		}
		if ok, reason := shouldAutoApply("1.0.0", tag); !ok {
			t.Errorf("marker for different tag should not block: %s", reason)
		}
	})

	t.Run("unwritable dir", func(t *testing.T) {
		setup(t)
		withResolveSelf(t, filepath.Join(t.TempDir(), "no-such-dir", "ccx"))
		if ok, _ := shouldAutoApply("1.0.0", tag); ok {
			t.Error("unwritable target dir should not auto-apply")
		}
	})
}

// --- TryAutoUpdate ---

func TestTryAutoUpdateSuccess(t *testing.T) {
	withTempCacheDir(t)
	withTerminal(t, true)
	t.Setenv(AutoUpdateEnv, "")
	target := fakeSelfBinary(t, []byte("old-binary"))

	payload := []byte("new-binary-payload")
	srv := newAutoUpdateServer(t, "v9.9.9", makeTestArchive(t, payload))
	withAPIBase(t, srv.URL)

	if err := SaveCache(CacheEntry{CheckedAt: time.Now(), LatestTag: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if !TryAutoUpdate(context.Background(), "1.0.0", "v9.9.9", &out) {
		t.Fatalf("TryAutoUpdate = false, want true. output:\n%s", out.String())
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("binary not replaced: got %q, want %q", got, payload)
	}
	if c := LoadCache(); c != nil {
		t.Errorf("cache not invalidated after success: %+v", c)
	}
	if !strings.Contains(out.String(), "takes effect from the next run") {
		t.Errorf("missing success message. output:\n%s", out.String())
	}
}

func TestTryAutoUpdateFetchError(t *testing.T) {
	withTempCacheDir(t)
	withTerminal(t, true)
	t.Setenv(AutoUpdateEnv, "")
	fakeSelfBinary(t, []byte("old"))

	srv := newTestServer(t, Release{}, 500)
	withAPIBase(t, srv.URL)

	var out bytes.Buffer
	if TryAutoUpdate(context.Background(), "1.0.0", "v9.9.9", &out) {
		t.Fatal("TryAutoUpdate = true on API error, want false")
	}
	if !strings.Contains(out.String(), "auto-update failed") {
		t.Errorf("missing failure message. output:\n%s", out.String())
	}
	c := LoadCache()
	if c == nil || c.AutoUpdateFailedTag != "v9.9.9" {
		t.Errorf("failure marker not recorded: %+v", c)
	}
	// 마커가 기록된 뒤엔 같은 태그 재시도가 게이팅에서 걸림
	if ok, _ := shouldAutoApply("1.0.0", "v9.9.9"); ok {
		t.Error("retry for failed tag should be gated off")
	}
}

func TestTryAutoUpdateAlreadyLatest(t *testing.T) {
	withTempCacheDir(t)
	withTerminal(t, true)
	t.Setenv(AutoUpdateEnv, "")
	oldContent := []byte("current-binary")
	target := fakeSelfBinary(t, oldContent)

	// 캐시는 새 버전(v1.0.1)이라 주장하지만 API는 현재와 같은 v1.0.0 반환 — 낡은 캐시 시나리오.
	srv := newTestServer(t, Release{TagName: "v1.0.0"}, 200)
	withAPIBase(t, srv.URL)
	if err := SaveCache(CacheEntry{CheckedAt: time.Now(), LatestTag: "v1.0.1"}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if !TryAutoUpdate(context.Background(), "1.0.0", "v1.0.1", &out) {
		t.Fatal("TryAutoUpdate = false, want true (stale cache should be treated as resolved)")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, oldContent) {
		t.Error("binary must not be touched when already latest")
	}
	if c := LoadCache(); c != nil {
		t.Errorf("stale cache not invalidated: %+v", c)
	}
}

func TestApplyReturnsAppliedFlag(t *testing.T) {
	withTempCacheDir(t)
	fakeSelfBinary(t, []byte("current"))
	srv := newTestServer(t, Release{TagName: "v1.0.0"}, 200)
	withAPIBase(t, srv.URL)

	// no-op 경로는 낡은 캐시를 무효화해야 한다 (수동 `ccx update`도 이 경로를 공유)
	if err := SaveCache(CacheEntry{CheckedAt: time.Now(), LatestTag: "v1.0.1"}); err != nil {
		t.Fatal(err)
	}

	applied, err := Apply(context.Background(), "1.0.0", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied {
		t.Error("Apply no-op(already latest) returned applied=true, want false")
	}
	if c := LoadCache(); c != nil {
		t.Errorf("no-op should invalidate stale cache: %+v", c)
	}
}

// --- WaitBackgroundFetch ---

func TestWaitBackgroundFetch(t *testing.T) {
	prev := bgDone
	t.Cleanup(func() { bgDone = prev })

	// fetch 미시작 — 즉시 반환
	bgDone = nil
	start := time.Now()
	WaitBackgroundFetch(time.Second)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("nil bgDone should return immediately, took %v", elapsed)
	}

	// 완료 대기 — close되면 반환
	done := make(chan struct{})
	bgDone = done
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()
	start = time.Now()
	WaitBackgroundFetch(5 * time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("should return shortly after close, took %v", elapsed)
	}

	// 타임아웃 — 영영 close 안 되면 max에서 반환
	bgDone = make(chan struct{})
	start = time.Now()
	WaitBackgroundFetch(50 * time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("should time out at max, took %v", elapsed)
	}
}

// --- 잔재 청소 ---

func TestCleanupStaleTemps(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)

	files := map[string]time.Time{
		"ccx-dl-stale":  old,
		"ccx-new-stale": old,
		"ccx-dl-fresh":  time.Now(),
		"unrelated.txt": old,
	}
	for name, mtime := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	cleanupStaleTemps(dir)

	for name, wantGone := range map[string]bool{
		"ccx-dl-stale":  true,
		"ccx-new-stale": true,
		"ccx-dl-fresh":  false,
		"unrelated.txt": false,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		gone := os.IsNotExist(err)
		if gone != wantGone {
			t.Errorf("%s: gone=%v, want %v", name, gone, wantGone)
		}
	}
}

func TestAtomicReplaceWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only behavior")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "ccx.exe")
	newFile := filepath.Join(dir, "ccx-new-test")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := atomicReplace(target, newFile); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("target = %q, want %q", got, "new")
	}
	// .old는 PID로 유니크화된 이름
	wantOld := target + ".old-" + strconv.Itoa(os.Getpid())
	if _, err := os.Stat(wantOld); err != nil {
		t.Errorf("unique .old missing: %v", err)
	}
}

func TestCleanupStaleBinaryWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only behavior")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "ccx.exe")
	if err := os.WriteFile(target, []byte("cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	withResolveSelf(t, target)

	// 레거시 .old + 유니크 .old-<pid> 둘 다 청소 대상
	for _, name := range []string{"ccx.exe.old", "ccx.exe.old-12345"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	CleanupStaleBinary()

	for _, name := range []string{"ccx.exe.old", "ccx.exe.old-12345"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s not cleaned up", name)
		}
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("current binary must survive cleanup: %v", err)
	}
}
