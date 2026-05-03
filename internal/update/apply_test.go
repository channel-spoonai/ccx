package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFromTarGz(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "ccx.tar.gz")
	wantBin := []byte("fake-ccx-binary-payload")

	// 아카이브 생성: README.md(첫번째, 무관 파일) + ccx(타겟)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"README.md", []byte("noise")},
		{"ccx", wantBin},
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
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "ccx-out")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := extractFromTarGz(archivePath, f); err != nil {
		t.Fatalf("extractFromTarGz: %v", err)
	}
	f.Close()

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantBin) {
		t.Errorf("extracted = %q, want %q", got, wantBin)
	}
}

func TestExtractFromTarGzMissingBinary(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "noccx.tar.gz")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "README.md", Mode: 0o644, Size: 5, Typeflag: tar.TypeReg}
	tw.WriteHeader(hdr)
	tw.Write([]byte("hello"))
	tw.Close()
	gz.Close()
	os.WriteFile(archivePath, buf.Bytes(), 0o644)

	if err := extractFromTarGz(archivePath, &bytes.Buffer{}); err == nil {
		t.Error("expected error when ccx binary missing, got nil")
	}
}
