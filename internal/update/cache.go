package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	CacheFilename = "update-check.json"
	// CacheTTL: 24시간에 한 번만 GitHub API 호출.
	CacheTTL = 24 * time.Hour
)

// CacheEntry는 마지막 업데이트 체크 결과를 저장한다. 24시간 이내면 신뢰.
type CacheEntry struct {
	CheckedAt time.Time `json:"checked_at"`
	LatestTag string    `json:"latest_tag,omitempty"`
	LatestURL string    `json:"latest_url,omitempty"`
}

// CachePath는 ~/.config/ccx/update-check.json 위치를 반환한다.
// XDG_CONFIG_HOME 우선 — internal/config의 canonicalPath와 동일 패턴.
func CachePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ccx", CacheFilename)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", CacheFilename)
	}
	return filepath.Join(home, ".config", "ccx", CacheFilename)
}

// LoadCache는 캐시 파일을 읽어 반환한다. 파일이 없으면 (nil, nil).
// 파싱 실패는 손상으로 보고 nil 반환 — 다음 SaveCache가 덮어쓴다.
func LoadCache() *CacheEntry {
	path := CachePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var e CacheEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil
	}
	return &e
}

// SaveCache는 캐시를 atomic하게 쓴다 (tmp + rename).
func SaveCache(e CacheEntry) error {
	path := CachePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	tmp, err := os.CreateTemp(dir, CacheFilename+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// 실패 시 잔재 청소
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Fresh는 CheckedAt이 TTL 이내인지 판정.
func (e *CacheEntry) Fresh(now time.Time) bool {
	if e == nil {
		return false
	}
	return now.Sub(e.CheckedAt) < CacheTTL
}

// InvalidateCache는 캐시 파일을 삭제한다 (ccx update 성공 후 호출).
func InvalidateCache() error {
	err := os.Remove(CachePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
