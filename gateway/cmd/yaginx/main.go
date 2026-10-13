package main

import (
	"context"
	"os"

	"yaginx-gateway/internal/app"
)

func main() {
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
