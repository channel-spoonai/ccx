package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, rel Release, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(rel)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchLatest(t *testing.T) {
	rel := Release{
		TagName: "v0.4.0",
		HTMLURL: "https://github.com/channel-spoonai/ccx/releases/tag/v0.4.0",
		Assets: []Asset{
			{Name: "ccx-0.4.0-darwin-arm64.tar.gz", DownloadURL: "https://example.com/darwin-arm64.tgz"},
			{Name: "ccx-0.4.0-linux-amd64.tar.gz", DownloadURL: "https://example.com/linux-amd64.tgz"},
			{Name: "ccx-0.4.0-windows-amd64.zip", DownloadURL: "https://example.com/windows-amd64.zip"},
		},
	}
	srv := newTestServer(t, rel, 200)
	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = prev })

	got, err := FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if got.TagName != "v0.4.0" {
		t.Errorf("TagName = %q, want v0.4.0", got.TagName)
	}
	if got.Version() != "0.4.0" {
		t.Errorf("Version() = %q, want 0.4.0", got.Version())
	}
}

func TestFetchLatestErrorStatus(t *testing.T) {
	srv := newTestServer(t, Release{}, 500)
	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = prev })

	if _, err := FetchLatest(context.Background()); err == nil {
		t.Error("expected error on 500, got nil")
	}
}

func TestAssetFor(t *testing.T) {
	r := &Release{
		TagName: "v0.4.0",
		Assets: []Asset{
			{Name: "ccx-0.4.0-darwin-arm64.tar.gz", DownloadURL: "url-darwin-arm64"},
			{Name: "ccx-0.4.0-linux-amd64.tar.gz", DownloadURL: "url-linux-amd64"},
			{Name: "ccx-0.4.0-windows-amd64.zip", DownloadURL: "url-windows-amd64"},
		},
	}
	cases := []struct {
		goos, goarch, want string
		wantErr            bool
	}{
		{"darwin", "arm64", "url-darwin-arm64", false},
		{"linux", "amd64", "url-linux-amd64", false},
		{"windows", "amd64", "url-windows-amd64", false},
		{"darwin", "ppc64", "", true},   // 없는 arch
		{"freebsd", "amd64", "", true}, // 없는 OS
	}
	for _, c := range cases {
		got, err := r.AssetFor(c.goos, c.goarch)
		if c.wantErr {
			if err == nil {
				t.Errorf("AssetFor(%s,%s) expected error, got %q", c.goos, c.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("AssetFor(%s,%s): %v", c.goos, c.goarch, err)
		}
		if got != c.want {
			t.Errorf("AssetFor(%s,%s) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}
