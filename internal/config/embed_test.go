package config

import (
	"bytes"
	"os"
	"testing"
)

// 모듈 루트의 정본 ccx.config.example.json과 임베드용 사본이 동일해야 한다.
// build.sh가 동기화하지만, 일반 go build/test에서도 어긋남을 잡기 위한 가드.
func TestEmbeddedExampleInSyncWithRoot(t *testing.T) {
	root, err := os.ReadFile("../../ccx.config.example.json")
	if err != nil {
		t.Fatalf("failed to read canonical example: %v", err)
	}
	if !bytes.Equal(root, embeddedExample) {
		t.Fatal("internal/config/ccx.config.example.json copy differs from the module root. " +
			"Run `./build.sh` or `cp ccx.config.example.json internal/config/` to sync.")
	}
}
