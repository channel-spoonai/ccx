package launcher

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			got := anthropicFixUsage(p, upstreamTLS{})
			if got.Enabled != c.want {
				t.Fatalf("Enabled = %v, want %v (%+v)", got.Enabled, c.want, got)
			}
			if got.Enabled && got.Reason == "" {
				t.Fatal("켜졌는데 근거가 비어 있음")
			}
		})
	}
}

// 자가서명 HTTPS 업스트림에서도 프로브가 프로파일 TLS 설정을 따라야 한다 — 기본 Transport로
// 보내면 인증서 오류로 조용히 실패해 mlx-serve 보정이 꺼진다.
func TestAnthropicFixUsageSelfSignedUpstream(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m","owned_by":"mlx-serve"}]}`))
	}))
	t.Cleanup(srv.Close)
	certPath := filepath.Join(t.TempDir(), "upstream.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		p    config.Profile
		want bool
	}{
		"TLS 설정 없음은 실패": {config.Profile{BaseURL: srv.URL}, false},
		"caCertFile":    {config.Profile{BaseURL: srv.URL, CACertFile: certPath}, true},
		"insecureTLS":   {config.Profile{BaseURL: srv.URL, InsecureTLS: true}, true},
	} {
		tlsOpt, err := resolveUpstreamTLS(&c.p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := anthropicFixUsage(&c.p, tlsOpt); got.Enabled != c.want {
			t.Errorf("%s: Enabled = %v, want %v", name, got.Enabled, c.want)
		}
	}
}

func TestResolveUpstreamTLSRejectsBadConfig(t *testing.T) {
	for name, p := range map[string]config.Profile{
		"상대경로":   {CACertFile: "certs/a.pem"},
		"없는 파일":  {CACertFile: filepath.Join(t.TempDir(), "missing.pem")},
		"둘 다 지정": {CACertFile: "/x.pem", InsecureTLS: true},
	} {
		if _, err := resolveUpstreamTLS(&p); err == nil {
			t.Errorf("%s: 에러여야 함", name)
		}
	}
}
