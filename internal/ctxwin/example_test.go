package ctxwin

import (
	"strings"
	"testing"

	"github.com/channel-spoonai/ccx/internal/config"
)

// example config에 적힌 모든 컨텍스트 suffix가 ParseSuffix로 해석 가능한지 가드.
// 해석 불가능한 suffix는 직결 경로에서 업스트림에 리터럴로 유출되므로 금지.
func TestExampleConfigSuffixesParse(t *testing.T) {
	profiles, err := config.LoadExample()
	if err != nil {
		t.Fatalf("LoadExample: %v", err)
	}
	check := func(profile, tier, model string) {
		if model == "" || !strings.Contains(model, "[") {
			return
		}
		if _, _, ok := ParseSuffix(model); !ok {
			t.Errorf("profile %q %s model %q: unparseable context suffix", profile, tier, model)
		}
	}
	for _, p := range profiles {
		if p.Models != nil {
			check(p.Name, "opus", p.Models.Opus)
			check(p.Name, "sonnet", p.Models.Sonnet)
			check(p.Name, "haiku", p.Models.Haiku)
		}
		check(p.Name, "model", p.Model)
	}
}
