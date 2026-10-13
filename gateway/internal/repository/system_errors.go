package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"yaginx-gateway/internal/management"
)

// SystemError is deliberately independent from proxy request history. Callers
// must only use it for Yaginx-owned failures, never for upstream responses.
type SystemError struct {
	Component, Operation, Severity, ErrorCode string
	Message                                   string
	Details                                   map[string]any
	TraceID                                   string
}

func (p *Postgres) SearchSystemErrorsByTrace(ctx context.Context, traceID string, limit int) (management.SystemErrorPage, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return management.SystemErrorPage{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `SELECT id,system_event_id,extract(epoch from occurred_at)::bigint,COALESCE(node_id::text,''),version,build_id,config_revision,component,operation,severity,error_code,message,details,trace_id,occurrence_count,extract(epoch from first_seen_at)::bigint,extract(epoch from last_seen_at)::bigint FROM yaginx_system_error WHERE trace_id=$1 ORDER BY last_seen_at DESC,id DESC LIMIT $2`, traceID, limit)
	if err != nil {
		return management.SystemErrorPage{}, err
	}
	defer rows.Close()
	out := make([]management.SystemErrorListItem, 0)
	for rows.Next() {
		var v management.SystemErrorListItem
		var raw []byte
		if err := rows.Scan(&v.ID, &v.SystemEventID, &v.OccurredAt, &v.NodeID, &v.Version, &v.BuildID, &v.ConfigRevision, &v.Component, &v.Operation, &v.Severity, &v.ErrorCode, &v.Message, &raw, &v.TraceID, &v.OccurrenceCount, &v.FirstSeenAt, &v.LastSeenAt); err != nil {
			return management.SystemErrorPage{}, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &v.Details)
		}
		out = append(out, v)
	}
	return management.SystemErrorPage{Records: out, Paging: management.PageCursor{PageIndex: 1, PageSize: limit, Total: int64(len(out))}}, rows.Err()
}

// EnsureSystemErrorSchema verifies the feature is available without granting
// the runtime role DDL privileges. The tables are owned by migrations.
func (p *Postgres) EnsureSystemErrorSchema(ctx context.Context) error {
	var errors, metrics int
	err := p.pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='yaginx_system_error'),
  (SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='yaginx_system_error_metric')`).Scan(&errors, &metrics)
	if err != nil {
		return err
	}
	if errors != 1 || metrics != 1 {
		return fmt.Errorf("system error schema is not migrated")
	}
	return nil
}

const systemErrorLock = int64(874231905123)

// SetRuntimeIdentity supplies low-cardinality release metadata for system
// failures. It is never populated from request data.
func (p *Postgres) SetRuntimeIdentity(nodeID, version, buildID string) {
	p.systemNodeID, p.systemVersion, p.systemBuildID = strings.TrimSpace(nodeID), strings.TrimSpace(version), strings.TrimSpace(buildID)
}

// SetSystemConfigRevision supplies the last successfully applied authoritative
// configuration revision for subsequent system-failure records.
func (p *Postgres) SetSystemConfigRevision(revision int64) {
	if revision < 0 {
		revision = 0
	}
	p.systemConfigRevision.Store(revision)
	p.systemConfigRevisionSet.Store(true)
}

// RecordSystemError upserts by a stable fingerprint and keeps only the newest
// 1000 distinct Yaginx-owned failures across the shared database.
func (p *Postgres) RecordSystemError(ctx context.Context, value SystemError) (string, error) {
	value.Component = strings.TrimSpace(value.Component)
	value.Operation = truncateSystemText(strings.TrimSpace(value.Operation), 96)
	value.Severity = strings.TrimSpace(value.Severity)
	value.ErrorCode = strings.TrimSpace(value.ErrorCode)
	value.Message = sanitizeSystemMessage(value.Message)
	if value.Component == "" || value.Operation == "" || value.Severity == "" || value.ErrorCode == "" {
		return "", fmt.Errorf("system error component, operation, severity, and error code are required")
	}
	if !validSystemErrorComponent(value.Component) || !validSystemErrorSeverity(value.Severity) {
		return "", fmt.Errorf("system error component or severity is invalid")
	}
	if !validSystemErrorCode(value.ErrorCode) {
		return "", fmt.Errorf("system error code must be stable uppercase snake case")
	}
	fingerprint := normalizeSystemErrorFingerprint(value.Component, value.Operation, value.ErrorCode, value.Message)
	eventID := uuid.New()
	details := map[string]any{}
	for key, item := range value.Details {
		if len(details) >= 32 || strings.TrimSpace(key) == "" {
			continue
		}
		details[truncateSystemText(key, 128)] = truncateSystemValue(item)
	}
	detailJSON, err := json.Marshal(details)
	if err != nil {
		return "", fmt.Errorf("marshal system error details: %w", err)
	}
	if len(detailJSON) > 8192 {
		detailJSON = []byte(`{"truncated":true}`)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", systemErrorLock); err != nil {
		return "", err
	}
	var stored string
	var configRevision any
	configRevisionValue := int64(0)
	if p.systemConfigRevisionSet.Load() {
		configRevisionValue = p.systemConfigRevision.Load()
		configRevision = configRevisionValue
	}
	err = tx.QueryRow(ctx, `
INSERT INTO yaginx_system_error (occurred_at,node_id,version,build_id,config_revision,component,operation,severity,error_code,fingerprint,system_event_id,message,details,trace_id,occurrence_count,first_seen_at,last_seen_at)
VALUES (clock_timestamp(),NULLIF($1,'')::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,COALESCE($13,''),1,clock_timestamp(),clock_timestamp())
ON CONFLICT (fingerprint) DO UPDATE SET
  occurred_at=EXCLUDED.occurred_at, node_id=EXCLUDED.node_id, version=EXCLUDED.version, build_id=EXCLUDED.build_id, config_revision=EXCLUDED.config_revision,
  severity=EXCLUDED.severity, system_event_id=EXCLUDED.system_event_id, message=EXCLUDED.message, details=EXCLUDED.details,
  trace_id=EXCLUDED.trace_id, occurrence_count=yaginx_system_error.occurrence_count+1, last_seen_at=clock_timestamp()
RETURNING system_event_id::text`, p.systemNodeID, p.systemVersion, p.systemBuildID, configRevision, value.Component, value.Operation, value.Severity, value.ErrorCode, fingerprint, eventID, value.Message, detailJSON, value.TraceID).Scan(&stored)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO yaginx_system_error_metric (period_day,component,error_code,severity,node_id,version,config_revision,occurrence_count,first_seen_at,last_seen_at)
VALUES (clock_timestamp()::date,$1,$2,$3,$4,$5,$6,1,clock_timestamp(),clock_timestamp())
ON CONFLICT (period_day,component,error_code,severity,node_id,version,config_revision) DO UPDATE SET occurrence_count=yaginx_system_error_metric.occurrence_count+1,last_seen_at=clock_timestamp()`, value.Component, value.ErrorCode, value.Severity, p.systemNodeID, p.systemVersion, configRevisionValue); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM yaginx_system_error WHERE id IN (SELECT id FROM yaginx_system_error ORDER BY last_seen_at DESC,id DESC OFFSET 1000)`); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return stored, nil
}

func (p *Postgres) SearchSystemErrors(ctx context.Context, input management.SystemErrorSearchQuery) (management.SystemErrorPage, error) {
	page, err := management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		return management.SystemErrorPage{}, err
	}
	input.Component = strings.TrimSpace(input.Component)
	input.Severity = strings.TrimSpace(input.Severity)
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	input.SystemEventID = strings.TrimSpace(input.SystemEventID)
	where, args := []string{"true"}, []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if input.Component != "" {
		add("component=$%d", input.Component)
	}
	if input.Severity != "" {
		add("severity=$%d", input.Severity)
	}
	if input.ErrorCode != "" {
		add("error_code=$%d", input.ErrorCode)
	}
	if input.ConfigRevision != nil {
		add("config_revision=$%d", *input.ConfigRevision)
	}
	if input.SystemEventID != "" {
		if _, err := uuid.Parse(input.SystemEventID); err != nil {
			return management.SystemErrorPage{}, fmt.Errorf("system event id is invalid")
		}
		add("system_event_id=$%d::uuid", input.SystemEventID)
	}
	filter := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM yaginx_system_error WHERE "+filter, args...).Scan(&total); err != nil {
		return management.SystemErrorPage{}, err
	}
	paging, offset, err := management.PageWindow(page, int64(total))
	if err != nil {
		return management.SystemErrorPage{}, err
	}
	args = append(args, paging.PageSize, offset)
	rows, err := p.pool.Query(ctx, "SELECT id,system_event_id,extract(epoch from occurred_at)::bigint,COALESCE(node_id::text,''),version,build_id,config_revision,component,operation,severity,error_code,message,details,trace_id,occurrence_count,extract(epoch from first_seen_at)::bigint,extract(epoch from last_seen_at)::bigint FROM yaginx_system_error WHERE "+filter+" ORDER BY last_seen_at DESC,id DESC LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	if err != nil {
		return management.SystemErrorPage{}, err
	}
	defer rows.Close()
	result := make([]management.SystemErrorListItem, 0)
	for rows.Next() {
		var v management.SystemErrorListItem
		var detailJSON []byte
		if err := rows.Scan(&v.ID, &v.SystemEventID, &v.OccurredAt, &v.NodeID, &v.Version, &v.BuildID, &v.ConfigRevision, &v.Component, &v.Operation, &v.Severity, &v.ErrorCode, &v.Message, &detailJSON, &v.TraceID, &v.OccurrenceCount, &v.FirstSeenAt, &v.LastSeenAt); err != nil {
			return management.SystemErrorPage{}, err
		}
		if len(detailJSON) != 0 {
			_ = json.Unmarshal(detailJSON, &v.Details)
		}
		result = append(result, v)
	}
	return management.SystemErrorPage{Records: result, Paging: paging}, rows.Err()
}

func normalizeSystemErrorFingerprint(component, operation, code, message string) string {
	messageKey := strings.ToLower(strings.Join(strings.Fields(message), " "))
	hash := sha256.Sum256([]byte(strings.Join([]string{component, operation, code, messageKey}, "\x00")))
	return hex.EncodeToString(hash[:])
}

func validSystemErrorCode(value string) bool {
	if len(value) < 3 || len(value) > 96 {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func validSystemErrorComponent(value string) bool {
	switch value {
	case "database", "listener", "configsync", "certificate", "acme", "upgrade", "telemetry", "management", "runtime":
		return true
	default:
		return false
	}
}

func validSystemErrorSeverity(value string) bool {
	switch value {
	case "info", "warning", "error", "critical":
		return true
	default:
		return false
	}
}

func sanitizeSystemMessage(value string) string {
	value = strings.TrimSpace(value)
	// Keep diagnostics useful while preventing URLs, addresses, UUIDs and
	// opaque identifiers from becoming durable high-cardinality data.
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, value)
	fields := strings.Fields(value)
	for i, field := range fields {
		lower := strings.ToLower(field)
		candidate := strings.Trim(field, "[](),;.")
		_, uuidErr := uuid.Parse(candidate)
		if ip := net.ParseIP(candidate); ip != nil || uuidErr == nil || strings.Contains(lower, "://") || strings.Contains(field, "@") || strings.Count(field, ".") >= 2 || (strings.Contains(field, ":") && strings.Count(field, ":") == 1) {
			fields[i] = "<redacted>"
		}
	}
	return truncateSystemText(strings.Join(fields, " "), 512)
}

func truncateSystemText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func truncateSystemValue(value any) any {
	switch item := value.(type) {
	case string:
		return sanitizeSystemMessage(item)
	case map[string]any:
		out := make(map[string]any, min(len(item), 16))
		for key, value := range item {
			if len(out) >= 16 || strings.TrimSpace(key) == "" {
				break
			}
			out[truncateSystemText(key, 64)] = truncateSystemValue(value)
		}
		return out
	case []any:
		out := make([]any, 0, min(len(item), 16))
		for _, value := range item {
			if len(out) >= 16 {
				break
			}
			out = append(out, truncateSystemValue(value))
		}
		return out
	default:
		return value
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
