package app

import (
	"fmt"
	"io"
)

const BaseVersion = "1.0"

var (
	Version   = BaseVersion + ".0"
	Commit    = "none"
	BuildTime = "unknown"
)

func runVersion(stdout io.Writer) int {
	fmt.Fprintf(stdout, "yaginx %s (commit=%s buildTime=%s)\n", Version, Commit, BuildTime)
	return 0
}
