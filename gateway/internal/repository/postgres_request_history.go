package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/telemetry"

	"github.com/jackc/pgx/v5"
)

// Per-request telemetry: the aggregate flush off the request path and the
// request-history reads the console and the MCP diagnose tool share.

func mustParseInt64(raw string) int64 { value, _ := strconv.ParseInt(raw, 10, 64); return value }

// Flush persists aggregate traffic off the request path. The per-host/period
// upsert is safe across nodes and deliberately ignores malformed host events.
func (p *Postgres) Flush(ctx context.Context, events []telemetry.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, event := range events {
		host := strings.TrimSpace(strings.Split(event.Host, ":")[0])
		if host == "" {
			continue
		}
		period := event.At.UTC().Truncate(time.Hour).Unix()
		if _, err := tx.Exec(ctx, "INSERT INTO host_traffic(host_name,period,request_counts,inbound_bytes,outbound_bytes) VALUES($1,$2,1,$3,$4) ON CONFLICT(host_name,period) DO UPDATE SET request_counts=host_traffic.request_counts+1,inbound_bytes=host_traffic.inbound_bytes+excluded.inbound_bytes,outbound_bytes=host_traffic.outbound_bytes+excluded.outbound_bytes", host, period, event.Inbound, event.Outbound); err != nil {
			return err
		}
		var requestID int64
		if p.requestID != nil {
			requestID, err = p.requestID()
			if err != nil {
				return err
			}
		} else if err := tx.QueryRow(ctx, "SELECT nextval('gateway_request_history_id_seq')").Scan(&requestID); err != nil {
			return err
		}
		if err := insertRequestHistory(ctx, tx, requestID, event, host); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func requestURL(event telemetry.Event) string {
	if event.Host == "" {
		return event.Path
	}
	path := event.Path
	if path == "" {
		path = "/"
	}
	if event.Query != "" {
		path += "?" + event.Query
	}
	scheme := event.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + event.Host + path
}

// ListRequestHistoryPage treats the timestamp inputs as Unix seconds but uses
// request_time for filtering, providing one coherent timeline for stored
// request events.
func (p *Postgres) SearchRequestHistory(ctx context.Context, input management.RequestHistorySearchQuery) (management.RequestHistoryPage, error) {
	page, err := management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		return management.RequestHistoryPage{}, err
	}
	input.StatusCodePattern, err = management.NormalizeStatusCodePattern(input.StatusCodePattern)
	if err != nil {
		return management.RequestHistoryPage{}, err
	}
	input.Host = strings.TrimSpace(input.Host)
	input.Path = strings.TrimSpace(input.Path)
	input.StatusSource = strings.TrimSpace(input.StatusSource)
	input.StatusFamily = strings.TrimSpace(input.StatusFamily)
	input.ErrorCategory = strings.TrimSpace(input.ErrorCategory)
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	input.ClientIP = strings.TrimSpace(input.ClientIP)
	input.UserAgent = strings.TrimSpace(input.UserAgent)
	where := []string{"true"}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if input.From != nil && *input.From > 0 {
		add("request_time >= to_timestamp($%d)", int64(*input.From))
	}
	if input.To != nil && *input.To > 0 {
		add("request_time <= to_timestamp($%d)", int64(*input.To))
	}
	if input.WebsiteID != nil && *input.WebsiteID > 0 {
		add("EXISTS (SELECT 1 FROM website w WHERE w.id = $%d AND (COALESCE(w.hosts, '[]'::jsonb) ? request_history.host OR EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(w.specifications->'hostBindings', '[]'::jsonb)) binding WHERE lower(COALESCE(binding->>'hostname', '')) = lower(request_history.host))) )", int64(*input.WebsiteID))
	}
	if bounds, ok := map[string][2]int{"1xx": {100, 199}, "2xx": {200, 299}, "3xx": {300, 399}, "4xx": {400, 499}, "5xx": {500, 599}, "6xx": {600, 699}}[input.StatusFamily]; ok {
		args = append(args, bounds[0], bounds[1])
		where = append(where, fmt.Sprintf("status_code BETWEEN $%d AND $%d", len(args)-1, len(args)))
	}
	if input.StatusCodePattern != "" {
		likePattern := strings.ReplaceAll(input.StatusCodePattern, "x", "_")
		add("(status_code BETWEEN 100 AND 999 AND status_code::text LIKE $%d)", likePattern)
	}
	for _, filter := range []struct{ column, value string }{{"host", input.Host}, {"path", input.Path}, {"status_source", input.StatusSource}, {"error_category", input.ErrorCategory}, {"client_ip", input.ClientIP}} {
		if filter.value != "" {
			add(filter.column+" = $%d", filter.value)
		}
	}
	if input.StatusCode != nil {
		add("status_code = $%d", *input.StatusCode)
	}
	if input.UpstreamStatusCode != nil {
		add("upstream_status_code = $%d", *input.UpstreamStatusCode)
	}
	if input.ErrorCode != "" {
		add("error_code ILIKE '%%' || $%d || '%%'", input.ErrorCode)
	}
	if input.UserAgent != "" {
		add("user_agent ILIKE '%%' || $%d || '%%'", input.UserAgent)
	}
	filter := strings.Join(where, " AND ")
	var total int64
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM request_history WHERE "+filter, args...).Scan(&total); err != nil {
		return management.RequestHistoryPage{}, err
	}
	paging, offset, err := management.PageWindow(page, total)
	if err != nil {
		return management.RequestHistoryPage{}, err
	}
	args = append(args, paging.PageSize, offset)
	rows, err := p.pool.Query(ctx, "SELECT id,request_time,duration_ms,COALESCE(client_ip,''),COALESCE(method,''),COALESCE(host,''),COALESCE(path,''),status_code,upstream_status_code,COALESCE(status_source,''),COALESCE(error_category,''),COALESCE(error_code,''),COALESCE(user_agent,''),COALESCE(proxy_stage,''),COALESCE(exception_stage,''),COALESCE(forwarder_error,''),COALESCE(exception_type,''),COALESCE(trace_id,'') FROM request_history WHERE "+filter+" ORDER BY request_time DESC,id DESC LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	if err != nil {
		return management.RequestHistoryPage{}, err
	}
	defer rows.Close()
	out := make([]management.RequestHistoryListItem, 0)
	for rows.Next() {
		var id, duration int64
		var at time.Time
		var clientIP, method, host, path, source, category, code, userAgent, stage, exceptionStage, forwarder, exceptionType string
		var status int
		var upstream *int
		var traceID string
		if err := rows.Scan(&id, &at, &duration, &clientIP, &method, &host, &path, &status, &upstream, &source, &category, &code, &userAgent, &stage, &exceptionStage, &forwarder, &exceptionType, &traceID); err != nil {
			return management.RequestHistoryPage{}, err
		}
		out = append(out, management.RequestHistoryListItem{ID: strconv.FormatInt(id, 10), RequestAt: at.UTC().Unix(), DurationMs: strconv.FormatInt(duration, 10), ClientIP: clientIP, Method: method, Host: host, Path: path, StatusCode: status, UpstreamStatusCode: upstream, StatusSource: source, ErrorCategory: category, ErrorCode: code, UserAgent: userAgent, ProxyStage: stage, ExceptionStage: exceptionStage, ForwarderError: forwarder, HasException: hasRequestException(exceptionType, exceptionStage, forwarder), ExceptionType: exceptionType, TraceID: traceID})
	}
	if err := rows.Err(); err != nil {
		return management.RequestHistoryPage{}, err
	}
	return management.RequestHistoryPage{Records: out, Paging: paging}, nil
}

func (p *Postgres) GetRequestHistory(ctx context.Context, key string) (management.RequestHistoryDetail, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return management.RequestHistoryDetail{}, management.ErrNotFound
	}
	var at time.Time
	var duration int64
	var clientIP, method, scheme, host, path, query, requestURLValue, protocol, userAgent, stage, exceptionStage, forwarder, requestBody, responseBody, requestContentType, responseContentType, exceptionType, exceptionMessage, exceptionStack, source, category, code, traceID string
	var status int
	var upstream *int
	var requestHeaders, responseHeaders []byte
	var requestTruncated, responseTruncated bool
	err = p.pool.QueryRow(ctx, "SELECT request_time,duration_ms,COALESCE(client_ip,''),COALESCE(method,''),COALESCE(scheme,''),COALESCE(host,''),COALESCE(path,''),COALESCE(query_string,''),COALESCE(url,''),COALESCE(protocol,''),status_code,COALESCE(user_agent,''),COALESCE(proxy_stage,''),COALESCE(exception_stage,''),COALESCE(forwarder_error,''),COALESCE(request_headers,'{}'::jsonb),COALESCE(response_headers,'{}'::jsonb),COALESCE(request_body_base64,''),COALESCE(response_body_base64,''),COALESCE(request_content_type,''),COALESCE(response_content_type,''),request_body_truncated,response_body_truncated,COALESCE(exception_type,''),COALESCE(exception_message,''),COALESCE(exception_stack_trace,''),upstream_status_code,COALESCE(status_source,''),COALESCE(error_category,''),COALESCE(error_code,''),COALESCE(trace_id,'') FROM request_history WHERE id=$1", id).Scan(&at, &duration, &clientIP, &method, &scheme, &host, &path, &query, &requestURLValue, &protocol, &status, &userAgent, &stage, &exceptionStage, &forwarder, &requestHeaders, &responseHeaders, &requestBody, &responseBody, &requestContentType, &responseContentType, &requestTruncated, &responseTruncated, &exceptionType, &exceptionMessage, &exceptionStack, &upstream, &source, &category, &code, &traceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.RequestHistoryDetail{}, management.ErrNotFound
	}
	if err != nil {
		return management.RequestHistoryDetail{}, err
	}
	var parsedRequestHeaders, parsedResponseHeaders map[string][]string
	if err := json.Unmarshal(requestHeaders, &parsedRequestHeaders); err != nil {
		return management.RequestHistoryDetail{}, err
	}
	if err := json.Unmarshal(responseHeaders, &parsedResponseHeaders); err != nil {
		return management.RequestHistoryDetail{}, err
	}
	return management.RequestHistoryDetail{RequestHistoryListItem: management.RequestHistoryListItem{ID: strconv.FormatInt(id, 10), RequestAt: at.UTC().Unix(), DurationMs: strconv.FormatInt(duration, 10), ClientIP: clientIP, Method: method, Host: host, Path: path, StatusCode: status, UpstreamStatusCode: upstream, StatusSource: source, ErrorCategory: category, ErrorCode: code, UserAgent: userAgent, ProxyStage: stage, ExceptionStage: exceptionStage, ForwarderError: forwarder, HasException: hasRequestException(exceptionType, exceptionStage, forwarder), ExceptionType: exceptionType, TraceID: traceID}, Scheme: scheme, QueryString: query, URL: requestURLValue, Protocol: protocol, RequestHeaders: parsedRequestHeaders, ResponseHeaders: parsedResponseHeaders, RequestBodyBase64: requestBody, ResponseBodyBase64: responseBody, RequestContentType: requestContentType, ResponseContentType: responseContentType, RequestBodyTruncated: requestTruncated, ResponseBodyTruncated: responseTruncated, ExceptionMessage: exceptionMessage, ExceptionStackTrace: exceptionStack}, nil
}

func hasRequestException(exceptionType, exceptionStage, forwarderError string) bool {
	return exceptionType != "" || exceptionStage != "" || forwarderError != ""
}
