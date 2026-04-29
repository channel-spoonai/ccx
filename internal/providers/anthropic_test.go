package providers

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
)

func TestCandidateModelURLs(t *testing.T) {
	cases := []struct {
		base string
		want []string
	}{
		{"https://api.deepseek.com/anthropic", []string{
			"https://api.deepseek.com/anthropic/v1/models",
			"https://api.deepseek.com/v1/models",
		}},
		{"https://api.z.ai/api/anthropic", []string{
			"https://api.z.ai/api/anthropic/v1/models",
			"https://api.z.ai/api/v1/models",
		}},
		{"https://api.example.com", []string{
			"https://api.example.com/v1/models",
		}},
		{"https://api.example.com/", []string{
			"https://api.example.com/v1/models",
		}},
	}
	for _, c := range cases {
		got := candidateModelURLs(c.base)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("candidateModelURLs(%q) = %v, want %v", c.base, got, c.want)
		}
	}
}

func TestFetchAnthropicModels_FallbackToStrippedRoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/anthropic/v1/models":
			http.Error(w, "not found", http.StatusNotFound)
		case "/v1/models":
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2","display_name":"Model 2"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	res := FetchAnthropicModels(&config.Profile{
		BaseURL:   srv.URL + "/anthropic",
		AuthToken: "tok",
	})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if len(res.Models) != 2 || res.Models[0].ID != "m1" || res.Models[1].DisplayName != "Model 2" {
		t.Fatalf("unexpected models: %+v", res.Models)
	}
}

func TestFetchAnthropicModels_MissingBaseURL(t *testing.T) {
	res := FetchAnthropicModels(&config.Profile{})
	if res.Err == nil {
		t.Fatal("expected error for empty baseURL")
	}
}
