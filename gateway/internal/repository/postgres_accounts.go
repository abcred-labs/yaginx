package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"yaginx-gateway/internal/management"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Console accounts: sign-in, registration, password recovery and profiles.

func (p *Postgres) Login(ctx context.Context, email, password string) (management.Account, error) {
	var a management.Account
	var legacy, hash, salt *string
	err := p.pool.QueryRow(ctx, "SELECT id,email,password,password_hash,password_salt FROM account WHERE lower(email)=lower($1) LIMIT 1", email).Scan(&a.ID, &a.Email, &legacy, &hash, &salt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, fmt.Errorf("%w: account not found", management.ErrNotFound)
	}
	if err != nil {
		return a, err
	}
	if hash != nil && *hash != "" {
		if salt == nil || subtle.ConstantTimeCompare([]byte(*hash), []byte(passwordHash(password, *salt))) != 1 {
			return a, fmt.Errorf("%w: password verification failed", management.ErrNotFound)
		}
		return a, nil
	}
	if legacy == nil || *legacy != password {
		return a, fmt.Errorf("%w: password verification failed", management.ErrNotFound)
	}
	// Match the retained .NET first-login migration: verify the legacy value
	// once, then replace it with the documented salted SHA-256 representation.
	// Future requests never need to retain or read this account's plaintext.
	nextSalt, err := randomSalt()
	if err != nil {
		return a, err
	}
	if _, err := p.pool.Exec(ctx, "UPDATE account SET password=NULL,password_hash=$2,password_salt=$3 WHERE id=$1", a.ID, passwordHash(password, nextSalt), nextSalt); err != nil {
		return a, err
	}
	return a, nil
}

// RegisterAccount stores only the legacy-compatible salted digest. The
// retained plaintext column belongs to the .NET rollback schema and must not
// receive newly created passwords.
func (p *Postgres) RegisterAccount(ctx context.Context, email, password string) error {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return fmt.Errorf("账户邮箱和密码不能为空")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Legacy account has no safe unique-email constraint to retrofit before the
	// .NET observation period. Serialize new registrations by normalized email
	// so concurrent requests cannot create an ambiguous account pair.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('yaginx:account:' || lower($1),0))", email); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM account WHERE lower(email)=lower($1))", email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("账户已存在")
	}
	salt, err := randomSalt()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO account(email,password,password_hash,password_salt) VALUES($1,NULL,$2,$3)", email, passwordHash(password, salt), salt)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return fmt.Errorf("账户已存在")
		}
		return err
	}
	return tx.Commit(ctx)
}

// CountAccounts reports whether first-boot administrator setup is still needed.
func (p *Postgres) CountAccounts(ctx context.Context) (int, error) {
	var count int
	err := p.pool.QueryRow(ctx, "SELECT count(*) FROM account").Scan(&count)
	return count, err
}

// CreateInitialAccount creates the first console account and refuses to alter
// an existing installation. It is intentionally separate from the authenticated
// registration endpoint so setup cannot be used to add arbitrary accounts.
func (p *Postgres) CreateInitialAccount(ctx context.Context, email, password string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var count int
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('yaginx:initial-account',0))"); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM account").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("accounts already exist")
	}
	return insertAccountTx(ctx, tx, email, password)
}

func insertAccountTx(ctx context.Context, tx pgx.Tx, email, password string) error {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return fmt.Errorf("账户邮箱和密码不能为空")
	}
	salt, err := randomSalt()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO account(email,password,password_hash,password_salt) VALUES($1,NULL,$2,$3)", email, passwordHash(password, salt), salt); err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return fmt.Errorf("账户已存在")
		}
		return err
	}
	return tx.Commit(ctx)
}

// ListAccounts returns the non-sensitive account identities used by the local
// administrator CLI. Account IDs are the only safe reset selector because the
// legacy schema does not guarantee unique email addresses.
func (p *Postgres) ListAccounts(ctx context.Context) ([]management.Account, error) {
	rows, err := p.pool.Query(ctx, "SELECT id,COALESCE(email,'') FROM account ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []management.Account{}
	for rows.Next() {
		var account management.Account
		if err := rows.Scan(&account.ID, &account.Email); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

// GetAccountByID obtains the account identity before an administrator resets
// its password, so the CLI can display exactly which account will be changed.
func (p *Postgres) GetAccountByID(ctx context.Context, id int64) (management.Account, error) {
	var account management.Account
	err := p.pool.QueryRow(ctx, "SELECT id,COALESCE(email,'') FROM account WHERE id=$1", id).Scan(&account.ID, &account.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return account, management.ErrNotFound
	}
	return account, err
}

// ResetAccountPassword replaces both legacy and current credential material.
// Newly reset accounts never retain a plaintext password in the rollback-era
// column.
func (p *Postgres) ResetAccountPassword(ctx context.Context, id int64, password string) error {
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}
	salt, err := randomSalt()
	if err != nil {
		return err
	}
	command, err := p.pool.Exec(ctx, "UPDATE account SET password=NULL,password_hash=$2,password_salt=$3 WHERE id=$1", id, passwordHash(password, salt), salt)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return management.ErrNotFound
	}
	return nil
}

func (p *Postgres) StartPasswordRecovery(ctx context.Context, email string) (management.PasswordRecoveryStartResponse, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return management.PasswordRecoveryStartResponse{}, fmt.Errorf("account email is required")
	}
	// Do not disclose account existence. Unknown accounts receive an opaque
	// unusable recovery ID and subsequent operations fail generically.
	recoveryID := uuid.NewString()
	masked := maskEmail(email)
	var accountID int64
	var accountEmail string
	err := p.pool.QueryRow(ctx, "SELECT id,email FROM account WHERE lower(email)=lower($1)", email).Scan(&accountID, &accountEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.PasswordRecoveryStartResponse{RecoveryID: recoveryID, MaskedEmail: masked}, nil
	}
	if err != nil {
		return management.PasswordRecoveryStartResponse{}, err
	}
	now := time.Now().UTC().UnixMilli()
	_, err = p.pool.Exec(ctx, "INSERT INTO account_password_recovery(recovery_id,account_id,account_email,email_verified,code_hash,code_salt,code_expire_ts,failed_attempts,status,create_ts,update_ts) VALUES($1,$2,$3,false,NULL,NULL,NULL,0,0,$4,$4)", recoveryID, accountID, accountEmail, now)
	if err != nil {
		return management.PasswordRecoveryStartResponse{}, err
	}
	return management.PasswordRecoveryStartResponse{RecoveryID: recoveryID, MaskedEmail: maskEmail(accountEmail)}, nil
}

func (p *Postgres) VerifyPasswordRecovery(ctx context.Context, recoveryID, email string) error {
	if _, err := uuid.Parse(strings.TrimSpace(recoveryID)); err != nil {
		return management.ErrNotFound
	}
	var accountEmail string
	err := p.pool.QueryRow(ctx, "SELECT account_email FROM account_password_recovery WHERE recovery_id=$1 AND status=0", recoveryID).Scan(&accountEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(email), accountEmail) {
		return management.ErrNotFound
	}
	code, err := randomCode()
	if err != nil {
		return err
	}
	if err := p.sendSMTP(ctx, accountEmail, "Yaginx password recovery code", "Your Yaginx password recovery code is: "+code+"\r\nIt expires in 10 minutes.\r\n"); err != nil {
		return err
	}
	salt, err := randomSalt()
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	_, err = p.pool.Exec(ctx, "UPDATE account_password_recovery SET email_verified=true,code_hash=$2,code_salt=$3,code_expire_ts=$4,failed_attempts=0,status=1,update_ts=$5 WHERE recovery_id=$1 AND status=0", recoveryID, passwordHash(code, salt), salt, now+10*60*1000, now)
	return err
}

func (p *Postgres) ResetPasswordRecovery(ctx context.Context, recoveryID, code, nextPassword, confirm string) error {
	if _, err := uuid.Parse(strings.TrimSpace(recoveryID)); err != nil {
		return management.ErrNotFound
	}
	if nextPassword == "" || nextPassword != confirm {
		return fmt.Errorf("password confirmation does not match")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountID int64
	var hash, salt *string
	var expires *int64
	var failures int
	err = tx.QueryRow(ctx, "SELECT account_id,code_hash,code_salt,code_expire_ts,failed_attempts FROM account_password_recovery WHERE recovery_id=$1 AND status=1 FOR UPDATE", recoveryID).Scan(&accountID, &hash, &salt, &expires, &failures)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrNotFound
	}
	if err != nil {
		return err
	}
	if hash == nil || salt == nil || expires == nil || time.Now().UTC().UnixMilli() > *expires || failures >= 5 || subtle.ConstantTimeCompare([]byte(*hash), []byte(passwordHash(code, *salt))) != 1 {
		_, _ = tx.Exec(ctx, "UPDATE account_password_recovery SET failed_attempts=failed_attempts+1,update_ts=$2 WHERE recovery_id=$1", recoveryID, time.Now().UTC().UnixMilli())
		_ = tx.Commit(ctx)
		return management.ErrNotFound
	}
	nextSalt, err := randomSalt()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE account SET password=NULL,password_hash=$2,password_salt=$3 WHERE id=$1", accountID, passwordHash(nextPassword, nextSalt), nextSalt); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE account_password_recovery SET status=2,update_ts=$2 WHERE recovery_id=$1", recoveryID, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetProfile(ctx context.Context, email string) (management.Profile, error) {
	var value string
	err := p.pool.QueryRow(ctx, "SELECT email FROM account WHERE lower(email)=lower($1)", email).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.Profile{}, management.ErrNotFound
	}
	if err != nil {
		return management.Profile{}, err
	}
	return management.Profile{Email: value}, nil
}

func (p *Postgres) UpdateProfile(ctx context.Context, email string, input management.ProfileUpdateRequest) error {
	oldPassword := input.OldPassword
	if oldPassword == "" {
		return fmt.Errorf("旧密码不能为空")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	var currentEmail string
	var legacy, hash, salt *string
	err = tx.QueryRow(ctx, "SELECT id,email,password,password_hash,password_salt FROM account WHERE lower(email)=lower($1) FOR UPDATE", email).Scan(&id, &currentEmail, &legacy, &hash, &salt)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrNotFound
	}
	if err != nil {
		return err
	}
	if hash == nil || *hash == "" || salt == nil || *salt == "" {
		if legacy == nil || subtle.ConstantTimeCompare([]byte(*legacy), []byte(oldPassword)) != 1 {
			return fmt.Errorf("旧密码错误")
		}
		migratedSalt, err := randomSalt()
		if err != nil {
			return err
		}
		hash, salt = ptr(passwordHash(oldPassword, migratedSalt)), ptr(migratedSalt)
	} else if subtle.ConstantTimeCompare([]byte(*hash), []byte(passwordHash(oldPassword, *salt))) != 1 {
		return fmt.Errorf("旧密码错误")
	}
	nextEmail := strings.TrimSpace(input.Email)
	if nextEmail == "" {
		nextEmail = currentEmail
	}
	nextHash, nextSalt := *hash, *salt
	if nextPassword := input.NewPassword; nextPassword != "" {
		if confirmed := input.ConfirmPassword; confirmed != "" && confirmed != nextPassword {
			return fmt.Errorf("新密码与确认密码不一致")
		}
		generatedSalt, err := randomSalt()
		if err != nil {
			return err
		}
		nextSalt, nextHash = generatedSalt, passwordHash(nextPassword, generatedSalt)
	}
	_, err = tx.Exec(ctx, "UPDATE account SET email=$2,password=NULL,password_hash=$3,password_salt=$4 WHERE id=$1", id, nextEmail, nextHash, nextSalt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func randomSalt() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func randomCode() (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := (uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])) % 1000000
	return fmt.Sprintf("%06d", value), nil
}

func passwordHash(password, salt string) string {
	value := sha256.Sum256([]byte(password + salt))
	return strings.ToUpper(hex.EncodeToString(value[:]))
}

func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 1 {
		return "***"
	}
	return email[:1] + strings.Repeat("*", max(1, at-2)) + email[at-1:]
}
