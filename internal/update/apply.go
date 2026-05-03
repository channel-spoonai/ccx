package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Apply는 최신 릴리즈로 현재 바이너리를 교체한다.
//
// 흐름: dev 빌드 거부 → self path 해석 → API 호출 → 동일 버전이면 no-op → 다운로드 →
// 추출 → atomic replace → 캐시 무효화. 진행 상황은 out으로 라인 출력.
func Apply(ctx context.Context, current string, out io.Writer) error {
	if IsDevBuild(current) {
		return errors.New("dev 빌드는 자동 업데이트를 지원하지 않습니다. install.sh를 다시 실행하세요")
	}

	target, err := resolveSelfPath()
	if err != nil {
		return fmt.Errorf("실행 파일 경로 확인 실패: %w", err)
	}

	rel, err := FetchLatest(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "[ccx] 현재 버전: v%s\n", StripV(current))
	fmt.Fprintf(out, "[ccx] 최신 버전: %s (%s 릴리즈)\n", rel.TagName, rel.PublishedAt.Format("2006-01-02"))

	if Compare(rel.TagName, current) <= 0 {
		fmt.Fprintf(out, "[ccx] 이미 최신 버전입니다 (%s).\n", rel.TagName)
		return nil
	}

	url, err := rel.AssetForCurrent()
	if err != nil {
		return fmt.Errorf("%w (OS=%s, arch=%s)", err, runtime.GOOS, runtime.GOARCH)
	}
	fmt.Fprintf(out, "[ccx] 다운로드 중: %s\n", filepath.Base(url))

	dir := filepath.Dir(target)
	// 같은 디렉터리(=같은 파일시스템)에 임시 파일을 만들어야 os.Rename이 atomic.
	if err := checkWritable(dir); err != nil {
		return err
	}

	archive, err := os.CreateTemp(dir, "ccx-dl-*")
	if err != nil {
		return fmt.Errorf("임시 파일 생성 실패: %w", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)

	if err := Download(ctx, url, archive); err != nil {
		archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}

	newBin, err := os.CreateTemp(dir, "ccx-new-*")
	if err != nil {
		return fmt.Errorf("임시 바이너리 생성 실패: %w", err)
	}
	newBinPath := newBin.Name()
	defer os.Remove(newBinPath)

	if err := extractCCX(archivePath, newBin); err != nil {
		newBin.Close()
		return err
	}
	if err := newBin.Close(); err != nil {
		return err
	}

	if err := os.Chmod(newBinPath, 0o755); err != nil {
		return fmt.Errorf("권한 설정 실패: %w", err)
	}
	if runtime.GOOS == "darwin" {
		// Gatekeeper quarantine 제거 (실패 무시 — xattr 없는 환경 대응).
		_ = exec.CommandContext(ctx, "xattr", "-d", "com.apple.quarantine", newBinPath).Run()
	}

	fmt.Fprintf(out, "[ccx] %s 교체\n", target)
	if err := atomicReplace(target, newBinPath); err != nil {
		return fmt.Errorf("바이너리 교체 실패: %w", err)
	}

	// 다음 실행에서 새 버전이 알림 없이 바로 보이도록.
	_ = InvalidateCache()

	fmt.Fprintf(out, "[ccx] 완료. 새 버전: %s\n", rel.TagName)
	return nil
}

// resolveSelfPath는 os.Executable()에 EvalSymlinks를 적용한 실제 바이너리 경로.
// ~/.local/bin/ccx 가 symlink일 때 진짜 파일을 찾기 위함.
func resolveSelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// symlink 해석 실패해도 원본 경로로 진행
		return exe, nil
	}
	return resolved, nil
}

// checkWritable은 디렉터리 쓰기 가능 여부를 확인한다 (atomic rename 가능 여부 사전 점검).
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, "ccx-write-test-*")
	if err != nil {
		return fmt.Errorf("%s 디렉터리에 쓸 권한이 없습니다 (sudo로 재실행 필요): %w", dir, err)
	}
	probe.Close()
	os.Remove(probe.Name())
	return nil
}

// extractCCX는 tar.gz/zip 아카이브에서 ccx 또는 ccx.exe 바이너리만 추출해 dst에 쓴다.
func extractCCX(archivePath string, dst io.Writer) error {
	if runtime.GOOS == "windows" {
		return extractFromZip(archivePath, dst)
	}
	return extractFromTarGz(archivePath, dst)
}

func extractFromTarGz(archivePath string, dst io.Writer) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip 디코딩 실패: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("아카이브에서 ccx 바이너리를 찾을 수 없습니다")
		}
		if err != nil {
			return fmt.Errorf("tar 읽기 실패: %w", err)
		}
		if filepath.Base(hdr.Name) != "ccx" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		if _, err := io.Copy(dst, tr); err != nil {
			return fmt.Errorf("바이너리 추출 실패: %w", err)
		}
		return nil
	}
}

func extractFromZip(archivePath string, dst io.Writer) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("zip 열기 실패: %w", err)
	}
	defer zr.Close()

	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != "ccx.exe" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("바이너리 추출 실패: %w", err)
		}
		return nil
	}
	return errors.New("아카이브에서 ccx.exe를 찾을 수 없습니다")
}
