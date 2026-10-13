package analytics

import (
	"testing"
	"time"
)

func TestScheduleHonorsPeriodDelays(t *testing.T) {
	schedule := Schedule{MinutelyDelay: 10 * time.Second, HourlyDelay: time.Minute, DailyDelay: 5 * time.Minute}
	base := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if due := schedule.Due(base.Add(9 * time.Second)); due.Any() {
		t.Fatal("minute report ran before its delay")
	}
	if due := schedule.Due(base.Add(10 * time.Second)); !due.Minutely || due.Hourly || due.Daily {
		t.Fatal("minute report did not run at its delay")
	}
	if due := schedule.Due(base.Add(20 * time.Second)); due.Any() {
		t.Fatal("minute report ran more than once in one period")
	}
	if due := schedule.Due(base.Add(time.Minute + 10*time.Second)); !due.Minutely || !due.Hourly || due.Daily {
		t.Fatalf("minute/hour report due=%#v", due)
	}
	if schedule.lastHour != base.Unix() {
		t.Fatalf("hour reconciliation key=%d want=%d", schedule.lastHour, base.Unix())
	}
	daily := time.Date(2026, 7, 28, 0, 5, 0, 0, time.UTC)
	if due := schedule.Due(daily); due.Minutely || due.Hourly || !due.Daily {
		t.Fatalf("minute/daily report due=%#v", due)
	}
	if schedule.lastDay != base.Unix() {
		t.Fatalf("daily reconciliation key=%d want=%d", schedule.lastDay, base.Unix())
	}
}

func TestScheduleSkipsCompletedStartupPeriods(t *testing.T) {
	schedule := Schedule{MinutelyDelay: 10 * time.Second, HourlyDelay: time.Minute, DailyDelay: 5 * time.Minute}
	started := time.Date(2026, 7, 28, 0, 0, 10, 0, time.UTC)
	schedule.MarkCompleted(started, Due{Minutely: true, Hourly: true, Daily: true})
	if due := schedule.Due(started.Add(10 * time.Second)); due.Any() {
		t.Fatalf("completed startup periods ran again: %#v", due)
	}
	if due := schedule.Due(started.Add(time.Minute)); !due.Minutely || due.Hourly || due.Daily {
		t.Fatalf("next minute due=%#v", due)
	}
}

func TestReportReconciliationWindowUsesUTCDateBounds(t *testing.T) {
	now := time.Date(2026, time.August, 3, 2, 17, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	begin, end := ReconciliationWindow(now, 48*time.Hour)
	if want := time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC); !begin.Equal(want) {
		t.Fatalf("begin=%s want=%s", begin, want)
	}
	if want := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("end=%s want=%s", end, want)
	}
}

func TestReportMinutelyReconciliationWindowUsesMinuteBounds(t *testing.T) {
	now := time.Date(2026, time.August, 3, 2, 17, 42, 0, time.FixedZone("UTC+8", 8*60*60))
	begin, end := MinutelyReconciliationWindow(now, 10*time.Minute)
	if want := time.Date(2026, time.August, 2, 18, 7, 0, 0, time.UTC); !begin.Equal(want) {
		t.Fatalf("begin=%s want=%s", begin, want)
	}
	if want := time.Date(2026, time.August, 2, 18, 18, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("end=%s want=%s", end, want)
	}
}

func TestEffectiveDailyReportRetentionPreservesLegacyLowerBound(t *testing.T) {
	if got := EffectiveDailyRetention(730, 365); got != 730 {
		t.Fatalf("default daily retention=%d", got)
	}
	if got := EffectiveDailyRetention(730, 1000); got != 1000 {
		t.Fatalf("legacy lower bound=%d", got)
	}
	if got := EffectiveDailyRetention(0, 365); got != 730 {
		t.Fatalf("invalid daily retention=%d", got)
	}
}
