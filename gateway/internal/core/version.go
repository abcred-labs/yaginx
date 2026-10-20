package core

import "fmt"

const BaseVersion = "1.1"

var (
	Version   = BaseVersion + ".0"
	Commit    = "none"
	BuildTime = "unknown"
)

func VersionString() string {
	return fmt.Sprintf("yaginx-core %s (commit=%s buildTime=%s)", Version, Commit, BuildTime)
}
