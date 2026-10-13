package repository

import (
	"testing"
	"time"
)

func TestCertificateValidity(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if got := certificateValidity(now.Add(time.Hour), now.Add(2*time.Hour), now); got != "not_yet_valid" {
		t.Fatalf("future certificate validity=%q", got)
	}
	if got := certificateValidity(now.Add(-2*time.Hour), now.Add(-time.Hour), now); got != "expired" {
		t.Fatalf("expired certificate validity=%q", got)
	}
	if got := certificateValidity(now.Add(-time.Hour), now.Add(time.Hour), now); got != "valid" {
		t.Fatalf("current certificate validity=%q", got)
	}
}

func TestACMEOrderProgressStates(t *testing.T) {
	for _, status := range []string{"pending", "locked", "authorizing", "finalizing", "retry_wait"} {
		if !acmeInProgressStatuses[status] {
			t.Fatalf("status %q should be in progress", status)
		}
	}
	for _, status := range []string{"valid", "failed", "canceled"} {
		if acmeInProgressStatuses[status] {
			t.Fatalf("status %q should be terminal", status)
		}
	}
}
