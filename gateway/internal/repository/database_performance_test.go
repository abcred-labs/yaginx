package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"yaginx-gateway/internal/management"
)

func TestDatabasePerformanceHealthThresholds(t *testing.T) {
	tests := []struct {
		name     string
		pool     management.DatabasePoolStats
		activity management.DatabaseActivity
		tables   []management.DatabaseTableStats
		level    string
	}{
		{
			name:     "healthy",
			pool:     management.DatabasePoolStats{UsageRate: .40},
			activity: management.DatabaseActivity{},
			level:    "healthy",
		},
		{
			name:     "warning for pool and table maintenance",
			pool:     management.DatabasePoolStats{UsageRate: .80},
			activity: management.DatabaseActivity{MaxLockWaitSeconds: 2, LockWaits: 1, IdleInTransaction: 1},
			tables:   []management.DatabaseTableStats{{Name: "request_history", LiveTuples: 800, DeadTuples: 200, DeadTupleRatio: .20}},
			level:    "warning",
		},
		{
			name:     "critical for sustained lock wait",
			pool:     management.DatabasePoolStats{UsageRate: .95},
			activity: management.DatabaseActivity{MaxLockWaitSeconds: 30, LockWaits: 1, UngrantedLocks: 1},
			level:    "critical",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			health := databasePerformanceHealth(test.pool, test.activity, test.tables)
			if health.Level != test.level {
				t.Fatalf("health=%#v want level=%q", health, test.level)
			}
		})
	}
}

func TestDatabaseCapabilityReasonIsSafeAndSpecific(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{code: "42P01", want: "extension is not installed"},
		{code: "42501", want: "permission denied"},
		{code: "55000", want: "extension is not loaded through shared_preload_libraries"},
		{code: "XX000", want: "fallback"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			got := databaseCapabilityReason(&pgconn.PgError{Code: test.code}, "fallback")
			if got != test.want {
				t.Fatalf("reason=%q want=%q", got, test.want)
			}
		})
	}
	if got := databaseCapabilityReason(errors.New("sensitive connection detail"), "fallback"); got != "fallback" {
		t.Fatalf("reason=%q", got)
	}
	if got := databaseIOCapabilityReason(&pgconn.PgError{Code: "42P01"}); got != "PostgreSQL 15 does not provide pg_stat_io" {
		t.Fatalf("pg_stat_io reason=%q", got)
	}
}

func TestDatabaseCapabilityReasonClassifiesCanceledQueries(t *testing.T) {
	if got := databaseCapabilityReason(context.DeadlineExceeded, "fallback"); got != "database performance query timed out" {
		t.Fatalf("deadline reason=%q", got)
	}
	if got := databaseCapabilityReason(context.Canceled, "fallback"); got != "database performance query canceled" {
		t.Fatalf("canceled reason=%q", got)
	}
	if got := databaseCapabilityReason(&pgconn.PgError{Code: "57014"}, "fallback"); got != "database performance query timed out" {
		t.Fatalf("statement cancel reason=%q", got)
	}
}

func TestDatabaseTopSQLQueryIDSupportsNullValues(t *testing.T) {
	tests := []struct {
		name  string
		value pgtype.Text
		want  string
	}{
		{
			name:  "query id is available",
			value: pgtype.Text{String: "12345", Valid: true},
			want:  "12345",
		},
		{
			name:  "query id is unavailable",
			value: pgtype.Text{},
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := databaseTopSQLQueryID(test.value); got != test.want {
				t.Fatalf("query id=%q want=%q", got, test.want)
			}
		})
	}
}
