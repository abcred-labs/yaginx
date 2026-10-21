//go:build integration

package database

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"

	v2migrations "yaginx-gateway/migrations/v2"
)

func TestV2InitializeEmptyDatabaseAndRejectRepeat(t *testing.T) {
	ctx, dsn, _ := isolatedV2Database(t)

	if state, err := DatabaseState(ctx, dsn); err != nil || state != StateUninitialized {
		t.Fatalf("initial state=%q err=%v", state, err)
	}
	initializeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var output bytes.Buffer
	if err := InitializeV2(initializeCtx, dsn, &output); err != nil {
		t.Fatalf("InitializeV2() error=%v", err)
	}
	for _, expected := range []string{"current-v2", "tables=", "indexes=", "triggers=", "functions=", "roles.sql"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("initialization output=%q, missing %q", output.String(), expected)
		}
	}
	if state, err := DatabaseState(ctx, dsn); err != nil || state != StateCurrentV2 {
		t.Fatalf("initialized state=%q err=%v", state, err)
	}
	if err := InitializeV2(ctx, dsn, bytes.NewBuffer(nil)); err == nil || !strings.Contains(err.Error(), "requires an empty database") {
		t.Fatalf("repeat initialization error=%v", err)
	}
}

func TestV2MigrateUpgradesExistingBaseline(t *testing.T) {
	ctx, dsn, _ := isolatedV2Database(t)
	db, err := openSQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(v2migrations.Files)
	goose.SetTableName("yaginx_schema_migration")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 1); err != nil {
		t.Fatalf("create baseline v2 schema: %v", err)
	}
	if state, err := DatabaseState(ctx, dsn); err != nil || state != StateCurrentV2Pending {
		t.Fatalf("baseline state=%q err=%v", state, err)
	}
	if err := MigrateV2(ctx, dsn, bytes.NewBuffer(nil)); err != nil {
		t.Fatalf("upgrade baseline v2 schema: %v", err)
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema=current_schema() AND table_name='gateway_acme_key_value'
	)`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("baseline upgrade did not create gateway_acme_key_value")
	}
}

func TestV2MigrateAcceptsPendingIntermediateLineage(t *testing.T) {
	ctx, dsn, _ := isolatedV2Database(t)
	db, err := openSQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(v2migrations.Files)
	goose.SetTableName("yaginx_schema_migration")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 4); err != nil {
		t.Fatalf("create intermediate v2 schema: %v", err)
	}
	if state, err := DatabaseState(ctx, dsn); err != nil || state != StateCurrentV2Pending {
		t.Fatalf("intermediate state=%q err=%v", state, err)
	}
	var output bytes.Buffer
	if err := MigrateV2(ctx, dsn, &output); err != nil {
		t.Fatalf("upgrade intermediate v2 schema: %v", err)
	}
	if !strings.Contains(output.String(), "pending migrations: 5,6,7,8,9,10,11") {
		t.Fatalf("migration output=%q, missing pending migration diagnostic", output.String())
	}
	if state, err := DatabaseState(ctx, dsn); err != nil || state != StateCurrentV2 {
		t.Fatalf("completed state=%q err=%v", state, err)
	}
}

func TestV2MigrateAcceptsVersionFivePendingLineage(t *testing.T) {
	ctx, dsn, _ := isolatedV2Database(t)
	db, err := openSQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(v2migrations.Files)
	goose.SetTableName("yaginx_schema_migration")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 5); err != nil {
		t.Fatalf("create version-five v2 schema: %v", err)
	}
	if state, err := DatabaseState(ctx, dsn); err != nil || state != StateCurrentV2Pending {
		t.Fatalf("version-five state=%q err=%v", state, err)
	}
}

func TestV2MigrateRejectsPartialAndBrokenLineage(t *testing.T) {
	ctx, partialDSN, partial := isolatedV2Database(t)
	if _, err := partial.Exec(ctx, `CREATE TABLE website(id bigint PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if state, err := DatabaseState(ctx, partialDSN); err != nil || state != StateUnsupported {
		t.Fatalf("partial state=%q err=%v", state, err)
	}
	if err := MigrateV2(ctx, partialDSN, bytes.NewBuffer(nil)); err == nil {
		t.Fatal("partial schema was accepted")
	}

	ctx, brokenDSN, broken := isolatedV2Database(t)
	brokenDB, err := openSQL(ctx, brokenDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer brokenDB.Close()
	goose.SetBaseFS(v2migrations.Files)
	goose.SetTableName("yaginx_schema_migration")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, brokenDB, ".", 4); err != nil {
		t.Fatalf("create broken v2 schema: %v", err)
	}
	if _, err := broken.Exec(ctx, `DELETE FROM yaginx_schema_migration WHERE version_id = 3`); err != nil {
		t.Fatal(err)
	}
	if state, err := DatabaseState(ctx, brokenDSN); err != nil || state != StateUnsupported {
		t.Fatalf("broken lineage state=%q err=%v", state, err)
	}
	if err := MigrateV2(ctx, brokenDSN, bytes.NewBuffer(nil)); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("broken lineage migration error=%v", err)
	}
}

func TestV2InitializeHonorsAdvisoryLock(t *testing.T) {
	ctx, dsn, base := isolatedV2Database(t)
	if _, err := base.Exec(ctx, "SELECT pg_advisory_lock($1)", MigrationLockKey); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- InitializeV2(context.Background(), dsn, bytes.NewBuffer(nil)) }()
	select {
	case err := <-done:
		t.Fatalf("initializer bypassed advisory lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := base.Exec(ctx, "SELECT pg_advisory_unlock($1)", MigrationLockKey); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestV2StateRejectsNonEmptyAndForgedSchemas(t *testing.T) {
	for _, object := range []struct {
		name string
		sql  string
	}{
		{name: "view", sql: `CREATE VIEW v2_probe AS SELECT 1 AS value`},
		{name: "sequence", sql: `CREATE SEQUENCE v2_probe`},
		{name: "function", sql: `CREATE FUNCTION v2_probe() RETURNS integer LANGUAGE sql AS $$ SELECT 1 $$`},
		{name: "type", sql: `CREATE TYPE v2_probe AS ENUM ('value')`},
	} {
		t.Run(object.name, func(t *testing.T) {
			ctx, dsn, base := isolatedV2Database(t)
			if _, err := base.Exec(ctx, object.sql); err != nil {
				t.Fatal(err)
			}
			if state, err := DatabaseState(ctx, dsn); err != nil || state != StateUnsupported {
				t.Fatalf("state=%q err=%v", state, err)
			}
		})
	}

	t.Run("forged v2 marker", func(t *testing.T) {
		ctx, dsn, base := isolatedV2Database(t)
		if _, err := base.Exec(ctx, `CREATE TABLE yaginx_schema_version (generation smallint PRIMARY KEY); INSERT INTO yaginx_schema_version(generation) VALUES (2)`); err != nil {
			t.Fatal(err)
		}
		if state, err := DatabaseState(ctx, dsn); err != nil || state != StateUnsupported {
			t.Fatalf("state=%q err=%v", state, err)
		}
	})

	t.Run("leftover legacy tables are ignored", func(t *testing.T) {
		ctx, dsn, base := isolatedV2Database(t)
		if err := InitializeV2(ctx, dsn, bytes.NewBuffer(nil)); err != nil {
			t.Fatal(err)
		}
		if _, err := base.Exec(ctx, `
CREATE TABLE "VersionInfo" ("Version" bigint PRIMARY KEY);
CREATE TABLE "__EFMigrationsHistory" ("MigrationId" text PRIMARY KEY);
CREATE TABLE gateway_schema_version (id bigint PRIMARY KEY, version_id bigint, is_applied boolean, tstamp timestamptz)`); err != nil {
			t.Fatal(err)
		}
		if state, err := DatabaseState(ctx, dsn); err != nil || state != StateCurrentV2 {
			t.Fatalf("state=%q err=%v", state, err)
		}
	})

	t.Run("wrong config trigger binding", func(t *testing.T) {
		ctx, dsn, base := isolatedV2Database(t)
		if err := InitializeV2(ctx, dsn, bytes.NewBuffer(nil)); err != nil {
			t.Fatal(err)
		}
		if _, err := base.Exec(ctx, `
DROP TRIGGER trg_gateway_ip_blacklist_config_event ON gateway_ip_blacklist;
CREATE TRIGGER trg_gateway_ip_blacklist_config_event
AFTER INSERT OR UPDATE OR DELETE ON gateway_ip_blacklist
FOR EACH ROW EXECUTE FUNCTION gateway_emit_config_event('id', 'website')`); err != nil {
			t.Fatal(err)
		}
		if state, err := DatabaseState(ctx, dsn); err != nil || state != StateUnsupported {
			t.Fatalf("state=%q err=%v", state, err)
		}
	})
}

func TestV2IPBlacklistEventsAndGatewayRole(t *testing.T) {
	ctx, dsn, base := isolatedV2Database(t)
	if err := InitializeV2(ctx, dsn, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	roles, err := os.ReadFile(filepath.Join("..", "..", "deploy", "postgres", "roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, string(roles)); err != nil {
		t.Fatalf("apply roles.sql: %v", err)
	}
	var schema string
	if err := base.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, `GRANT USAGE ON SCHEMA `+pgx.Identifier{schema}.Sanitize()+` TO yaginx_gateway`); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, `SET ROLE yaginx_gateway`); err != nil {
		t.Fatalf("set gateway role: %v", err)
	}
	t.Cleanup(func() { _, _ = base.Exec(ctx, `RESET ROLE`) })

	var id int64
	if err := base.QueryRow(ctx, `INSERT INTO gateway_ip_blacklist(cidr) VALUES ('203.0.113.0/24') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("gateway insert blacklist: %v", err)
	}
	if _, err := base.Exec(ctx, `UPDATE gateway_ip_blacklist SET blocked_count = 1 WHERE id = $1`, id); err != nil {
		t.Fatalf("gateway update blacklist: %v", err)
	}
	if _, err := base.Exec(ctx, `DELETE FROM gateway_ip_blacklist WHERE id = $1`, id); err != nil {
		t.Fatalf("gateway delete blacklist: %v", err)
	}
	var events int
	if err := base.QueryRow(ctx, `
SELECT count(*)
FROM gateway_config_event
WHERE resource_type = 'gateway_ip_blacklist'
  AND resource_id = $1
  AND operation IN ('insert', 'update', 'delete')`, strconv.FormatInt(id, 10)).Scan(&events); err != nil {
		t.Fatalf("read blacklist events: %v", err)
	}
	if events != 3 {
		t.Fatalf("blacklist events=%d want=3", events)
	}
	var runtimeMaterialCount int
	if err := base.QueryRow(ctx, `SELECT count(*) FROM gateway_runtime_material`).Scan(&runtimeMaterialCount); err != nil {
		t.Fatalf("gateway read runtime material: %v", err)
	}
	if _, err := base.Exec(ctx, `CREATE TABLE gateway_role_ddl_probe (id integer)`); err == nil {
		t.Fatal("gateway role unexpectedly has DDL permission")
	}
}

func isolatedV2Database(t *testing.T) (context.Context, string, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close(ctx) })
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	schema := "yaginx_v2_" + hex.EncodeToString(raw)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		base.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})
	isolated, err := WithSearchPath(dsn, schema)
	if err != nil {
		base.Close(ctx)
		t.Fatal(err)
	}
	if _, err := base.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		base.Close(ctx)
		t.Fatal(err)
	}
	return ctx, isolated, base
}
