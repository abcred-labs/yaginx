package app

import (
	"flag"
	"os"

	"yaginx-gateway/internal/config"
)

func configPath(fs *flag.FlagSet) *string {
	defaultPath := config.DefaultRuntimePath
	if fromEnvironment := os.Getenv("YAGINX_CONFIG_PATH"); fromEnvironment != "" {
		defaultPath = fromEnvironment
	}
	return fs.String("config", defaultPath, "path to the Yaginx JSON configuration file")
}
