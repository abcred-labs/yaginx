// Package acme contains cross-node ACME coordination primitives.
package acme

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/platform/hostnorm"
)

// DomainLock owns a dedicated PostgreSQL session for the entire external ACME
// order. Releasing its connection releases the advisory lock even on crash.
type DomainLock struct {
	connection *pgxpool.Conn
	key        string
}

func AcquireDomainLock(ctx context.Context, pool *pgxpool.Pool, domain string) (*DomainLock, bool, error) {
	var ok bool
	domain, ok = hostnorm.Domain(domain)
	if !ok {
		return nil, false, fmt.Errorf("invalid ACME domain")
	}
	return acquireLock(ctx, pool, "yaginx:acme:", domain)
}

// AcquireAccountLock serializes the initial registration/persistence of an
// ACME account shared by every approved node. It is intentionally separate
// from a domain lock so unrelated domains can still renew in parallel once
// the account exists.
func AcquireAccountLock(ctx context.Context, pool *pgxpool.Pool, directoryURL string) (*DomainLock, bool, error) {
	directoryURL = strings.TrimSpace(directoryURL)
	if directoryURL == "" {
		return nil, false, fmt.Errorf("invalid ACME directory URL")
	}
	return acquireLock(ctx, pool, "yaginx:acme-account:", directoryURL)
}

func acquireLock(ctx context.Context, pool *pgxpool.Pool, namespace, key string) (*DomainLock, bool, error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	err = connection.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1 || $2, 0))", namespace, key).Scan(&acquired)
	if err != nil {
		connection.Release()
		return nil, false, err
	}
	if !acquired {
		connection.Release()
		return nil, false, nil
	}
	return &DomainLock{connection: connection, key: namespace + key}, true, nil
}
func (l *DomainLock) Release(ctx context.Context) {
	if l == nil || l.connection == nil {
		return
	}
	_, _ = l.connection.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", l.key)
	l.connection.Release()
	l.connection = nil
}

// normalizeDomain remains as a package-local compatibility shim for callers
// and tests while normalization is owned by platform/hostnorm.
func normalizeDomain(value string) string {
	value, ok := hostnorm.Domain(value)
	if !ok {
		return ""
	}
	return value
}
