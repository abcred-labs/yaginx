//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
)

func TestRetiredUpgradeSettingsReadApplyAndSave(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(context.Background())
	schema := "upgrade_compat_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolated)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	legacy := `{"upgrade":{"enabled":true,"minPublishedAge":false,"checkInterval":"invalid","downloadTimeout":"7m"}}`
	if _, err := pool.Exec(ctx, `INSERT INTO gateway_system_setting(setting_key,value,content_type) VALUES('runtime',$1::jsonb,'application/json')`, legacy); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	loaded, err := store.GetRuntimeSetting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Warnings) != 3 {
		t.Fatalf("shared load warnings=%v", loaded.Warnings)
	}
	assertManualRuntimeSetting(t, loaded)
	var stillEnabled bool
	if err := pool.QueryRow(ctx, `SELECT (value->'upgrade'->>'enabled')::boolean FROM gateway_system_setting WHERE setting_key='runtime'`).Scan(&stillEnabled); err != nil || !stillEnabled {
		t.Fatalf("reading changed stored legacy settings: enabled=%t err=%v", stillEnabled, err)
	}
	values, err := config.NewRuntimeValues(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ApplySharedRuntime(loaded.Config, values); err != nil {
		t.Fatalf("apply database settings: %v", err)
	}
	if values["configVersion"] != float64(1) {
		t.Fatalf("configVersion changed: %v", values["configVersion"])
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(legacy), &input); err != nil {
		t.Fatal(err)
	}
	saved, err := store.SetRuntimeSetting(ctx, management.RuntimeSettingUpsertRequest{Config: input, ExpectedRevision: &loaded.Revision})
	if err != nil {
		t.Fatal(err)
	}
	assertManualRuntimeSetting(t, saved)
	if len(saved.Warnings) != 0 {
		t.Fatalf("saved settings still contain legacy fields: %v", saved.Warnings)
	}
	var hasLegacy bool
	if err := pool.QueryRow(ctx, `SELECT value->'upgrade' ?| ARRAY['enabled','minPublishedAge','checkInterval'] FROM gateway_system_setting WHERE setting_key='runtime'`).Scan(&hasLegacy); err != nil || hasLegacy {
		t.Fatalf("legacy fields persisted: %t err=%v", hasLegacy, err)
	}
}

func assertManualRuntimeSetting(t *testing.T, setting *management.RuntimeSetting) {
	t.Helper()
	upgrade := setting.Config["upgrade"].(map[string]any)
	if len(upgrade) != 1 || upgrade["downloadTimeout"] != "7m" {
		t.Fatalf("unexpected manual upgrade settings: %#v", upgrade)
	}
	exported, err := json.Marshal(setting)
	if err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{"minPublishedAge", "checkInterval", `"enabled"`, `"Warnings"`} {
		if bytes.Contains(exported, []byte(retired)) {
			t.Fatalf("export contains %s: %s", retired, exported)
		}
	}
}
