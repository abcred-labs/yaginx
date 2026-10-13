package bootstrap

import (
	"context"
	"os"
	"os/signal"
)

// ContextWithSignals is reserved for the future serve lifecycle.
func ContextWithSignals(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}
