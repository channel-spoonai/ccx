package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StoredAuth는 디스크에 저장되는 OAuth 토큰.
// CGO_ENABLED=0 빌드 제약으로 keychain은 사용하지 않고 파일에 저장한다 (mode 0600).
type StoredAuth struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	AccountID    string    `json:"account_id,omitempty"`
}

// authPath는 토큰 저장 경로를 반환한다. internal/config의 canonicalPath와 동일한
// 디렉토리 결정 로직을 따라 사용자 헷갈림을 줄인다.
func authPath() string {
	base := configBase()
	return filepath.Join(base, "ccx", "auth", "codex.json")
}

func configBase() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}

// AuthPath는 외부에서 표시 목적으로 경로를 알 수 있게 노출한다.
func AuthPath() string { return authPath() }

// LoadAuth는 저장된 토큰을 읽는다. 파일이 없으면 (nil, nil)을 돌려준다.
func LoadAuth() (*StoredAuth, error) {
	path := authPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("토큰 파일 읽기 실패: %w", err)
	}
	var auth StoredAuth
	if err := json.Unmarshal(raw, &auth); err != nil {
		return nil, fmt.Errorf("토큰 파일 파싱 실패: %w", err)
	}
	return &auth, nil
}

// SaveAuth는 토큰을 디스크에 atomic하게 쓴다 (tmp → rename).
func SaveAuth(auth *StoredAuth) error {
	path := authPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %w", err)
	}
	// 기존 디렉토리가 더 느슨한 퍼미션이면 타이트닝 (config.go 패턴 따름).
	_ = os.Chmod(dir, 0o700)

	buf, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("토큰 파일 쓰기 실패: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("토큰 파일 rename 실패: %w", err)
	}
	return nil
}

// ClearAuth는 토큰 파일을 삭제한다 (logout). 파일이 없어도 오류 아님.
func ClearAuth() error {
	if err := os.Remove(authPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// fromTokenResponse는 OAuth 응답을 디스크 형식으로 변환한다.
func fromTokenResponse(t TokenResponse) *StoredAuth {
	expiresIn := time.Duration(t.ExpiresIn) * time.Second
	if expiresIn == 0 {
		expiresIn = time.Hour // expires_in 누락 시 raine과 동일하게 1시간 가정
	}
	return &StoredAuth{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    time.Now().Add(expiresIn),
		AccountID:    ExtractAccountID(t),
	}
}

// IsExpired는 만료 마진을 적용해 갱신이 필요한지 판단한다.
// `expires - margin <= now` 이면 만료로 친다(경계 포함). raine과 동일한 보수적 판단.
func (a *StoredAuth) IsExpired(now time.Time) bool {
	return !a.ExpiresAt.After(now.Add(RefreshMargin))
}
