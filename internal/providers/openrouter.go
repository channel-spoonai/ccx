package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
)

type OpenRouterModel struct {
	ID            string  `json:"id"`
	ContextLength int     `json:"context_length"`
	Pricing       Pricing `json:"pricing"`
	TopProvider   struct {
		ContextLength int `json:"context_length"`
	} `json:"top_provider"`
}

// EffectiveContext는 모델 레벨 컨텍스트와 라우팅 1순위 프로바이더의 실서빙
// 한도 중 작은 값. OpenRouter는 요청별로 서빙 프로바이더가 달라질 수 있어
// 보수적으로 잡아야 컨텍스트 오버플로가 없다.
func EffectiveContext(m OpenRouterModel) int {
	c := m.ContextLength
	if t := m.TopProvider.ContextLength; t > 0 && (c == 0 || t < c) {
		c = t
	}
	return c
}

type Pricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type OpenRouterResult struct {
	Models []OpenRouterModel
	Err    error
}

// FetchOpenRouterModels returns the public model catalog. Token is optional
// but eases rate-limits. Timeout is 10s because the list is sizeable.
func FetchOpenRouterModels(token string) OpenRouterResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return OpenRouterResult{Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return OpenRouterResult{Err: err}
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		return OpenRouterResult{Err: fmt.Errorf("HTTP %d %s", res.StatusCode, res.Status)}
	}

	var body struct {
		Data []OpenRouterModel `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return OpenRouterResult{Err: err}
	}
	return OpenRouterResult{Models: body.Data}
}

// FormatDescription builds the "ctx 128k · $3.00/$15.00 per 1M" style line.
// Pricing is per-token in USD as strings — converted to $/1M tokens for display.
// ctx 표시는 박제되는 Payload와 같은 EffectiveContext 기준 (표시-저장 일관성).
func FormatDescription(m OpenRouterModel) string {
	var parts []string
	if c := EffectiveContext(m); c > 0 {
		k := int(math.Round(float64(c) / 1000))
		parts = append(parts, fmt.Sprintf("ctx %dk", k))
	}
	pIn, errIn := strconv.ParseFloat(m.Pricing.Prompt, 64)
	pOut, errOut := strconv.ParseFloat(m.Pricing.Completion, 64)
	if errIn == nil && errOut == nil {
		if pIn > 0 || pOut > 0 {
			parts = append(parts, fmt.Sprintf("$%.2f/$%.2f per 1M", pIn*1_000_000, pOut*1_000_000))
		} else if pIn == 0 && pOut == 0 {
			parts = append(parts, "free")
		}
	}
	return strings.Join(parts, " · ")
}

// IsOpenRouter matches by profile name or baseUrl host.
func IsOpenRouter(p *config.Profile) bool {
	if p == nil {
		return false
	}
	if regexp.MustCompile(`(?i)openrouter`).MatchString(p.Name) {
		return true
	}
	return regexp.MustCompile(`(?i)openrouter\.ai`).MatchString(p.BaseURL)
}
