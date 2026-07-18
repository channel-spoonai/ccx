package ctxwin

import "testing"

func TestCatalogLookup(t *testing.T) {
	cases := []struct {
		id     string
		window int
		ok     bool
	}{
		{"GLM-4.7", 200_000, true},
		{"GLM-4.7-Flash", 200_000, true},
		{"glm-4.5-air", 131_072, true},
		{"kimi-k2.5", 262_144, true},
		{"kimi-k2.7-code-highspeed", 262_144, true},
		{"kimi-k3", 1_000_000, true},
		{"deepseek-v4-pro", 1_000_000, true},
		{"MiniMax-M2.7", 204_800, true},
		{"MiniMax-M2-her", 64_000, true}, // 최장 prefix가 minimax-m2를 이김
		{"MiniMax-M3", 1_000_000, true},
		{"gpt-5.6-sol", 1_050_000, true},
		{"gpt-5.5", 1_050_000, true},
		{"ibm/granite-4-micro", 0, false},
		{"", 0, false},
		// 토큰 경계: prefix 뒤에 영숫자가 이어지면 다른 모델이므로 매칭 금지
		{"kimi-k30", 0, false},
		{"GLM-4.5V", 0, false},
		{"gpt-5.55", 0, false},
	}
	for _, c := range cases {
		w, ok := CatalogLookup(c.id)
		if w != c.window || ok != c.ok {
			t.Errorf("CatalogLookup(%q) = (%d, %v), want (%d, %v)", c.id, w, ok, c.window, c.ok)
		}
	}
}
