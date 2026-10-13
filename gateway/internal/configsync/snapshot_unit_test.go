package configsync

import (
	"context"
	"testing"
	"time"
)

func TestDebounceWakeCoalescesNotifications(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wake := make(chan struct{}, 3)
	called := make(chan struct{}, 2)
	go debounceWake(ctx, wake, 20*time.Millisecond, func() { called <- struct{}{} })
	wake <- struct{}{}
	wake <- struct{}{}
	wake <- struct{}{}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("debounced wake did not run")
	}
	select {
	case <-called:
		t.Fatal("notification burst was not coalesced")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNextRetryCapsAtMaximum(t *testing.T) {
	if got := nextRetry(250*time.Millisecond, time.Second); got != 500*time.Millisecond {
		t.Fatalf("first retry=%s", got)
	}
	if got := nextRetry(500*time.Millisecond, time.Second); got != time.Second {
		t.Fatalf("capped retry=%s", got)
	}
}
