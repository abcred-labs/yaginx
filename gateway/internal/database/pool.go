package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

var ErrUnavailable = errors.New("PostgreSQL unavailable")

type PoolOptions struct {
	MinimumServerMajor    int
	MaxConnections        int
	MinConnections        int
	ConnectTimeout        time.Duration
	StatementTimeout      time.Duration
	HealthQueryTimeout    time.Duration
	MaxConnectionLifetime time.Duration
	MaxConnectionIdleTime time.Duration
}

// NewRuntimePool materializes the database portion of the runtime contract.
// It performs the version check before handing the pool to request paths.
func NewRuntimePool(ctx context.Context, databaseURL string, options PoolOptions) (*pgxpool.Pool, error) {
	if options.MinimumServerMajor < 1 {
		return nil, fmt.Errorf("minimum PostgreSQL major version must be positive")
	}
	if options.MinConnections < 0 || options.MaxConnections < 1 || options.MinConnections > options.MaxConnections {
		return nil, fmt.Errorf("invalid pool sizes min=%d max=%d", options.MinConnections, options.MaxConnections)
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	poolConfig.MinConns = int32(options.MinConnections)
	poolConfig.MaxConns = int32(options.MaxConnections)
	poolConfig.MaxConnLifetime = options.MaxConnectionLifetime
	poolConfig.MaxConnIdleTime = options.MaxConnectionIdleTime
	poolConfig.PingTimeout = options.HealthQueryTimeout
	poolConfig.ConnConfig.ConnectTimeout = options.ConnectTimeout
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(options.StatementTimeout.Milliseconds(), 10)
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: create PostgreSQL pool: %v", ErrUnavailable, err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, options.HealthQueryTimeout)
	defer cancel()
	var version string
	if err := pool.QueryRow(checkCtx, "SHOW server_version_num").Scan(&version); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: query PostgreSQL server version: %v", ErrUnavailable, err)
	}
	number, err := strconv.Atoi(version)
	if err != nil || number/10000 < options.MinimumServerMajor {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL server version %q is below required major %d", version, options.MinimumServerMajor)
	}
	return pool, nil
}

// Check verifies connectivity without applying migrations or modifying application data.
func Check(ctx context.Context, databaseURL string, minimumServerMajor int) error {
	pool, err := NewRuntimePool(ctx, databaseURL, PoolOptions{MinimumServerMajor: minimumServerMajor, MaxConnections: 4, MinConnections: 0, ConnectTimeout: 10 * time.Second, StatementTimeout: 15 * time.Second, HealthQueryTimeout: 2 * time.Second, MaxConnectionLifetime: 30 * time.Minute, MaxConnectionIdleTime: 5 * time.Minute})
	if err != nil {
		return err
	}
	defer pool.Close()
	return nil
}

// WithSearchPath derives a connection string for an explicitly named schema.
// It accepts either libpq key/value or URL input and never changes roles,
// databases, or public. Disposable integration tests and migration tooling use
// it to ensure all DDL and data stay inside their named schema.
func WithSearchPath(databaseURL, schema string) (string, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return "", err
	}
	params := url.Values{}
	for key, value := range config.RuntimeParams {
		params.Set(key, value)
	}
	params.Set("search_path", pgx.Identifier{schema}.Sanitize())
	if config.TLSConfig == nil {
		params.Set("sslmode", "disable")
	}
	value := &url.URL{Scheme: "postgres", User: url.UserPassword(config.User, config.Password), Host: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), Path: config.Database, RawQuery: params.Encode()}
	return value.String(), nil
}

func openSQL(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	// Migration locking occupies one session while schema inspection and Goose
	// use the shared DB handle, which needs a second connection to avoid a
	// self-deadlock.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return db, nil
}

func openReadOnlySQL(ctx context.Context, config *pgx.ConnConfig) (*sql.DB, error) {
	if config == nil {
		return nil, fmt.Errorf("PostgreSQL configuration is required")
	}
	copy := config.Copy()
	if copy.RuntimeParams == nil {
		copy.RuntimeParams = map[string]string{}
	}
	copy.RuntimeParams["default_transaction_read_only"] = "on"
	db, err := sql.Open("pgx", stdlib.RegisterConnConfig(copy))
	if err != nil {
		return nil, fmt.Errorf("open read-only PostgreSQL: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping read-only PostgreSQL: %w", err)
	}
	return db, nil
}
