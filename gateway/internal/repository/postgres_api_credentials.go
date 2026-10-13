package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"yaginx-gateway/internal/management"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Database-backed API credentials: the tokens non-browser clients (today the
// MCP endpoint) authenticate with.

// ValidateAPIToken checks the active credential hash. The raw token is never persisted.
func (p *Postgres) ValidateAPIToken(ctx context.Context, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	var stored []byte
	err := p.pool.QueryRow(ctx, "SELECT token_hash FROM gateway_api_credential WHERE status='active' AND token_hash=$1", sum[:]).Scan(&stored)
	return err == nil && subtle.ConstantTimeCompare(stored, sum[:]) == 1
}

func newAPIToken() (string, []byte, string, error) {
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, "", err
	}
	token := "ygx_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	prefix := token
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return token, sum[:], prefix, nil
}

func (p *Postgres) ListAPICredentials(ctx context.Context) ([]management.APICredential, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,name,purpose,token_prefix,status,extract(epoch FROM created_at)::bigint,extract(epoch FROM updated_at)::bigint,extract(epoch FROM revoked_at)::bigint
FROM gateway_api_credential ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]management.APICredential, 0)
	for rows.Next() {
		item, err := scanAPICredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateAPICredential(ctx context.Context, input management.APICredentialCreateRequest) (management.APICredentialSecret, error) {
	name := strings.TrimSpace(input.Name)
	purpose := strings.TrimSpace(input.Purpose)
	if name == "" || purpose == "" {
		return management.APICredentialSecret{}, fmt.Errorf("credential name and purpose are required")
	}
	token, hash, prefix, err := newAPIToken()
	if err != nil {
		return management.APICredentialSecret{}, err
	}
	item, err := scanAPICredentialRow(p.pool.QueryRow(ctx, `INSERT INTO gateway_api_credential(name,purpose,token_prefix,token_hash,status)
VALUES($1,$2,$3,$4,'active')
RETURNING id,name,purpose,token_prefix,status,extract(epoch FROM created_at)::bigint,extract(epoch FROM updated_at)::bigint,extract(epoch FROM revoked_at)::bigint`, name, purpose, prefix, hash))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return management.APICredentialSecret{}, management.ErrConflict
		}
		return management.APICredentialSecret{}, err
	}
	return management.APICredentialSecret{APICredential: item, Token: token}, nil
}

func (p *Postgres) ResetAPICredential(ctx context.Context, key string) (management.APICredentialSecret, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return management.APICredentialSecret{}, management.ErrNotFound
	}
	token, hash, prefix, err := newAPIToken()
	if err != nil {
		return management.APICredentialSecret{}, err
	}
	item, err := scanAPICredentialRow(p.pool.QueryRow(ctx, `UPDATE gateway_api_credential
SET token_prefix=$2,token_hash=$3,updated_at=clock_timestamp()
WHERE id=$1 AND status='active'
RETURNING id,name,purpose,token_prefix,status,extract(epoch FROM created_at)::bigint,extract(epoch FROM updated_at)::bigint,extract(epoch FROM revoked_at)::bigint`, id, prefix, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return management.APICredentialSecret{}, credentialMutationError(ctx, p, id)
	}
	if err != nil {
		return management.APICredentialSecret{}, err
	}
	return management.APICredentialSecret{APICredential: item, Token: token}, nil
}

func (p *Postgres) RevokeAPICredential(ctx context.Context, key string) (management.APICredential, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return management.APICredential{}, management.ErrNotFound
	}
	item, err := scanAPICredentialRow(p.pool.QueryRow(ctx, `UPDATE gateway_api_credential
SET status='revoked',revoked_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=$1 AND status='active'
RETURNING id,name,purpose,token_prefix,status,extract(epoch FROM created_at)::bigint,extract(epoch FROM updated_at)::bigint,extract(epoch FROM revoked_at)::bigint`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return management.APICredential{}, credentialMutationError(ctx, p, id)
	}
	if err != nil {
		return management.APICredential{}, err
	}
	return item, nil
}

type apiCredentialRow interface {
	Scan(dest ...any) error
}

func scanAPICredentialRow(row apiCredentialRow) (management.APICredential, error) {
	var id int64
	var item management.APICredential
	err := row.Scan(&id, &item.Name, &item.Purpose, &item.TokenPrefix, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.RevokedAt)
	item.ID = management.ID(id)
	return item, err
}

func scanAPICredential(rows interface{ Scan(dest ...any) error }) (management.APICredential, error) {
	return scanAPICredentialRow(rows)
}

func credentialMutationError(ctx context.Context, p *Postgres, id int64) error {
	var status string
	err := p.pool.QueryRow(ctx, "SELECT status FROM gateway_api_credential WHERE id=$1", id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "revoked" {
		return management.ErrConflict
	}
	return management.ErrNotFound
}
