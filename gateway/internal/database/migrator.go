package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/pressly/goose/v3"

	v2migrations "yaginx-gateway/migrations/v2"
)

// MigrationLockKey is part of the database contract. It must never be derived
// from a string: derivation changes between PostgreSQL versions and could let
// two migrators alter a schema concurrently.
const MigrationLockKey int64 = 6431500123034371919
const migrationLockSQL = "SELECT pg_advisory_lock(6431500123034371919)"
const migrationUnlockSQL = "SELECT pg_advisory_unlock(6431500123034371919)"
const latestV2Migration int64 = 13

type State string

const (
	StateUninitialized    State = "uninitialized"
	StateCurrentV2        State = "current-v2"
	StateCurrentV2Pending State = "current-v2-with-pending-migrations"
	StateUnsupported      State = "unsupported"
)

type schemaObjectCounts struct {
	Tables    int
	Indexes   int
	Triggers  int
	Functions int
}

// InitializeV2 provisions a brand-new, empty database. Existing databases
// must use MigrateV2 so an operator never mistakes an upgrade for bootstrap.
func InitializeV2(ctx context.Context, databaseURL string, output io.Writer) error {
	db, err := openSQL(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return withMigrationLock(ctx, db, func() error {
		state, err := detectState(ctx, db)
		if err != nil {
			return err
		}
		if state != StateUninitialized {
			return fmt.Errorf("database initialization requires an empty database; found %s", state)
		}
		if err := applyV2Migrations(ctx, db, output); err != nil {
			return err
		}
		if state, err := detectState(ctx, db); err != nil || state != StateCurrentV2 || !v2MigrationsComplete(ctx, db) {
			if err != nil {
				return err
			}
			return fmt.Errorf("initialization completed without v2 schema marker")
		}
		if err := ValidateGatewaySchema(ctx, db); err != nil {
			return err
		}
		if err := ValidateAPICredentialSchema(ctx, db); err != nil {
			return err
		}
		counts, err := countSchemaObjects(ctx, db)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(
			output,
			"database initialized: current-v2 (tables=%d indexes=%d triggers=%d functions=%d); run gateway/deploy/postgres/roles.sql before starting the service\n",
			counts.Tables,
			counts.Indexes,
			counts.Triggers,
			counts.Functions,
		)
		return nil
	})
}

// MigrateV2 upgrades an existing v2 schema.
func MigrateV2(ctx context.Context, databaseURL string, output io.Writer) error {
	db, err := openSQL(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return withMigrationLock(ctx, db, func() error {
		state, err := detectState(ctx, db)
		if err != nil {
			return err
		}
		switch state {
		case StateCurrentV2, StateCurrentV2Pending:
			// Existing v2 installations may have only the baseline migration.
		case StateUninitialized:
			return fmt.Errorf("database is uninitialized; run `yaginx database init`")
		default:
			return fmt.Errorf("database schema is unsupported; only current-v2 may be upgraded")
		}
		if state == StateCurrentV2Pending {
			progress, progressErr := v2MigrationProgress(ctx, db)
			if progressErr != nil {
				return progressErr
			}
			pending := make([]int64, 0, latestV2Migration-progress)
			for version := progress + 1; version <= latestV2Migration; version++ {
				pending = append(pending, version)
			}
			_, _ = fmt.Fprintf(output, "database schema is current-v2; pending migrations: %s\n", formatMigrationIDs(pending))
		}
		if err := applyV2Migrations(ctx, db, output); err != nil {
			return err
		}
		if state, err := detectState(ctx, db); err != nil || state != StateCurrentV2 || !v2MigrationsComplete(ctx, db) {
			if err != nil {
				return err
			}
			return fmt.Errorf("migration completed without v2 schema marker")
		}
		if err := ValidateGatewaySchema(ctx, db); err != nil {
			return err
		}
		return ValidateAPICredentialSchema(ctx, db)
	})
}

func formatMigrationIDs(ids []int64) string {
	if len(ids) == 0 {
		return "none"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	return strings.Join(parts, ",")
}

func DatabaseState(ctx context.Context, databaseURL string) (State, error) {
	db, err := openSQL(ctx, databaseURL)
	if err != nil {
		return "", err
	}
	defer db.Close()
	return detectState(ctx, db)
}

// DatabaseNeedsMigration reports whether the database needs the explicit
// bootstrap/migrator path before the runtime service can start.
func DatabaseNeedsMigration(ctx context.Context, databaseURL string) (bool, error) {
	db, err := openSQL(ctx, databaseURL)
	if err != nil {
		return false, err
	}
	defer db.Close()
	state, err := detectState(ctx, db)
	if err != nil {
		return false, err
	}
	switch state {
	case StateUninitialized:
		return true, nil
	case StateCurrentV2, StateCurrentV2Pending:
		return !v2MigrationsComplete(ctx, db), nil
	case StateUnsupported:
		return false, fmt.Errorf("database schema is unsupported")
	default:
		return false, fmt.Errorf("unknown database state %q", state)
	}
}

func withMigrationLock(ctx context.Context, db *sql.DB, run func() error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration lock connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, migrationLockSQL); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), migrationUnlockSQL)
	return run()
}

func applyV2Migrations(ctx context.Context, db *sql.DB, output io.Writer) error {
	goose.SetBaseFS(v2migrations.Files)
	goose.SetTableName("yaginx_schema_migration")
	goose.SetLogger(log.New(output, "", 0))
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set v2 baseline dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("apply v2 migrations: %w", err)
	}
	return nil
}

func detectState(ctx context.Context, db *sql.DB) (State, error) {
	v2, err := schemaTableExists(ctx, db, "yaginx_schema_version")
	if err != nil {
		return "", fmt.Errorf("inspect v2 schema marker: %w", err)
	}
	v2Tracker, err := schemaTableExists(ctx, db, "yaginx_schema_migration")
	if err != nil {
		return "", fmt.Errorf("inspect v2 migration tracker: %w", err)
	}
	if v2 {
		if !v2Tracker {
			return StateUnsupported, nil
		}
		if err := validateV2Marker(ctx, db); err != nil {
			return StateUnsupported, nil
		}
		progress, err := v2MigrationProgress(ctx, db)
		if err != nil {
			return StateUnsupported, nil
		}
		if err := ValidateGatewaySchema(ctx, db); err != nil {
			return StateUnsupported, nil
		}
		if progress < latestV2Migration {
			return StateCurrentV2Pending, nil
		}
		if err := ValidateV2Schema(ctx, db); err != nil {
			return StateUnsupported, nil
		}
		return StateCurrentV2, nil
	}
	if v2Tracker {
		return StateUnsupported, nil
	}
	hasObjects, err := schemaHasUserObjects(ctx, db)
	if err != nil {
		return "", err
	}
	if !hasObjects {
		return StateUninitialized, nil
	}
	return StateUnsupported, nil
}

func schemaTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = current_schema()
      AND table_name = $1
      AND table_type = 'BASE TABLE'
)`, table).Scan(&exists)
	return exists, err
}

func schemaHasUserObjects(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = current_schema()
      AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f', 'i')
    UNION ALL
    SELECT 1
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = current_schema()
    UNION ALL
    SELECT 1
    FROM pg_type t
    JOIN pg_namespace n ON n.oid = t.typnamespace
    WHERE n.nspname = current_schema()
      AND t.typtype IN ('b', 'd', 'e', 'r')
    UNION ALL
    SELECT 1
    FROM pg_trigger t
    JOIN pg_class c ON c.oid = t.tgrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = current_schema()
      AND NOT t.tgisinternal
)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect database objects: %w", err)
	}
	return exists, nil
}

func v2MigrationProgress(ctx context.Context, db *sql.DB) (int64, error) {
	columns, err := tableColumns(ctx, db, "yaginx_schema_migration")
	if err != nil {
		return -1, err
	}
	for _, column := range []string{"id", "version_id", "is_applied", "tstamp"} {
		if _, ok := columns[column]; !ok {
			return -1, fmt.Errorf("yaginx_schema_migration is missing tracker column %q", column)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT version_id, is_applied FROM yaginx_schema_migration ORDER BY id`)
	if err != nil {
		return -1, fmt.Errorf("read yaginx_schema_migration lineage: %w", err)
	}
	defer rows.Close()
	var expected int64
	var last int64 = -1
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil {
			return -1, err
		}
		if version != expected {
			return -1, fmt.Errorf("yaginx_schema_migration lineage[%d] = %d, want %d", expected, version, expected)
		}
		if !applied {
			return -1, fmt.Errorf("yaginx_schema_migration has unapplied version %d", version)
		}
		if version > latestV2Migration {
			return -1, fmt.Errorf("yaginx_schema_migration has unknown version %d", version)
		}
		last = version
		expected++
	}
	if err := rows.Err(); err != nil {
		return -1, err
	}
	if last < 0 {
		return -1, fmt.Errorf("yaginx_schema_migration has no applied versions")
	}
	return last, nil
}

func v2MigrationsComplete(ctx context.Context, db *sql.DB) bool {
	progress, err := v2MigrationProgress(ctx, db)
	return err == nil && progress == latestV2Migration
}

func validateV2Marker(ctx context.Context, db *sql.DB) error {
	var count, minimum, maximum int
	if err := db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(min(generation), 0), COALESCE(max(generation), 0)
FROM yaginx_schema_version`).Scan(&count, &minimum, &maximum); err != nil {
		return err
	}
	if count != 1 || minimum != 2 || maximum != 2 {
		return fmt.Errorf("invalid v2 schema marker")
	}
	return nil
}

func countSchemaObjects(ctx context.Context, db *sql.DB) (schemaObjectCounts, error) {
	var counts schemaObjectCounts
	err := db.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM information_schema.tables
   WHERE table_schema=current_schema() AND table_type='BASE TABLE'),
  (SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema()),
  (SELECT count(*) FROM pg_trigger t
   JOIN pg_class c ON c.oid=t.tgrelid
   JOIN pg_namespace n ON n.oid=c.relnamespace
   WHERE n.nspname=current_schema() AND NOT t.tgisinternal),
  (SELECT count(*) FROM pg_proc p
   JOIN pg_namespace n ON n.oid=p.pronamespace
   WHERE n.nspname=current_schema())`).Scan(
		&counts.Tables,
		&counts.Indexes,
		&counts.Triggers,
		&counts.Functions,
	)
	if err != nil {
		return schemaObjectCounts{}, fmt.Errorf("count v2 schema objects: %w", err)
	}
	return counts, nil
}
