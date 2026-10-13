package repository

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"yaginx-gateway/internal/management"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Outbound mail settings and the test-send path that proves them.

func (p *Postgres) GetSMTP(ctx context.Context) (*management.SMTPSettings, error) {
	var payload []byte
	var updatedAt int64
	err := p.pool.QueryRow(ctx, "SELECT value::text,extract(epoch FROM updated_at)::bigint FROM gateway_system_setting WHERE setting_key='smtp'").Scan(&payload, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out management.SMTPSettings
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	out.UpdateTS = management.ID(updatedAt)
	return &out, nil
}

func (p *Postgres) SetSMTP(ctx context.Context, value management.SMTPSettings) error {
	return p.saveSMTP(ctx, p.pool, smtpSettingsPayload(value))
}

func smtpSettingsPayload(value management.SMTPSettings) map[string]any {
	return map[string]any{
		"enabled":   value.Enabled,
		"host":      value.Host,
		"port":      value.Port,
		"useSsl":    value.UseSSL,
		"userName":  value.UserName,
		"password":  value.Password,
		"fromEmail": value.FromEmail,
		"fromName":  value.FromName,
	}
}

type smtpExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (p *Postgres) saveSMTP(ctx context.Context, executor smtpExecutor, v map[string]any) error {
	plain, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = executor.Exec(ctx, "INSERT INTO gateway_system_setting(setting_key,value,content_type,updated_at) VALUES('smtp',$1::jsonb,'application/json',clock_timestamp()) ON CONFLICT(setting_key) DO UPDATE SET value=excluded.value,updated_at=clock_timestamp()", string(plain))
	return err
}

// ImportSMTPIfAbsent preserves a setting already written through the Go
// management API. Legacy system.json is only a bootstrap source, never an
// authority after the PostgreSQL migration.
func (p *Postgres) ImportSMTPIfAbsent(ctx context.Context, value map[string]any) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var present bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway_system_setting WHERE setting_key='smtp' FOR UPDATE)").Scan(&present); err != nil {
		return false, err
	}
	if present {
		return false, tx.Commit(ctx)
	}
	if err := p.saveSMTP(ctx, tx, value); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (p *Postgres) TestSMTP(ctx context.Context, email string) error {
	return p.sendSMTP(ctx, strings.TrimSpace(email), "Yaginx SMTP test", "This is a Yaginx SMTP configuration test.\r\n")
}

func (p *Postgres) sendSMTP(ctx context.Context, recipient, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.ContainsAny(recipient+subject, "\r\n") || recipient == "" {
		return fmt.Errorf("invalid email message")
	}
	setting, err := p.GetSMTP(ctx)
	if err != nil {
		return err
	}
	if setting == nil || !setting.Enabled {
		return fmt.Errorf("SMTP is not enabled")
	}
	host := stringPointerValue(setting.Host)
	from := stringPointerValue(setting.FromEmail)
	if host == "" || from == "" || strings.ContainsAny(host+from, "\r\n") {
		return fmt.Errorf("SMTP host and sender are required")
	}
	port := setting.Port
	if port == 0 {
		port = 25
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid SMTP port")
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	var connection net.Conn
	if setting.UseSSL {
		connection, err = tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", address, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		connection, err = net.DialTimeout("tcp", address, 10*time.Second)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	defer connection.Close()
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return err
	}
	defer client.Quit()
	username, password := stringPointerValue(setting.UserName), stringPointerValue(setting.Password)
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(writer, "To: %s\r\nFrom: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", recipient, from, subject, body); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
