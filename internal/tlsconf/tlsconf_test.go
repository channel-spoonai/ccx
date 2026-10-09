package tlsconf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// vastLike는 vast.ai처럼 별도 CA가 서명한 leaf **한 장만** 내보내는 TLS 서버를 띄운다.
// 돌려주는 PEM 경로: CA, leaf.
func vastLike(t *testing.T, leafIP string) (srv *httptest.Server, caPath, leafPath string) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Jupyter CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "instance.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP(leafIP)},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	write := func(name string, der []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return srv, write("ca.pem", caDER), write("leaf.pem", leafDER)
}

func get(t *testing.T, tr *http.Transport, url string) error {
	t.Helper()
	c := &http.Client{Timeout: 5 * time.Second}
	if tr != nil {
		c.Transport = tr
	}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func TestTransportSelfSignedUpstream(t *testing.T) {
	srv, caPath, leafPath := vastLike(t, "127.0.0.1")

	if err := get(t, nil, srv.URL); err == nil {
		t.Fatal("기본 Transport가 자가서명 업스트림을 통과시키면 안 됨")
	}

	for name, path := range map[string]string{"CA": caPath, "leaf만": leafPath} {
		tr, err := Transport(path, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := get(t, tr, srv.URL); err != nil {
			t.Fatalf("%s로 고정하면 붙어야 하는데 %v", name, err)
		}
	}

	tr, err := Transport("", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := get(t, tr, srv.URL); err != nil {
		t.Fatalf("insecureTLS면 붙어야 하는데 %v", err)
	}
}

// leaf를 고정해도 호스트(IP SAN) 검사는 남는다 — 다른 주소용 인증서를 내미는 서버는 거부한다.
func TestTransportPinStillChecksHost(t *testing.T) {
	srv, _, leafPath := vastLike(t, "10.9.8.7")
	tr, err := Transport(leafPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := get(t, tr, srv.URL); err == nil {
		t.Fatal("IP SAN이 다른 leaf는 고정해도 거부돼야 함")
	}
}

// 고정한 인증서와 다른 인증서를 내미는 서버는 거부한다 — 시스템 루트와 합치지 않는다.
func TestTransportPinRejectsOtherCert(t *testing.T) {
	srv, _, _ := vastLike(t, "127.0.0.1")
	_, _, otherLeaf := vastLike(t, "127.0.0.1")
	tr, err := Transport(otherLeaf, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := get(t, tr, srv.URL); err == nil {
		t.Fatal("다른 인증서로 고정했는데 붙으면 안 됨")
	}
}

func TestTransportOptions(t *testing.T) {
	if tr, err := Transport("", false); tr != nil || err != nil {
		t.Fatalf("설정 없음은 nil이어야 함: %v %v", tr, err)
	}
	if _, err := Transport("/x.pem", true); err == nil {
		t.Fatal("둘 다 지정하면 에러여야 함")
	}
	if _, err := Transport(filepath.Join(t.TempDir(), "missing.pem"), false); err == nil {
		t.Fatal("없는 파일은 에러여야 함")
	}
	junk := filepath.Join(t.TempDir(), "junk.pem")
	_ = os.WriteFile(junk, []byte("not a cert"), 0o600)
	if _, err := Transport(junk, false); err == nil {
		t.Fatal("PEM이 아닌 파일은 에러여야 함")
	}
}

func TestResolvePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	abs := filepath.Join(t.TempDir(), "a.pem")
	for in, want := range map[string]string{
		"":              "",
		abs:             abs,
		"~/certs/a.pem": filepath.Join(home, "certs", "a.pem"),
	} {
		got, err := ResolvePath(in)
		if err != nil || got != want {
			t.Errorf("ResolvePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ResolvePath("certs/a.pem"); err == nil {
		t.Error("상대경로는 거부해야 함")
	}
}
