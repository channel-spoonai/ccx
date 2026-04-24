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

	"github.com/yobuce/claudex/internal/config"
)

type OpenRouterModel struct {
	ID            string  `json:"id"`
	ContextLength int     `json:"context_length"`
	Pricing       Pricing `json:"pricing"`
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
func FormatDescription(m OpenRouterModel) string {
	var parts []string
	if m.ContextLength > 0 {
		k := int(math.Round(float64(m.ContextLength) / 1000))
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
