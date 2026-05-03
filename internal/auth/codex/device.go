package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// deviceInitResponse는 OpenAI의 /api/accounts/deviceauth/usercode 응답.
type deviceInitResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	// interval은 문자열로 내려옴(raine 코드 참조).
	Interval string `json:"interval"`
}

// devicePollResponse는 인증이 완료됐을 때 받는 임시 코드.
// 이걸로 다시 /oauth/token에 authorization_code grant를 한 번 더 호출해야 함.
type devicePollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// DevicePrompter는 디바이스 코드 흐름의 안내문 출력 콜백.
type DevicePrompter func(verificationURL, userCode string)

// RunDeviceLogin은 RFC 8628과 유사한 OpenAI 디바이스 인증 흐름을 수행한다.
// 헤드리스/SSH 환경에서 사용. 사용자가 다른 기기 브라우저로 코드를 입력해야 한다.
func RunDeviceLogin(ctx context.Context, prompt DevicePrompter) (TokenResponse, error) {
	init, err := initDevice(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	intervalSec, _ := strconv.Atoi(init.Interval)
	if intervalSec < 1 {
		intervalSec = 5
	}
	// raine과 동일한 안전 마진 3초 추가.
	pollInterval := time.Duration(intervalSec)*time.Second + 3*time.Second

	verificationURL := Issuer + "/codex/device"
	if prompt != nil {
		prompt(verificationURL, init.UserCode)
	} else {
		fmt.Printf("\n브라우저에서 %s 를 열고 코드를 입력하세요: %s\n\n", verificationURL, init.UserCode)
	}

	for {
		select {
		case <-ctx.Done():
			return TokenResponse{}, ctx.Err()
		default:
		}

		poll, status, err := pollDevice(ctx, init.DeviceAuthID, init.UserCode)
		if err != nil {
			return TokenResponse{}, err
		}
		if status == http.StatusOK {
			return finishDeviceExchange(ctx, poll)
		}
		// 403/404는 "아직 인증 안됨" — 계속 폴링.
		// 그 외 상태는 즉시 실패.
		if status != http.StatusForbidden && status != http.StatusNotFound {
			return TokenResponse{}, fmt.Errorf("디바이스 폴링 실패: status %d", status)
		}
		select {
		case <-ctx.Done():
			return TokenResponse{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func initDevice(ctx context.Context) (*deviceInitResponse, error) {
	body := strings.NewReader(`{"client_id":"` + ClientID + `"}`)
	req, err := http.NewRequestWithContext(ctx, "POST", Issuer+"/api/accounts/deviceauth/usercode", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("디바이스 초기화 요청 실패: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("디바이스 초기화 실패 (%d): %s", resp.StatusCode, string(raw))
	}
	var init deviceInitResponse
	if err := json.Unmarshal(raw, &init); err != nil {
		return nil, fmt.Errorf("디바이스 초기화 응답 파싱 실패: %w", err)
	}
	if init.DeviceAuthID == "" || init.UserCode == "" {
		return nil, fmt.Errorf("디바이스 초기화 응답이 비어있음")
	}
	return &init, nil
}

func pollDevice(ctx context.Context, deviceAuthID, userCode string) (*devicePollResponse, int, error) {
	payload, _ := json.Marshal(map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", Issuer+"/api/accounts/deviceauth/token", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("디바이스 폴링 요청 실패: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var poll devicePollResponse
	if err := json.Unmarshal(raw, &poll); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("디바이스 폴링 응답 파싱 실패: %w", err)
	}
	return &poll, resp.StatusCode, nil
}

func finishDeviceExchange(ctx context.Context, poll *devicePollResponse) (TokenResponse, error) {
	// device flow는 OpenAI가 결정하는 고정 redirect_uri를 그대로 사용 — 우리가 listen하는 게 아님.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {poll.AuthorizationCode},
		"redirect_uri":  {Issuer + "/deviceauth/callback"},
		"client_id":     {ClientID},
		"code_verifier": {poll.CodeVerifier},
	}
	return postTokenForm(ctx, form)
}
