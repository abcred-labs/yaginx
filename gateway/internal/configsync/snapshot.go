// Package configsync persists last-known-good runtime snapshots and provides a
// revision poller fallback for LISTEN/NOTIFY outages.
package configsync

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/platform/atomicfile"
)

func SaveLKG(path string, snapshot []byte) error {
	return atomicfile.Write(path, snapshot, 0600)
}
func LoadLKG(path string, read func(string) ([]byte, error)) ([]byte, error) {
	return read(path)
}

type RevisionSource interface {
	Revision(context.Context) (int64, error)
}

// ListenOptions controls notification coalescing and retry behavior. Database
// notifications are wakeups only, so collapsing a burst never loses the
// authoritative revision reconciliation.
type ListenOptions struct {
	NotifyDebounce          time.Duration
	ReconnectInitialBackoff time.Duration
	ReconnectMaximumBackoff time.Duration
}

func Poll(ctx context.Context, interval time.Duration, source RevisionSource, onRevision func(int64), observeError ...func(error)) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last int64 = -1
	for {
		revision, err := source.Revision(ctx)
		if err != nil {
			if ctx.Err() == nil && len(observeError) > 0 && observeError[0] != nil {
				observeError[0](err)
			}
		} else if revision != last {
			last = revision
			onRevision(revision)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Listen uses a dedicated acquired connection. Notification payloads are only
// wakeups; callers must reconcile from the authoritative revision table.
func Listen(ctx context.Context, pool *pgxpool.Pool, channel string, onWake func(), observe ...func(string)) {
	ListenWithOptions(ctx, pool, channel, ListenOptions{}, onWake, observe...)
}

// ListenWithOptions maintains the dedicated LISTEN connection. It uses
// bounded exponential retry and a single debounced wakeup worker so a burst of
// pg_notify calls cannot trigger concurrent snapshot builds.
func ListenWithOptions(ctx context.Context, pool *pgxpool.Pool, channel string, options ListenOptions, onWake func(), observe ...func(string)) {
	report := func(result string) {
		if len(observe) != 0 && observe[0] != nil {
			observe[0](result)
		}
	}
	if options.NotifyDebounce <= 0 {
		options.NotifyDebounce = 100 * time.Millisecond
	}
	if options.ReconnectInitialBackoff <= 0 {
		options.ReconnectInitialBackoff = 250 * time.Millisecond
	}
	if options.ReconnectMaximumBackoff < options.ReconnectInitialBackoff {
		options.ReconnectMaximumBackoff = 30 * time.Second
	}
	wake := make(chan struct{}, 1)
	go debounceWake(ctx, wake, options.NotifyDebounce, onWake)
	retry := options.ReconnectInitialBackoff
	for ctx.Err() == nil {
		connection, err := pool.Acquire(ctx)
		if err != nil {
			report("failure")
			if !waitRetry(ctx, retry) {
				return
			}
			retry = nextRetry(retry, options.ReconnectMaximumBackoff)
			continue
		}
		_, err = connection.Exec(ctx, "LISTEN "+quoteIdentifier(channel))
		if err == nil {
			report("success")
			retry = options.ReconnectInitialBackoff
			for ctx.Err() == nil {
				_, err = connection.Conn().WaitForNotification(ctx)
				if err != nil {
					if ctx.Err() == nil {
						report("failure")
					}
					break
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		} else {
			report("failure")
		}
		connection.Release()
		if !waitRetry(ctx, retry) {
			return
		}
		retry = nextRetry(retry, options.ReconnectMaximumBackoff)
	}
}

func debounceWake(ctx context.Context, wake <-chan struct{}, delay time.Duration, onWake func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
		timer := time.NewTimer(delay)
		for {
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(delay)
			case <-timer.C:
				if onWake != nil {
					onWake()
				}
				goto next
			}
		}
	next:
	}
}

func waitRetry(ctx context.Context, retry time.Duration) bool {
	timer := time.NewTimer(retry)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextRetry(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}
func quoteIdentifier(value string) string {
	out := "\""
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			out += string(r)
		}
	}
	return out + "\""
}
