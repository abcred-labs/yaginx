package main

import (
	"context"
	"os"

	"yaginx-gateway/internal/core"
)

func main() {
	os.Exit(core.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
