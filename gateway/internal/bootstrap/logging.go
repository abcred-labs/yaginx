package bootstrap

import (
	"io"
	"log/slog"
	"strings"
)

// NewLogger builds the initialization logger without changing process-global state.
func NewLogger(output io.Writer, level, format string) *slog.Logger {
	var configured slog.Level
	if err := configured.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		configured = slog.LevelInfo
	}
	options := &slog.HandlerOptions{Level: configured}
	if strings.EqualFold(format, "text") {
		return slog.New(slog.NewTextHandler(output, options))
	}
	return slog.New(slog.NewJSONHandler(output, options))
}
