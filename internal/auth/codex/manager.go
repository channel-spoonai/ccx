package codex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNotAuthenticated는 토큰 파일이 없을 때 반환된다.
// 호출자는 이걸 잡고 사용자에게 `ccx codex login`을 안내해야 한다.
var ErrNotAuthenticated = errors.New("Codex OAuth 인증 안됨 — `ccx codex login` 을 먼저 실행하세요")

// Manager는 토큰을 메모리 캐시하고, 만료 임박 시 자동 refresh한다.
// 동시 요청에서 refresh가 중복 실행되지 않도록 single-flight 보장.
type Manager struct {
	mu       sync.Mutex
	cached   *StoredAuth
	inflight chan struct{}
	refErr   error
	refRes   *StoredAuth
}

// NewManager는 빈 매니저를 만든다. 첫 GetAccessToken 호출 때 디스크에서 로드한다.
func NewManager() *Manager { return &Manager{} }

// GetAccessToken은 항상 유효한 access_token을 반환한다.
// 만료 5분 이내면 refresh를 트리거한다. 동시 호출은 한 번만 refresh.
func (m *Manager) GetAccessToken(ctx context.Context) (string, error) {
	auth, err := m.ensureFresh(ctx)
	if err != nil {
		return "", err
	}
	return auth.AccessToken, nil
}

// Snapshot은 현재 유효한 토큰 전체를 반환한다 (account_id 포함).
func (m *Manager) Snapshot(ctx context.Context) (*StoredAuth, error) {
	return m.ensureFresh(ctx)
}

// PersistInitial은 login 직후 받은 토큰을 디스크에 저장하고 캐시한다.
func (m *Manager) PersistInitial(t TokenResponse) error {
	auth := fromTokenResponse(t)
	if err := SaveAuth(auth); err != nil {
		return err
	}
	m.mu.Lock()
	m.cached = auth
	m.mu.Unlock()
	return nil
}

func (m *Manager) ensureFresh(ctx context.Context) (*StoredAuth, error) {
	m.mu.Lock()
	if m.cached == nil {
		stored, err := LoadAuth()
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		if stored == nil {
			m.mu.Unlock()
			return nil, ErrNotAuthenticated
		}
		m.cached = stored
	}
	if !m.cached.IsExpired(time.Now()) {
		auth := m.cached
		m.mu.Unlock()
		return auth, nil
	}
	// 만료 임박 — refresh single-flight.
	if m.inflight != nil {
		ch := m.inflight
		m.mu.Unlock()
		<-ch
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.refErr != nil {
			return nil, m.refErr
		}
		return m.refRes, nil
	}
	m.inflight = make(chan struct{})
	current := m.cached
	ch := m.inflight
	m.mu.Unlock()

	res, err := m.doRefresh(ctx, current)

	m.mu.Lock()
	m.refErr = err
	m.refRes = res
	close(ch)
	m.inflight = nil
	if err == nil {
		m.cached = res
	}
	auth := m.cached
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return auth, nil
}

func (m *Manager) doRefresh(ctx context.Context, current *StoredAuth) (*StoredAuth, error) {
	tok, err := RefreshTokens(ctx, current.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("토큰 refresh 실패: %w", err)
	}
	next := fromTokenResponse(tok)
	// refresh_token이 비어있는 응답이면 기존 것 유지 (raine과 동일).
	if next.RefreshToken == "" {
		next.RefreshToken = current.RefreshToken
	}
	if next.AccountID == "" {
		next.AccountID = current.AccountID
	}
	if err := SaveAuth(next); err != nil {
		return nil, fmt.Errorf("refresh된 토큰 저장 실패: %w", err)
	}
	return next, nil
}

// ResetCache는 외부에서 강제로 캐시를 비울 때(예: logout 직후) 호출.
func (m *Manager) ResetCache() {
	m.mu.Lock()
	m.cached = nil
	m.mu.Unlock()
}

// ForceRefresh는 만료 마진과 무관하게 즉시 토큰을 refresh한다.
// 401 응답을 받았을 때처럼 캐시된 토큰이 실제론 무효임을 외부에서 알았을 때 사용.
func (m *Manager) ForceRefresh(ctx context.Context) (*StoredAuth, error) {
	m.mu.Lock()
	if m.cached == nil {
		stored, err := LoadAuth()
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		if stored == nil {
			m.mu.Unlock()
			return nil, ErrNotAuthenticated
		}
		m.cached = stored
	}
	if m.inflight != nil {
		ch := m.inflight
		m.mu.Unlock()
		<-ch
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.refErr != nil {
			return nil, m.refErr
		}
		return m.refRes, nil
	}
	m.inflight = make(chan struct{})
	current := m.cached
	ch := m.inflight
	m.mu.Unlock()

	res, err := m.doRefresh(ctx, current)

	m.mu.Lock()
	m.refErr = err
	m.refRes = res
	close(ch)
	m.inflight = nil
	if err == nil {
		m.cached = res
	}
	auth := m.cached
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return auth, nil
}
