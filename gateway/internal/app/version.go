package app

import (
	"fmt"
	"io"

	"yaginx-gateway/internal/core"
)

// BaseVersion is shared with core so a release bump is a single edit.
const BaseVersion = core.BaseVersion

var (
	Version   = BaseVersion + ".0"
	Commit    = "none"
	BuildTime = "unknown"
)

func runVersion(stdout io.Writer) int {
	fmt.Fprintf(stdout, "yaginx %s (commit=%s buildTime=%s)\n", Version, Commit, BuildTime)
	return 0
}
