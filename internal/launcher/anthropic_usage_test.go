package launcher

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
	proxy "github.com/channel-spoonai/ccx/internal/proxy/anthropic"
)

func modelsServer(t *testing.T, ownedBy string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer tok" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m","owned_by":"` + ownedBy + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnthropicFixUsage(t *testing.T) {
	mlx := modelsServer(t, "mlx-serve").URL
	other := modelsServer(t, "lmstudio").URL
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	cases := []struct {
		name    string
		baseURL string
		env     map[string]string
		want    bool
	}{
		{"mlx-serve 자동 감지", mlx, nil, true},
		{"baseUrl이 /v1로 끝나도 감지", mlx + "/v1", nil, true},
		{"다른 서버는 끈 채로", other, nil, false},
		{"서버가 꺼져 있으면 끈 채로", down.URL, nil, false},
		{"profile.env true가 이긴다", other, map[string]string{proxy.CCXUsageIncludesCacheEnv: "true"}, true},
		{"profile.env false로 자동 감지를 끈다", mlx, map[string]string{proxy.CCXUsageIncludesCacheEnv: "false"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &config.Profile{BaseURL: c.baseURL, AuthToken: "tok", Env: c.env}
			got := anthropicFixUsage(p)
			if got.Enabled != c.want {
				t.Fatalf("Enabled = %v, want %v (%+v)", got.Enabled, c.want, got)
			}
			if got.Enabled && got.Reason == "" {
				t.Fatal("켜졌는데 근거가 비어 있음")
			}
		})
	}
}
