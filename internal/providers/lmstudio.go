package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type openAIModelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type LMStudioResult struct {
	Models []string
	URL    string
	Err    error
}

// FetchLMStudioModels hits the OpenAI-compatible /v1/models endpoint that
// LM Studio exposes. baseUrl is normalized (trailing slashes + /v1 suffix
// stripped) so both "http://localhost:1234" and ".../v1" work.
func FetchLMStudioModels(baseURL, token string) LMStudioResult {
	base := strings.TrimRight(baseURL, "/")
	base = regexp.MustCompile(`/v1$`).ReplaceAllString(base, "")
	url := base + "/v1/models"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return LMStudioResult{URL: url, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return LMStudioResult{URL: url, Err: err}
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		return LMStudioResult{URL: url, Err: fmt.Errorf("HTTP %d %s", res.StatusCode, res.Status)}
	}

	var list openAIModelList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return LMStudioResult{URL: url, Err: err}
	}

	models := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return LMStudioResult{Models: models, URL: url}
}

// IsLMStudio heuristically detects LM Studio profiles by name.
func IsLMStudio(name string) bool {
	return regexp.MustCompile(`(?i)LM\s*Studio`).MatchString(name)
}
