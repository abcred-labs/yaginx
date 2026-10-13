package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"yaginx-gateway/internal/management"

	"github.com/jackc/pgx/v5"
)

// Refresh sessions: one row per logged-in browser/device. The row tracks a
// SHA-256 hash of an opaque refresh secret (never the secret itself), plus an
// idle deadline that is pushed forward on every successful refresh and an
// absolute deadline that never moves. Both sides of the caller supply the TTLs
// so the account/handler package stays the only place that knows about
// "remember me" vs. default durations.

// CreateAccountSession records a new refresh session issued at login.
func (p *Postgres) CreateAccountSession(ctx context.Context, accountID int64, tokenHash []byte, remembered bool, userAgent, clientIP string, idleTTL, absoluteTTL time.Duration) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO account_session (account_id, refresh_token_hash, remembered, user_agent, client_ip, idle_expires_at, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5, clock_timestamp() + make_interval(secs => $6), clock_timestamp() + make_interval(secs => $7))
		RETURNING id
	`, accountID, tokenHash, remembered, nullableText(userAgent), nullableText(clientIP), idleTTL.Seconds(), absoluteTTL.Seconds()).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// RotateAccountSession validates the presented secret's hash against the
// stored one and, if the session is neither revoked nor past either
// deadline, rotates in the new hash and pushes the idle deadline forward by
// idleTTL or rememberedIdleTTL, picking whichever matches the session's own
// stored "remembered" flag (set once at login and never changed by a
// refresh) — the caller does not need to know or re-derive it. It returns
// management.ErrNotFound when the session is missing, revoked, or expired —
// callers must treat that as "refresh failed", not as a generic lookup miss.
func (p *Postgres) RotateAccountSession(ctx context.Context, sessionID int64, oldHash, newHash []byte, idleTTL, rememberedIdleTTL time.Duration) (accountID int64, email string, remembered bool, err error) {
	err = p.pool.QueryRow(ctx, `
		UPDATE account_session
		SET refresh_token_hash = $3,
		    last_used_at = clock_timestamp(),
		    idle_expires_at = clock_timestamp() + CASE WHEN remembered THEN make_interval(secs => $5) ELSE make_interval(secs => $4) END
		WHERE id = $1
		  AND refresh_token_hash = $2
		  AND revoked_at IS NULL
		  AND idle_expires_at > clock_timestamp()
		  AND absolute_expires_at > clock_timestamp()
		RETURNING account_id, remembered
	`, sessionID, oldHash, newHash, idleTTL.Seconds(), rememberedIdleTTL.Seconds()).Scan(&accountID, &remembered)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, fmt.Errorf("%w: refresh session is missing, revoked, or expired", management.ErrNotFound)
	}
	if err != nil {
		return 0, "", false, err
	}
	if err := p.pool.QueryRow(ctx, "SELECT email FROM account WHERE id = $1", accountID).Scan(&email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", false, fmt.Errorf("%w: account no longer exists", management.ErrNotFound)
		}
		return 0, "", false, err
	}
	return accountID, email, remembered, nil
}

// RevokeAccountSessionByToken best-effort revokes the session the refresh
// cookie names, verifying the secret's hash first. Used by Logout, which
// always clears cookies and returns success regardless of whether a matching
// row existed.
func (p *Postgres) RevokeAccountSessionByToken(ctx context.Context, sessionID int64, tokenHash []byte) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE account_session SET revoked_at = clock_timestamp()
		WHERE id = $1 AND refresh_token_hash = $2 AND revoked_at IS NULL
	`, sessionID, tokenHash)
	return err
}

// RevokeAccountSession revokes one of accountID's own sessions. Scoping to
// accountID keeps a caller from revoking another account's session by
// guessing a session id.
func (p *Postgres) RevokeAccountSession(ctx context.Context, accountID, sessionID int64) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE account_session SET revoked_at = clock_timestamp()
		WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL
	`, sessionID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: session not found", management.ErrNotFound)
	}
	return nil
}

// RevokeOtherAccountSessions revokes every session for accountID except
// keepSessionID ("sign out of all other devices"). Revoking zero rows is not
// an error — there may simply be nothing else to sign out of.
func (p *Postgres) RevokeOtherAccountSessions(ctx context.Context, accountID, keepSessionID int64) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE account_session SET revoked_at = clock_timestamp()
		WHERE account_id = $1 AND id <> $2 AND revoked_at IS NULL
	`, accountID, keepSessionID)
	return err
}

// ListAccountSessions lists accountID's active (unrevoked, unexpired)
// sessions, most recently used first. Current is left false here; the
// handler fills it in from the caller's own authenticated session id.
func (p *Postgres) ListAccountSessions(ctx context.Context, accountID int64) ([]management.SessionSummary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, remembered, COALESCE(user_agent, ''), COALESCE(client_ip, ''), issued_at, last_used_at, idle_expires_at, absolute_expires_at
		FROM account_session
		WHERE account_id = $1 AND revoked_at IS NULL AND idle_expires_at > clock_timestamp() AND absolute_expires_at > clock_timestamp()
		ORDER BY last_used_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []management.SessionSummary
	for rows.Next() {
		var s management.SessionSummary
		var id int64
		if err := rows.Scan(&id, &s.Remembered, &s.UserAgent, &s.ClientIP, &s.IssuedAt, &s.LastUsedAt, &s.IdleExpiresAt, &s.AbsoluteExpiresAt); err != nil {
			return nil, err
		}
		s.ID = management.ID(id)
		out = append(out, s)
	}
	return out, rows.Err()
}

// PruneExpiredAccountSessions deletes rows that can no longer authenticate
// anything: past either deadline, or revoked more than a day ago (kept
// briefly so a near-simultaneous reuse attempt still has a row to reject
// against, rather than looking like "unknown session").
func (p *Postgres) PruneExpiredAccountSessions(ctx context.Context, now time.Time) error {
	_, err := p.pool.Exec(ctx, `
		DELETE FROM account_session
		WHERE idle_expires_at <= $1
		   OR absolute_expires_at <= $1
		   OR (revoked_at IS NOT NULL AND revoked_at <= $1 - interval '1 day')
	`, now.UTC())
	return err
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
