package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
)

const (
	// 릴리즈 owner/repo. install.sh와 .goreleaser.yaml과 동일.
	repoOwner = "channel-spoonai"
	repoName  = "ccx"

	// GitHub API 호스트는 테스트에서 httptest 서버로 교체할 수 있도록 변수.
	defaultAPIBase = "https://api.github.com"
)

// apiBase는 단위 테스트에서 httptest.URL로 덮어쓰기 위해 변수.
// 운영 코드에서는 바꾸지 않는다.
var apiBase = defaultAPIBase

// httpClient는 GitHub API/다운로드용 공유 클라이언트.
// codex/oauth.go의 패턴과 동일 (Timeout 30s).
var httpClient = &http.Client{Timeout: 30 * time.Second}

// Asset은 GitHub Release Asset의 부분 집합.
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

// Release는 GitHub Releases API 응답의 부분 집합.
type Release struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Version은 "v0.4.0" → "0.4.0"으로 노멀라이즈된 버전 문자열.
func (r *Release) Version() string { return StripV(r.TagName) }

// FetchLatest는 GitHub Releases API에서 최신 stable 릴리즈를 가져온다.
// goreleaser의 prerelease: auto 설정 덕분에 /releases/latest는 stable만 반환.
func FetchLatest(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBase, repoOwner, repoName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ccx-update-check")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub API 응답 오류 (%d): %s", resp.StatusCode, string(body))
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("릴리즈 응답 파싱 실패: %w", err)
	}
	if rel.TagName == "" {
		return nil, errors.New("릴리즈 응답에 tag_name이 비어있습니다")
	}
	return &rel, nil
}

// AssetFor는 OS/arch에 맞는 다운로드 URL을 반환한다.
// 명명 규칙은 .goreleaser.yaml: ccx-{ver}-{os}-{arch}.tar.gz (windows는 .zip).
func (r *Release) AssetFor(goos, goarch string) (string, error) {
	want := assetName(r.Version(), goos, goarch)
	for _, a := range r.Assets {
		if a.Name == want {
			return a.DownloadURL, nil
		}
	}
	return "", fmt.Errorf("아카이브를 찾을 수 없습니다: %s", want)
}

func assetName(ver, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("ccx-%s-%s-%s.%s", ver, goos, goarch, ext)
}

// AssetForCurrent는 현재 실행 중인 바이너리의 OS/arch에 맞는 URL을 반환.
func (r *Release) AssetForCurrent() (string, error) {
	return r.AssetFor(runtime.GOOS, runtime.GOARCH)
}

// Download는 url에서 데이터를 받아 w로 복사한다. 다운로드 자체는 시간이 걸릴 수 있어
// httpClient의 Timeout(30s)을 우회하는 별도 클라이언트(no timeout, ctx로 제어)를 사용.
func Download(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ccx-update")

	// 다운로드는 분 단위가 걸릴 수 있어 client는 timeout 없이, ctx deadline에 의존.
	dl := &http.Client{Timeout: 0}
	resp, err := dl.Do(req)
	if err != nil {
		return fmt.Errorf("다운로드 실패: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("다운로드 응답 오류 (%d) — %s", resp.StatusCode, url)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("다운로드 스트림 오류: %w", err)
	}
	return nil
}
