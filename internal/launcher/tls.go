package launcher

import (
	"fmt"
	"net/http"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/tlsconf"
)

// upstreamTLS는 프로파일의 caCertFile / insecureTLS를 해석한 결과. 데몬에는 CAFile/Insecure를
// env로 넘기고, 부모가 런치 전에 직접 하는 업스트림 요청(/v1/models 프로브)은 transport를 쓴다.
type upstreamTLS struct {
	CAFile    string // 절대경로 ("" = 지정 안 함)
	Insecure  bool
	transport *http.Transport // nil이면 기본 Transport
}

// resolveUpstreamTLS는 경로를 펼치고 Transport를 미리 만들어 본다 — 파일이 없거나 PEM이 아니면
// 데몬을 띄우기 전에 부모에서 분명한 에러로 멈춘다(데몬에서 실패하면 "ready" 대기 타임아웃으로만 보인다).
func resolveUpstreamTLS(p *config.Profile) (upstreamTLS, error) {
	caFile, err := tlsconf.ResolvePath(ResolveSecret(p.CACertFile))
	if err != nil {
		return upstreamTLS{}, err
	}
	tr, err := tlsconf.Transport(caFile, p.InsecureTLS)
	if err != nil {
		return upstreamTLS{}, err
	}
	return upstreamTLS{CAFile: caFile, Insecure: p.InsecureTLS, transport: tr}, nil
}

// client는 이 TLS 설정을 쓰는 단발성 HTTP 클라이언트.
func (u upstreamTLS) client(timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if u.transport != nil {
		c.Transport = u.transport
	}
	return c
}

func printUpstreamTLS(u upstreamTLS) {
	switch {
	case u.Insecure:
		fmt.Printf("\x1B[33m[ccx]\x1B[0m TLS: upstream certificate verification OFF (insecureTLS) — prefer caCertFile\n")
	case u.CAFile != "":
		fmt.Printf("\x1B[36m[ccx]\x1B[0m TLS: upstream trusts only %s\n", u.CAFile)
	}
}

// warnUpstreamTLSIgnored는 프록시가 없는 auth 경로에 TLS 옵션이 있으면 무시된다고 알린다.
// 직결은 Claude Code(Node)가 업스트림에 연결하므로 ccx가 TLS를 정할 자리가 없다.
func warnUpstreamTLSIgnored(p *config.Profile) {
	if p.CACertFile == "" && !p.InsecureTLS {
		return
	}
	hint := ""
	if p.Auth == "" {
		hint = ` — for a direct profile set env NODE_EXTRA_CA_CERTS, or use auth: "anthropic"`
	}
	fmt.Printf("\x1B[33m[ccx]\x1B[0m TLS: caCertFile/insecureTLS ignored (only auth: \"anthropic\" / \"openai-chat\" use them)%s\n", hint)
}
