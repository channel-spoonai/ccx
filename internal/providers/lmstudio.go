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

// openAIModelList는 표준 /v1/models 응답. context 관련 필드는 스펙에 없지만
// vLLM·SGLang·MTPLX 등 다수 서버가 실제 서빙 한도를 여기에 실어 보낸다.
type openAIModelList struct {
	Data []struct {
		ID               string `json:"id"`
		ContextLength    int    `json:"context_length"`
		MaxContextLength int    `json:"max_context_length"`
		MaxModelLen      int    `json:"max_model_len"`
	} `json:"data"`
}

type LMStudioResult struct {
	Models []string

	// Contexts는 /v1/models가 선언한 모델별 컨텍스트 한도. 선언이 없으면 빈 맵.
	// 여러 필드가 동시에 오면 최솟값을 취한다 — max_context_length는 모델 스펙상
	// 최대치이고 max_model_len은 이 배포가 실제로 서빙하는 한도라, 큰 쪽을 믿으면
	// 조용한 컨텍스트 오버플로가 된다.
	Contexts map[string]int

	URL string
	Err error
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
	contexts := map[string]int{}
	for _, m := range list.Data {
		if m.ID == "" {
			continue
		}
		models = append(models, m.ID)
		if w := minPositive(m.MaxModelLen, m.ContextLength, m.MaxContextLength); w > 0 {
			contexts[m.ID] = w
		}
	}
	return LMStudioResult{Models: models, Contexts: contexts, URL: url}
}

// minPositive는 0보다 큰 값들 중 최솟값을 돌려준다 (전부 0이면 0).
func minPositive(values ...int) int {
	out := 0
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if out == 0 || v < out {
			out = v
		}
	}
	return out
}

// IsLMStudio heuristically detects LM Studio profiles by name.
func IsLMStudio(name string) bool {
	return regexp.MustCompile(`(?i)LM\s*Studio`).MatchString(name)
}

// FetchLMStudioContexts는 모델 ID → 로드된 인스턴스에 실제 할당된 컨텍스트
// 길이 맵을 돌려준다. OpenAI 호환 /v1/models에는 컨텍스트 정보가 없어
// LM Studio 네이티브 REST API로만 얻을 수 있다. max_context_length는 모델의
// "가능 최대치"라 실제 로드 설정과 다를 수 있으므로 로드된 값만 신뢰한다.
// 같은 모델의 인스턴스가 여럿이면 보수적으로 최솟값. 네이티브 API가 없는
// 구버전·연결 실패 시에는 빈 맵 (조용한 폴백 — 컨텍스트 감지만 포기).
// token은 FetchLMStudioModels와 동일하게 선택적 Bearer.
func FetchLMStudioContexts(baseURL, token string) map[string]int {
	base := strings.TrimRight(baseURL, "/")
	base = regexp.MustCompile(`/v1$`).ReplaceAllString(base, "")
	out := map[string]int{}

	// v1 네이티브 API (LM Studio 0.4.x+): models[].loaded_instances[].config.context_length
	var v1 struct {
		Models []struct {
			Key             string `json:"key"`
			ID              string `json:"id"`
			LoadedInstances []struct {
				ID     string `json:"id"`
				Key    string `json:"key"`
				Config struct {
					ContextLength int `json:"context_length"`
				} `json:"config"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	record := func(id string, c int) {
		if id == "" || c <= 0 {
			return
		}
		if cur, ok := out[id]; !ok || c < cur {
			out[id] = c
		}
	}
	if getJSON(base+"/api/v1/models", token, &v1) == nil {
		for _, m := range v1.Models {
			id := m.Key
			if id == "" {
				id = m.ID
			}
			for _, inst := range m.LoadedInstances {
				c := inst.Config.ContextLength
				record(id, c)
				// 다중 로드 시 /v1/models가 인스턴스 식별자("model:2" 등)를
				// 노출할 수 있으므로 인스턴스 자체 식별자로도 매핑해 둔다.
				record(inst.ID, c)
				record(inst.Key, c)
			}
		}
	}
	if len(out) > 0 {
		return out
	}

	// v0 폴백 (deprecated이나 동작): data[].loaded_context_length가 실할당값
	var v0 struct {
		Data []struct {
			ID                  string `json:"id"`
			LoadedContextLength int    `json:"loaded_context_length"`
		} `json:"data"`
	}
	if getJSON(base+"/api/v0/models", token, &v0) == nil {
		for _, m := range v0.Data {
			if m.ID != "" && m.LoadedContextLength > 0 {
				out[m.ID] = m.LoadedContextLength
			}
		}
	}
	return out
}

func getJSON(url, token string, dst any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d %s", res.StatusCode, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(dst)
}
