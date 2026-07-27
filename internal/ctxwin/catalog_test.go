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

		{"glm-5.2", 1_000_000, true},

		// "vendor/model" ID는 벤더를 벗겨 매칭하지 않는다 — 같은 모델도 호스팅
		// 프로바이더마다 실서빙 한도가 달라(NIM의 deepseek-v4-pro는 262,144)
		// 벤더를 무시하면 조용한 오버플로가 된다. NIM 경로는 providers의 실측
		// 테이블이 suffix로 박제한다.
		{"deepseek-ai/deepseek-v4-pro", 0, false},
		{"z-ai/glm-5.2", 0, false},
		{"nvidia/nemotron-3-ultra-550b-a55b", 0, false},
	}
	for _, c := range cases {
		w, ok := CatalogLookup(c.id)
		if w != c.window || ok != c.ok {
			t.Errorf("CatalogLookup(%q) = (%d, %v), want (%d, %v)", c.id, w, ok, c.window, c.ok)
		}
	}
}
