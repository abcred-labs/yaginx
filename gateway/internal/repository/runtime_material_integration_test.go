//go:build integration

package repository

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/database"
)

func TestRuntimeMaterialIsCreatedOnceAndShared(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_runtime" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pools := make([]*pgxpool.Pool, 4)
	for index := range pools {
		pools[index], err = pgxpool.New(ctx, isolation)
		if err != nil {
			t.Fatal(err)
		}
		defer pools[index].Close()
	}
	values := make([]RuntimeMaterial, len(pools))
	errs := make(chan error, len(pools))
	var wait sync.WaitGroup
	for index, pool := range pools {
		wait.Add(1)
		go func(index int, pool *pgxpool.Pool) {
			defer wait.Done()
			material, createErr := New(pool).LoadOrCreateRuntimeMaterial(ctx)
			values[index] = material
			errs <- createErr
		}(index, pool)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for index := 1; index < len(values); index++ {
		if string(values[index].JWTCurrent) != string(values[0].JWTCurrent) || len(values[index].JWTCurrent) < 32 {
			t.Fatalf("instance %d received different runtime material", index)
		}
	}
	var count int
	if err := pools[0].QueryRow(ctx, "SELECT count(*) FROM gateway_runtime_material").Scan(&count); err != nil || count != 1 {
		t.Fatalf("runtime material rows=%d err=%v", count, err)
	}
	const replacement = "01234567890123456789012345678901-replacement"
	if _, err := pools[0].Exec(ctx, "UPDATE gateway_runtime_material SET jwt_previous=jwt_current,jwt_current=$1,updated_at=clock_timestamp() WHERE singleton_id=1", replacement); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(pools[1]).LoadRuntimeMaterial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(reloaded.JWTCurrent) != replacement || string(reloaded.JWTPrevious) != string(values[0].JWTCurrent) {
		t.Fatalf("reloaded runtime material=%#v", reloaded)
	}
	var events int
	if err := pools[0].QueryRow(ctx, "SELECT count(*) FROM gateway_config_event WHERE resource_type='gateway_runtime_material'").Scan(&events); err != nil || events < 2 {
		t.Fatalf("runtime material events=%d err=%v", events, err)
	}
}
