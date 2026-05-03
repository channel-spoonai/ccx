package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/channel-spoonai/ccx/internal/update"
)

func runUpdateCommand(argv []string) {
	for _, a := range argv {
		switch a {
		case "-h", "--help", "help":
			printUpdateUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "Error: 알 수 없는 인자 %q\n\n", a)
			printUpdateUsage()
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := update.Apply(ctx, version, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "[ccx] 업데이트 실패:", err)
		os.Exit(1)
	}
}

func printUpdateUsage() {
	fmt.Println(`사용법:
  ccx update    GitHub 최신 릴리즈로 ccx 바이너리를 갱신`)
}
