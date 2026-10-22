package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/management"
)

var acmeLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ACMECompletedOrderRetention is how long finished (valid) orders are kept.
const ACMECompletedOrderRetention = 7 * 24 * time.Hour

// PurgeCompletedACMEOrders deletes valid orders finished before the retention
// window; their logs go with them via ON DELETE CASCADE.
func (p *Postgres) PurgeCompletedACMEOrders(ctx context.Context, retention time.Duration) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM gateway_certificate_order WHERE status='valid' AND COALESCE(finished_at,updated_at) < clock_timestamp() - make_interval(secs => $1)`, retention.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (p *Postgres) SearchACMEOrderLogs(ctx context.Context, orderID management.ID, request management.PageRequest) (management.ACMEOrderLogPage, error) {
	page, err := management.NormalizePageRequest(request)
	if err != nil {
		return management.ACMEOrderLogPage{}, err
	}
	var total int64
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM gateway_certificate_order_log WHERE order_id=$1", int64(orderID)).Scan(&total); err != nil {
		return management.ACMEOrderLogPage{}, err
	}
	paging, offset, err := management.PageWindow(page, total)
	if err != nil {
		return management.ACMEOrderLogPage{}, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id,order_id,occurred_at,operation,status,result,error_code,message,COALESCE(node_id::text,''),attempt FROM gateway_certificate_order_log WHERE order_id=$1 ORDER BY occurred_at DESC,id DESC LIMIT $2 OFFSET $3`, int64(orderID), paging.PageSize, offset)
	if err != nil {
		return management.ACMEOrderLogPage{}, err
	}
	defer rows.Close()
	out := management.ACMEOrderLogPage{Paging: paging, Records: make([]management.ACMEOrderLog, 0)}
	for rows.Next() {
		var item management.ACMEOrderLog
		if err := rows.Scan(&item.ID, &item.OrderID, &item.OccurredAt, &item.Operation, &item.Status, &item.Result, &item.ErrorCode, &item.Message, &item.NodeID, &item.Attempt); err != nil {
			return management.ACMEOrderLogPage{}, err
		}
		out.Records = append(out.Records, item)
	}
	return out, rows.Err()
}

func (p *Postgres) RecordACMEOrderEvent(ctx context.Context, orderID int64, operation, status, result, errorCode, message string) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO gateway_certificate_order_log(order_id,operation,status,result,error_code,message,node_id,attempt) SELECT id,$2,$3,$4,$5,$6,node_id,attempt FROM gateway_certificate_order WHERE id=$1`, orderID, operation, status, result, errorCode, sanitizeACMEError(message))
	return err
}

func (p *Postgres) recordACMEOrderEvent(ctx context.Context, orderID int64, operation, status, result, errorCode, message string) {
	_ = p.RecordACMEOrderEvent(ctx, orderID, operation, status, result, errorCode, message)
}

func (p *Postgres) SearchACMEOrders(ctx context.Context, page, size int, domain, status string) (management.ACMEOrderPage, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	offset := (page - 1) * size
	var out management.ACMEOrderPage
	out.PageIndex, out.PageSize = page, size
	// Domain filter is a case-insensitive substring match; LIKE wildcards in the
	// user input are escaped so they match literally.
	domain = strings.ToLower(strings.TrimSpace(domain))
	pattern := ""
	if domain != "" {
		pattern = "%" + acmeLikeEscaper.Replace(domain) + "%"
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM gateway_certificate_order WHERE ($1='' OR domain_name LIKE $1) AND ($2='' OR status=$2)`, pattern, status).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id,domain_name,status,attempt,error_class,COALESCE(error_message,''),created_at,updated_at,next_attempt_at,finished_at FROM gateway_certificate_order WHERE ($1='' OR domain_name LIKE $1) AND ($2='' OR status=$2) ORDER BY updated_at DESC,id DESC LIMIT $3 OFFSET $4`, pattern, status, size, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v management.ACMEOrder
		if err := rows.Scan(&v.ID, &v.Domain, &v.Status, &v.Attempt, &v.ErrorClass, &v.ErrorMessage, &v.CreatedAt, &v.UpdatedAt, &v.NextAttemptAt, &v.FinishedAt); err != nil {
			return out, err
		}
		out.Records = append(out.Records, v)
	}
	return out, rows.Err()
}

func (p *Postgres) CancelACMEOrder(ctx context.Context, id management.ID) error {
	tag, err := p.pool.Exec(ctx, `UPDATE gateway_certificate_order SET status='canceled',error_class='operator_canceled',error_message='canceled by operator',next_attempt_at=NULL,finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND status IN ('pending','locked','authorizing','finalizing','retry_wait')`, int64(id))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("ACME order not found or already terminal")
	}
	p.recordACMEOrderEvent(ctx, int64(id), "cancel", "canceled", "operator", "operator_canceled", "canceled by operator")
	return nil
}

func (p *Postgres) RetryACMEOrderForOperator(ctx context.Context, id management.ID) error {
	var domain string
	err := p.pool.QueryRow(ctx, `UPDATE gateway_certificate_order SET status='retry_wait',error_class='temporary',error_message='',next_attempt_at=clock_timestamp(),finished_at=NULL,updated_at=clock_timestamp() WHERE id=$1 AND status IN ('failed','canceled','retry_wait') RETURNING domain_name`, int64(id)).Scan(&domain)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("ACME order cannot be retried")
	}
	if err == nil {
		p.recordACMEOrderEvent(ctx, int64(id), "retry", "retry_wait", "operator", "", "retry scheduled by operator")
	}
	return err
}
