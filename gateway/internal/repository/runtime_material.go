package repository

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RuntimeMaterial is shared by every gateway instance through PostgreSQL.
// Database read access therefore grants access to JWT signing material.
type RuntimeMaterial struct {
	JWTCurrent  []byte
	JWTPrevious []byte
}

func (p *Postgres) LoadOrCreateRuntimeMaterial(ctx context.Context) (RuntimeMaterial, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return RuntimeMaterial{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('yaginx:runtime-material', 0))"); err != nil {
		return RuntimeMaterial{}, err
	}
	material, found, err := loadRuntimeMaterial(ctx, tx)
	if err != nil {
		return RuntimeMaterial{}, err
	}
	if !found {
		current, err := newJWTKey()
		if err != nil {
			return RuntimeMaterial{}, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO gateway_runtime_material(singleton_id,jwt_current,updated_at) VALUES(1,$1,clock_timestamp())", current); err != nil {
			return RuntimeMaterial{}, err
		}
		material = RuntimeMaterial{JWTCurrent: []byte(current)}
	}
	if err := tx.Commit(ctx); err != nil {
		return RuntimeMaterial{}, err
	}
	return material, nil
}

func (p *Postgres) LoadRuntimeMaterial(ctx context.Context) (RuntimeMaterial, error) {
	material, found, err := loadRuntimeMaterial(ctx, p.pool)
	if err != nil {
		return RuntimeMaterial{}, err
	}
	if !found {
		return RuntimeMaterial{}, fmt.Errorf("runtime material is not initialized")
	}
	return material, nil
}

type runtimeMaterialQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadRuntimeMaterial(ctx context.Context, query runtimeMaterialQuerier) (RuntimeMaterial, bool, error) {
	var current string
	var previous *string
	err := query.QueryRow(ctx, "SELECT jwt_current,jwt_previous FROM gateway_runtime_material WHERE singleton_id=1").Scan(&current, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeMaterial{}, false, nil
	}
	if err != nil {
		return RuntimeMaterial{}, false, err
	}
	material := RuntimeMaterial{JWTCurrent: []byte(current)}
	if previous != nil {
		material.JWTPrevious = []byte(*previous)
	}
	return material, true, nil
}

func newJWTKey() (string, error) {
	value := make([]byte, 48)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(value), nil
}
