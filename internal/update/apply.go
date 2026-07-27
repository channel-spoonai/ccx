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
	"strings"
	"time"
)

// Apply는 최신 릴리즈로 현재 바이너리를 교체한다.
//
// 흐름: dev 빌드 거부 → self path 해석 → API 호출 → 동일 버전이면 no-op → 다운로드 →
// 추출 → atomic replace → 캐시 무효화. 진행 상황은 out으로 라인 출력.
// applied는 실제 교체가 일어났을 때만 true — 이미 최신이라 건너뛴 no-op은 (false, nil).
func Apply(ctx context.Context, current string, out io.Writer) (applied bool, err error) {
	if IsDevBuild(current) {
		return false, errors.New("dev builds do not support auto-update. Please run install.sh again")
	}

	target, err := resolveSelf()
	if err != nil {
		return false, fmt.Errorf("failed to resolve executable path: %w", err)
	}

	rel, err := FetchLatest(ctx)
	if err != nil {
		return false, err
	}

	fmt.Fprintf(out, "[ccx] Current version: v%s\n", StripV(current))
	fmt.Fprintf(out, "[ccx] Latest version: %s (released %s)\n", rel.TagName, rel.PublishedAt.Format("2006-01-02"))

	if Compare(rel.TagName, current) <= 0 {
		fmt.Fprintf(out, "[ccx] Already on the latest version (%s).\n", rel.TagName)
		// 캐시가 더 새 버전을 주장하고 있었다면(릴리즈 롤백 등 낡은 캐시) 여기서 해소 —
		// 남겨두면 캐시 TTL까지 허위 알림이 반복된다.
		_ = InvalidateCache()
		return false, nil
	}

	url, err := rel.AssetForCurrent()
	if err != nil {
		return false, fmt.Errorf("%w (OS=%s, arch=%s)", err, runtime.GOOS, runtime.GOARCH)
	}
	fmt.Fprintf(out, "[ccx] Downloading: %s\n", filepath.Base(url))

	dir := filepath.Dir(target)
	// 같은 디렉터리(=같은 파일시스템)에 임시 파일을 만들어야 os.Rename이 atomic.
	if err := checkWritable(dir); err != nil {
		return false, err
	}

	archive, err := os.CreateTemp(dir, "ccx-dl-*")
	if err != nil {
		return false, fmt.Errorf("failed to create temp file: %w", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)

	if err := Download(ctx, url, archive); err != nil {
		archive.Close()
		return false, err
	}
	if err := archive.Close(); err != nil {
		return false, err
	}

	newBin, err := os.CreateTemp(dir, "ccx-new-*")
	if err != nil {
		return false, fmt.Errorf("failed to create temp binary: %w", err)
	}
	newBinPath := newBin.Name()
	defer os.Remove(newBinPath)

	if err := extractCCX(archivePath, newBin); err != nil {
		newBin.Close()
		return false, err
	}
	if err := newBin.Close(); err != nil {
		return false, err
	}

	if err := os.Chmod(newBinPath, 0o755); err != nil {
		return false, fmt.Errorf("failed to set permissions: %w", err)
	}
	if runtime.GOOS == "darwin" {
		// Gatekeeper quarantine 제거 (실패 무시 — xattr 없는 환경 대응).
		_ = exec.CommandContext(ctx, "xattr", "-d", "com.apple.quarantine", newBinPath).Run()
	}

	fmt.Fprintf(out, "[ccx] Replacing %s\n", target)
	if err := atomicReplace(target, newBinPath); err != nil {
		return false, fmt.Errorf("failed to replace binary: %w", err)
	}

	// 다음 실행에서 새 버전이 알림 없이 바로 보이도록.
	_ = InvalidateCache()

	fmt.Fprintf(out, "[ccx] Done. New version: %s\n", rel.TagName)
	return true, nil
}

// resolveSelf는 테스트에서 가짜 바이너리 경로로 오버라이드하기 위한 훅 (apiBase 패턴).
var resolveSelf = resolveSelfPath

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

// cleanupStaleTemps는 dir에 남은 중단된 업데이트 잔재(ccx-dl-*, ccx-new-*)를
// best-effort로 지운다. Ctrl-C 중단 시 defer가 실행되지 않아 쌓일 수 있다.
// 동시에 실행 중인 다른 인스턴스의 진행 중 다운로드를 지우지 않도록
// mtime이 1시간을 넘긴 파일만 대상.
func cleanupStaleTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "ccx-dl-") && !strings.HasPrefix(name, "ccx-new-") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// checkWritable은 디렉터리 쓰기 가능 여부를 확인한다 (atomic rename 가능 여부 사전 점검).
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, "ccx-write-test-*")
	if err != nil {
		return fmt.Errorf("no write permission for directory %s (re-run with sudo): %w", dir, err)
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
		return fmt.Errorf("gzip decoding failed: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("ccx binary not found in archive")
		}
		if err != nil {
			return fmt.Errorf("tar read failed: %w", err)
		}
		if filepath.Base(hdr.Name) != "ccx" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		if _, err := io.Copy(dst, tr); err != nil {
			return fmt.Errorf("failed to extract binary: %w", err)
		}
		return nil
	}
}

func extractFromZip(archivePath string, dst io.Writer) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
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
			return fmt.Errorf("failed to extract binary: %w", err)
		}
		return nil
	}
	return errors.New("ccx.exe not found in archive")
}
