// Package tlsconf는 프로파일의 업스트림 TLS 설정(caCertFile / insecureTLS)을 http.Transport로 만든다.
//
// 자가서명 HTTPS 백엔드(vast.ai 인스턴스 등)용이다. ccx는 Go 바이너리라 NODE_TLS_REJECT_UNAUTHORIZED나
// NODE_EXTRA_CA_CERTS가 프록시 데몬에 닿지 않고, Windows·macOS에서는 SSL_CERT_FILE도 읽지 않는다.
package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Transport는 caFile/insecure를 반영한 업스트림 Transport를 만든다. 둘 다 비어 있으면 nil —
// 호출자는 기본 Transport를 그대로 쓴다.
//
// caFile은 그 파일의 인증서**만** 신뢰한다(시스템 루트와 합치지 않는다) — 프로파일 하나의
// 업스트림을 고정하는 용도라 신뢰 범위를 넓힐 이유가 없다. CA 없이 leaf만 넣어도 된다:
// Go 검증기는 루트 풀에 든 인증서를 체인 구성 없이 그대로 신뢰하고(crypto/x509 verify.go
// `opts.Roots.contains(c)`), 호스트명(IP SAN 포함)과 유효기간 검사는 그대로 한다. vast.ai는
// leaf만 보내 체인을 완성할 CA를 엔드포인트에서 얻을 수 없다(2026-10 실측).
//
// Windows에서 leaf를 OS 저장소에 넣는 우회가 실패하는 것도 이 차이다 — 시스템 풀은 CryptoAPI가
// 검증하고, CryptoAPI는 leaf→CA 체인을 요구한다. 여기서는 커스텀 풀이라 Go 검증기를 탄다.
func Transport(caFile string, insecure bool) (*http.Transport, error) {
	if caFile != "" && insecure {
		return nil, errors.New("caCertFile and insecureTLS are mutually exclusive — use one")
	}
	if caFile == "" && !insecure {
		return nil, nil
	}
	cfg := &tls.Config{}
	if insecure {
		cfg.InsecureSkipVerify = true
	} else {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read caCertFile: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("caCertFile %s has no PEM certificate", caFile)
		}
		cfg.RootCAs = pool
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = cfg
	return t, nil
}

// ResolvePath는 caCertFile 값을 절대경로로 바꾼다. "~/"는 홈 디렉터리로 펼친다.
// 상대경로는 거부한다 — 데몬과 런치 위치(cwd)가 매번 달라 해석 기준이 모호하다.
func ResolvePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand ~ in caCertFile: %w", err)
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("caCertFile must be an absolute path (or start with ~/): %s", p)
	}
	return filepath.Clean(p), nil
}
