package config

import "testing"

func TestIsLocalBaseURL(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:8000":          true,
		"http://127.0.0.1:8010":          true,
		"http://192.168.0.176:8000":      true,
		"http://10.0.0.5:8000":           true,
		"http://mac-mini.local:8000":     true,
		"https://api.z.ai/api/anthropic": false,
		"https://openrouter.ai/api":      false,
		"":                               false,
		"not a url":                      false,
	}
	for u, want := range cases {
		if got := IsLocalBaseURL(u); got != want {
			t.Errorf("IsLocalBaseURL(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestIsLocalBaseURLResolvesEnvRef(t *testing.T) {
	t.Setenv("CCX_TEST_LOCAL_URL", "http://192.168.1.20:8000")
	if !IsLocalBaseURL("env:CCX_TEST_LOCAL_URL") {
		t.Fatal("env: 참조를 해석한 뒤 판정해야 한다")
	}
}
