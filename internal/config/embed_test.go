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
		t.Fatalf("정본 example 읽기 실패: %v", err)
	}
	if !bytes.Equal(root, embeddedExample) {
		t.Fatal("internal/config/ccx.config.example.json 사본이 모듈 루트 정본과 다릅니다. " +
			"`./build.sh` 또는 `cp ccx.config.example.json internal/config/`로 동기화하세요.")
	}
}
