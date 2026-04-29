package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/channel-spoonai/ccx/internal/config"
)

type AnthropicModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Type        string `json:"type,omitempty"`
}

type AnthropicModelsResult struct {
	Models []AnthropicModel
	URL    string
	Err    error
}

// FetchAnthropicModels queries an Anthropic-compatible /v1/models endpoint.
// Different providers expose the catalog at different paths — z.ai/DeepSeek
// keep it on the OpenAI-compat root rather than the /anthropic prefix — so
// the function tries {baseURL}/v1/models first, then a stripped root.
func FetchAnthropicModels(p *config.Profile) AnthropicModelsResult {
	if p == nil || p.BaseURL == "" {
		return AnthropicModelsResult{Err: errors.New("baseURL이 비어 있습니다")}
	}
	urls := candidateModelURLs(p.BaseURL)
	var lastErr error
	var lastURL string
	for _, url := range urls {
		res := fetchModelsAt(url, p)
		lastURL = url
		if res.Err == nil && len(res.Models) > 0 {
			return res
		}
		if res.Err != nil {
			lastErr = res.Err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("모델 목록이 비어 있습니다")
	}
	return AnthropicModelsResult{URL: lastURL, Err: lastErr}
}

// candidateModelURLs returns endpoint URLs to try, in order.
func candidateModelURLs(baseURL string) []string {
	base := strings.TrimRight(baseURL, "/")
	primary := base + "/v1/models"
	out := []string{primary}
	for _, suf := range []string{"/anthropic", "/v1/anthropic"} {
		if strings.HasSuffix(base, suf) {
			alt := strings.TrimSuffix(base, suf) + "/v1/models"
			if alt != primary {
				out = append(out, alt)
			}
		}
	}
	return out
}

func fetchModelsAt(url string, p *config.Profile) AnthropicModelsResult {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return AnthropicModelsResult{URL: url, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	// Some providers (z.ai, OpenRouter) expect Bearer; others (DeepSeek,
	// real Anthropic) expect x-api-key. Send both when available so a
	// single GET works across all of them.
	if p.APIKey != "" {
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if p.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.AuthToken)
		if req.Header.Get("x-api-key") == "" {
			req.Header.Set("x-api-key", p.AuthToken)
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return AnthropicModelsResult{URL: url, Err: err}
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		return AnthropicModelsResult{URL: url, Err: fmt.Errorf("HTTP %d %s", res.StatusCode, res.Status)}
	}

	body := struct {
		Data []AnthropicModel `json:"data"`
	}{}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return AnthropicModelsResult{URL: url, Err: err}
	}
	out := make([]AnthropicModel, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, m)
		}
	}
	return AnthropicModelsResult{URL: url, Models: out}
}
