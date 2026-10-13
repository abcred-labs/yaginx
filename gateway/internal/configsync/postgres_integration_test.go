//go:build integration

package configsync_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/configsync"
	"yaginx-gateway/internal/database"
)

func TestPostgresNotifyReachesThreeListeners(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schema := uniqueSchema(t)
	t.Logf("isolated schema=%s", schema)
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	listenCtx, stopListen := context.WithCancel(ctx)
	callback := make(chan time.Time, 3)
	pools := make([]*pgxpool.Pool, 0, 3)
	for range 3 {
		pool, err := pgxpool.New(ctx, isolated)
		if err != nil {
			t.Fatal(err)
		}
		pools = append(pools, pool)
		go configsync.Listen(listenCtx, pool, "yaginx_config_changed", func() {
			select {
			case callback <- time.Now():
			default:
			}
		})
	}
	defer func() {
		stopListen()
		for _, pool := range pools {
			pool.Close()
		}
	}()
	// LISTEN registration occurs on dedicated connections; allow it to complete
	// before issuing the trigger mutation.
	time.Sleep(300 * time.Millisecond)
	started := time.Now()
	if _, err := base.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES('notify-site','{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')"); err != nil {
		t.Fatal(err)
	}
	seen := 0
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for seen < 3 {
		select {
		case <-callback:
			seen++
		case <-deadline.C:
			t.Fatalf("received %d/3 NOTIFY callbacks in %s", seen, time.Since(started))
		}
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("propagation=%s", elapsed)
	}
	// A single fast notification does not prove the R2 propagation SLO. Run a
	// deterministic sample of independent committed revisions and use the slow
	// 95th-percentile triplet as the acceptance measurement.
	durations := make([]time.Duration, 0, 20)
	for iteration := 0; iteration < 20; iteration++ {
		started = time.Now()
		if _, err := base.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES($1,'{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')", fmt.Sprintf("notify-p95-%d", iteration)); err != nil {
			t.Fatal(err)
		}
		seen = 0
		deadline.Reset(2 * time.Second)
		for seen < 3 {
			select {
			case <-callback:
				seen++
			case <-deadline.C:
				t.Fatalf("sample %d received %d/3 NOTIFY callbacks", iteration, seen)
			}
		}
		durations = append(durations, time.Since(started))
	}
	if got := percentile95(durations); got >= 2*time.Second {
		t.Fatalf("three-node propagation p95=%s, want <2s; samples=%v", got, durations)
	}
	// Polling is the recovery path when a node misses NOTIFY. Establish its
	// initial revision, then mutate again with no dependence on callback data.
	poll := make(chan int64, 6)
	for _, pool := range pools {
		source := revisionSource{pool}
		go configsync.Poll(listenCtx, 10*time.Millisecond, source, func(revision int64) { poll <- revision })
	}
	for i := 0; i < 3; i++ {
		select {
		case <-poll:
		case <-time.After(time.Second):
			t.Fatal("initial revision poll did not reach all nodes")
		}
	}
	if _, err := base.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES('poll-site','{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')"); err != nil {
		t.Fatal(err)
	}
	seen = 0
	deadline.Reset(2 * time.Second)
	for seen < 3 {
		select {
		case revision := <-poll:
			if revision >= 2 {
				seen++
			}
		case <-deadline.C:
			t.Fatalf("revision poll reached %d/3 nodes", seen)
		}
	}
}

func percentile95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (95*len(sorted) + 99) / 100
	return sorted[index-1]
}

// The poller is the reconciliation path when a notification connection is
// interrupted. Terminating the dedicated pool backend gives this gate a real
// PostgreSQL connection loss (not a mocked RevisionSource), after which the
// next revision must be observed without relying on NOTIFY delivery.
func TestPostgresRevisionPollRecoversAfterConnectionTermination(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schema := uniqueSchema(t)
	t.Logf("isolated schema=%s", schema)
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	poolConfig, err := pgxpool.ParseConfig(isolated)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pollCtx, stopPoll := context.WithCancel(ctx)
	defer stopPoll()
	revisions := make(chan int64, 4)
	go configsync.Poll(pollCtx, 15*time.Millisecond, revisionSource{pool}, func(revision int64) {
		select {
		case revisions <- revision:
		default:
		}
	})
	select {
	case revision := <-revisions:
		if revision != 0 {
			t.Fatalf("initial revision=%d", revision)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initial poll did not establish a database connection")
	}
	var backendPID int
	if err := pool.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, "SELECT pg_terminate_backend($1)", backendPID); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES('recovery-site','{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')"); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case revision := <-revisions:
			if revision >= 1 {
				return
			}
		case <-deadline.C:
			t.Fatal("revision poll did not recover after PostgreSQL connection termination")
		}
	}
}

type revisionSource struct{ pool *pgxpool.Pool }

func (s revisionSource) Revision(ctx context.Context) (int64, error) {
	var revision int64
	err := s.pool.QueryRow(ctx, "SELECT revision FROM gateway_config_revision LIMIT 1").Scan(&revision)
	return revision, err
}

func uniqueSchema(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return "yaginx_goal_nt" + hex.EncodeToString(raw)
}
