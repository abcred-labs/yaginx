package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Automatic ACME issuance is rate limited per domain:
//   - at most ACMEAutoMaxAttemptsPerDay attempts per UTC day, spaced at least
//     ACMEAutoMinInterval apart (so a failing preflight cannot spin);
//   - after ACMEAutoMaxFailureDays consecutive days that ended without a
//     success, automatic renewal stops for good until an operator-triggered
//     request succeeds (or the operator re-enables auto-request).
//
// Operator-triggered ("manual") requests are never limited; they are recorded
// so a manual success can clear the stopped state.
const (
	ACMEAutoMaxAttemptsPerDay = 3
	ACMEAutoMaxFailureDays    = 3
	ACMEAutoMinInterval       = 10 * time.Minute

	ACMETriggerAuto   = "auto"
	ACMETriggerManual = "manual"

	ACMEAttemptSuccess = "success"
	ACMEAttemptFailure = "failure"
	ACMEAttemptSkipped = "skipped"

	acmeAutoDenyStopped  = "auto_stopped"
	acmeAutoDenyDaily    = "daily_limit"
	acmeAutoDenyTooSoon  = "too_soon"
	acmeAutoDenyBackoff  = "backoff"
	acmeAttemptRetention = 30 * 24 * time.Hour
)

const acmeUTCDayStart = `(date_trunc('day', clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')`

// BeginACMEAttempt records the start of an issuance attempt. For the automatic
// trigger it first enforces the stop flag, daily cap and minimum spacing; when
// denied, allowed is false, reason names the rule, and nothing is recorded.
func (p *Postgres) BeginACMEAttempt(ctx context.Context, domain, trigger string) (attemptID int64, allowed bool, reason string, err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, false, "", err
	}
	defer tx.Rollback(ctx)
	if trigger == ACMETriggerAuto {
		if _, err = tx.Exec(ctx, "INSERT INTO gateway_acme_auto_state(domain_name) VALUES($1) ON CONFLICT DO NOTHING", domain); err != nil {
			return 0, false, "", err
		}
		// The state row lock serializes concurrent nodes deciding on one domain.
		var stopped bool
		if err = tx.QueryRow(ctx, "SELECT stopped_at IS NOT NULL FROM gateway_acme_auto_state WHERE domain_name=$1 FOR UPDATE", domain).Scan(&stopped); err != nil {
			return 0, false, "", err
		}
		if stopped {
			return 0, false, acmeAutoDenyStopped, nil
		}
		var today int
		var tooSoon bool
		if err = tx.QueryRow(ctx, `SELECT
  count(*) FILTER (WHERE started_at >= `+acmeUTCDayStart+`),
  coalesce(bool_or(started_at > clock_timestamp() - make_interval(secs => $2)), false)
FROM gateway_acme_attempt WHERE domain_name=$1 AND trigger='auto' AND outcome<>'skipped'`, domain, ACMEAutoMinInterval.Seconds()).Scan(&today, &tooSoon); err != nil {
			return 0, false, "", err
		}
		if today >= ACMEAutoMaxAttemptsPerDay {
			return 0, false, acmeAutoDenyDaily, nil
		}
		if tooSoon {
			return 0, false, acmeAutoDenyTooSoon, nil
		}
		// An order waiting out its retry backoff must not be probed again (and
		// must not churn attempt rows) before it is due.
		var backoff bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM gateway_certificate_order WHERE domain_name=$1 AND status IN ('pending','retry_wait') AND next_attempt_at > clock_timestamp())", domain).Scan(&backoff); err != nil {
			return 0, false, "", err
		}
		if backoff {
			return 0, false, acmeAutoDenyBackoff, nil
		}
	}
	if err = tx.QueryRow(ctx, "INSERT INTO gateway_acme_attempt(domain_name,trigger) VALUES($1,$2) RETURNING id", domain, trigger).Scan(&attemptID); err != nil {
		return 0, false, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, false, "", err
	}
	return attemptID, true, "", nil
}

// FinishACMEAttempt closes an attempt and maintains the automatic-renewal
// state: any success clears it; an automatic failure advances the
// consecutive-failure-day counter and may stop automatic renewal. stopped
// reports that this very failure tripped the stop.
func (p *Postgres) FinishACMEAttempt(ctx context.Context, attemptID int64, domain, trigger, outcome string, orderID int64, message string) (stopped bool, err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var order any
	if orderID > 0 {
		order = orderID
	}
	if _, err = tx.Exec(ctx, "UPDATE gateway_acme_attempt SET outcome=$2,order_id=$3,message=$4,finished_at=clock_timestamp() WHERE id=$1", attemptID, outcome, order, sanitizeACMEError(message)); err != nil {
		return false, err
	}
	switch {
	case outcome == ACMEAttemptSuccess:
		if _, err = tx.Exec(ctx, "DELETE FROM gateway_acme_auto_state WHERE domain_name=$1", domain); err != nil {
			return false, err
		}
	case outcome == ACMEAttemptFailure && trigger == ACMETriggerAuto:
		if _, err = tx.Exec(ctx, "INSERT INTO gateway_acme_auto_state(domain_name) VALUES($1) ON CONFLICT DO NOTHING", domain); err != nil {
			return false, err
		}
		var failDays int
		err = tx.QueryRow(ctx, `UPDATE gateway_acme_auto_state SET
  fail_days = CASE WHEN last_fail_day = d.day THEN fail_days WHEN last_fail_day = d.day - 1 THEN fail_days + 1 ELSE 1 END,
  last_fail_day = d.day,
  updated_at = clock_timestamp()
FROM (SELECT (clock_timestamp() AT TIME ZONE 'UTC')::date AS day) d
WHERE domain_name=$1 AND stopped_at IS NULL RETURNING fail_days`, domain).Scan(&failDays)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return false, err
		}
		if failDays >= ACMEAutoMaxFailureDays {
			if _, err = tx.Exec(ctx, "UPDATE gateway_acme_auto_state SET stopped_at=clock_timestamp(),stop_reason=$2 WHERE domain_name=$1", domain, "连续3天自动申请失败，已永久停止自动申请；手动申请成功后恢复"); err != nil {
				return false, err
			}
			if err = cancelPendingACMEOrders(ctx, tx, domain, 0, "auto renewal stopped after consecutive failures"); err != nil {
				return false, err
			}
			stopped = true
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return stopped, nil
}

// PruneACMEAttempts bounds the attempt history.
func (p *Postgres) PruneACMEAttempts(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, "DELETE FROM gateway_acme_attempt WHERE started_at < clock_timestamp() - make_interval(secs => $1)", acmeAttemptRetention.Seconds())
	return err
}

// ACMEAutoSummary is the per-host automatic-renewal view the Host catalog shows.
type ACMEAutoSummary struct {
	FailDays      int
	StoppedAt     *time.Time
	StopReason    string
	AttemptsToday int
	LastOrder     *ACMELatestOrder
}

type ACMELatestOrder struct {
	ID        int64
	Status    string
	UpdatedAt time.Time
}

// ACMEAutoSummaries returns the summary for every domain that has any state,
// attempt today, or order. Hosts absent from the map have none.
func (p *Postgres) ACMEAutoSummaries(ctx context.Context) (map[string]*ACMEAutoSummary, error) {
	out := map[string]*ACMEAutoSummary{}
	get := func(name string) *ACMEAutoSummary {
		if out[name] == nil {
			out[name] = &ACMEAutoSummary{}
		}
		return out[name]
	}
	rows, err := p.pool.Query(ctx, "SELECT domain_name,fail_days,stopped_at,stop_reason FROM gateway_acme_auto_state")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, reason string
		var days int
		var stoppedAt *time.Time
		if err := rows.Scan(&name, &days, &stoppedAt, &reason); err != nil {
			rows.Close()
			return nil, err
		}
		item := get(name)
		item.FailDays, item.StoppedAt, item.StopReason = days, stoppedAt, reason
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = p.pool.Query(ctx, "SELECT domain_name,count(*) FROM gateway_acme_attempt WHERE trigger='auto' AND outcome<>'skipped' AND started_at >= "+acmeUTCDayStart+" GROUP BY domain_name")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			rows.Close()
			return nil, err
		}
		get(name).AttemptsToday = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = p.pool.Query(ctx, "SELECT DISTINCT ON (domain_name) domain_name,id,status,updated_at FROM gateway_certificate_order ORDER BY domain_name,created_at DESC,id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var order ACMELatestOrder
		if err := rows.Scan(&name, &order.ID, &order.Status, &order.UpdatedAt); err != nil {
			return nil, err
		}
		get(name).LastOrder = &order
	}
	return out, rows.Err()
}
