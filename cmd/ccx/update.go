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
			fmt.Fprintf(os.Stderr, "Error: unknown argument %q\n\n", a)
			printUpdateUsage()
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := update.Apply(ctx, version, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "[ccx] update failed:", err)
		os.Exit(1)
	}
}

func printUpdateUsage() {
	fmt.Println(`Usage:
  ccx update    Update the ccx binary to the latest GitHub release`)
}
