package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
)

// NVIDIABaseURL은 NVIDIA NIM(build.nvidia.com)의 호스팅 추론 엔드포인트.
// Anthropic /v1/messages가 없고 OpenAI 호환 /v1/chat/completions만 있어
// auth: "openai-chat" 변환 프록시 경로로 라우팅한다.
const NVIDIABaseURL = "https://integrate.api.nvidia.com/v1"

type NVIDIAModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type NVIDIAResult struct {
	Models []NVIDIAModel
	URL    string
	Err    error
}

// FetchNVIDIAModels는 NIM의 OpenAI 호환 /v1/models를 조회한다. 응답 필드는
// id/owned_by뿐이라 컨텍스트·가격·무료 여부는 알 수 없다 (컨텍스트는 ctxwin
// 카탈로그 담당). NVIDIA는 이 목록을 무인증으로도 공개하므로 token은 선택.
// baseURL이 비면 NVIDIABaseURL을 쓰고, 있으면 LM Studio와 같은 방식으로
// trailing slash와 /v1 suffix를 정규화한다.
func FetchNVIDIAModels(baseURL, token string) NVIDIAResult {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = NVIDIABaseURL
	}
	base = regexp.MustCompile(`/v1$`).ReplaceAllString(base, "")
	url := base + "/v1/models"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return NVIDIAResult{URL: url, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return NVIDIAResult{URL: url, Err: err}
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		return NVIDIAResult{URL: url, Err: fmt.Errorf("HTTP %d %s", res.StatusCode, res.Status)}
	}

	var body struct {
		Data []NVIDIAModel `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return NVIDIAResult{URL: url, Err: err}
	}

	models := make([]NVIDIAModel, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			models = append(models, m)
		}
	}
	return NVIDIAResult{Models: models, URL: url}
}

// RecommendedNVIDIAModels는 NIM 프로파일에 노출할 모델 allowlist. NIM 카탈로그에는
// 100개가 넘는 모델이 올라와 있지만 대부분은 Claude Code의 에이전트 워크로드
// (툴 콜링 + 긴 컨텍스트)를 감당하지 못하고, 임베딩·리랭커·가드레일처럼 아예
// 대화형이 아닌 것도 섞여 있다. 그래서 서버 목록을 이 집합과 교차시켜 보여준다.
// 순서가 곧 메뉴 표시 순서다. 신규 모델은 여기 추가 후 릴리즈하면 자동
// 업데이트로 전파된다.
var RecommendedNVIDIAModels = []string{
	"z-ai/glm-5.2",
	"minimaxai/minimax-m3",
	"nvidia/nemotron-3-ultra-550b-a55b",
	"nvidia/nemotron-3-super-120b-a12b",
	"deepseek-ai/deepseek-v4-flash",
	"deepseek-ai/deepseek-v4-pro",
}

// nvidiaContextWindows는 NIM이 **실제로 서빙하는** 컨텍스트 한도. 2026-07에
// 한도 초과 요청의 에러 메시지("This model's maximum context length is N tokens")로
// 직접 측정한 값이며, 모델 공식 스펙과 다르다 — NIM은 모델 카드의 최대치가 아니라
// 자체 `--max-model-len` 설정으로 배포하기 때문. 예: GLM-5.2는 z.ai 직결에서 1M
// 이지만 NIM에서는 202,752, DeepSeek V4 Pro는 1M이 아니라 262,144다.
//
// 프로바이더마다 값이 다르므로 프로바이더 무관 테이블인 ctxwin.catalog로는
// 표현할 수 없다. 그래서 등록 시 이 수치를 모델 ID에 suffix로 박제한다
// (OpenRouter/LM Studio와 같은 방식).
var nvidiaContextWindows = map[string]int{
	"z-ai/glm-5.2":                      202_752,
	"minimaxai/minimax-m3":              524_288,
	"nvidia/nemotron-3-ultra-550b-a55b": 1_000_000,
	"nvidia/nemotron-3-super-120b-a12b": 1_000_000,
	"deepseek-ai/deepseek-v4-flash":     1_000_000,
	"deepseek-ai/deepseek-v4-pro":       262_144,
}

// NVIDIAContextWindow는 실측 컨텍스트를 돌려준다. 미상이면 0 —
// 호출부는 suffix 없이 두고 Claude Code 기본 추정에 맡긴다.
func NVIDIAContextWindow(id string) int {
	return nvidiaContextWindows[strings.ToLower(strings.TrimSpace(id))]
}

// IsRecommended는 모델 ID가 allowlist에 있는지 본다 (NIM ID는 소문자지만
// 비교는 대소문자 무시).
func IsRecommended(id string) bool {
	for _, r := range RecommendedNVIDIAModels {
		if strings.EqualFold(id, r) {
			return true
		}
	}
	return false
}

// FilterRecommended는 서버가 실제로 서빙하는 모델과 allowlist의 교집합을
// allowlist 순서로 돌려준다. 교집합이 비면 빈 슬라이스 — 호출부(flows)가 수동
// 입력으로 폴백한다.
func FilterRecommended(models []NVIDIAModel) []NVIDIAModel {
	available := make(map[string]NVIDIAModel, len(models))
	for _, m := range models {
		available[strings.ToLower(m.ID)] = m
	}
	out := make([]NVIDIAModel, 0, len(RecommendedNVIDIAModels))
	for _, want := range RecommendedNVIDIAModels {
		if m, ok := available[strings.ToLower(want)]; ok {
			out = append(out, m)
		}
	}
	return out
}

// IsNVIDIA matches by profile name or baseUrl host.
func IsNVIDIA(p *config.Profile) bool {
	if p == nil {
		return false
	}
	if regexp.MustCompile(`(?i)nvidia`).MatchString(p.Name) {
		return true
	}
	return regexp.MustCompile(`(?i)nvidia\.com`).MatchString(p.BaseURL)
}
