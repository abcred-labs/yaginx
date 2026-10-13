// Package account serves the management console's authentication, password
// recovery and profile operations.
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/auth"
	"yaginx-gateway/internal/management"
)

// Default refresh-session durations, used only when the server did not supply
// its own (e.g. a Handler built directly in a test). Production wiring
// supplies these from the security.* runtime config instead.
const (
	defaultIdleTimeout      = 2 * time.Hour
	defaultAbsoluteTimeout  = 12 * time.Hour
	defaultRememberIdle     = 14 * 24 * time.Hour
	defaultRememberAbsolute = 30 * 24 * time.Hour
)

// Store is the account slice of the management repository.
type Store interface {
	Login(context.Context, string, string) (management.Account, error)
	RegisterAccount(context.Context, string, string) error
	GetProfile(context.Context, string) (management.Profile, error)
	UpdateProfile(context.Context, string, management.ProfileUpdateRequest) error
	StartPasswordRecovery(context.Context, string) (management.PasswordRecoveryStartResponse, error)
	VerifyPasswordRecovery(context.Context, string, string) error
	ResetPasswordRecovery(context.Context, string, string, string, string) error

	// CreateAccountSession records the refresh session issued at login.
	CreateAccountSession(ctx context.Context, accountID int64, tokenHash []byte, remembered bool, userAgent, clientIP string, idleTTL, absoluteTTL time.Duration) (sessionID int64, err error)
	// RotateAccountSession validates the presented secret's hash and, if the
	// session is live, rotates in a new hash and pushes the idle deadline
	// forward by whichever of idleTTL/rememberedIdleTTL matches the session's
	// own stored "remembered" flag (also returned, so the caller can set a
	// matching cookie lifetime). It returns management.ErrNotFound when the
	// session is missing, revoked, or past either deadline.
	RotateAccountSession(ctx context.Context, sessionID int64, oldHash, newHash []byte, idleTTL, rememberedIdleTTL time.Duration) (accountID int64, email string, remembered bool, err error)
	// RevokeAccountSessionByToken is the best-effort revoke Logout uses: it
	// verifies the secret's hash itself since Logout has no bearer identity.
	RevokeAccountSessionByToken(ctx context.Context, sessionID int64, tokenHash []byte) error
	// RevokeAccountSession revokes one of accountID's own sessions.
	RevokeAccountSession(ctx context.Context, accountID, sessionID int64) error
	// RevokeOtherAccountSessions signs out every other session for accountID.
	RevokeOtherAccountSessions(ctx context.Context, accountID, keepSessionID int64) error
	// ListAccountSessions lists accountID's active sessions, most recent first.
	ListAccountSessions(ctx context.Context, accountID int64) ([]management.SessionSummary, error)
}

// Handler owns the account operations. Keys is a function because the signing
// keys are rotated while the server runs. The four durations configure the
// refresh session's idle/absolute deadlines; the "Remember" pair applies when
// LoginRequest.Remember is true, otherwise the plain pair applies. A zero
// duration falls back to the package defaults above, so a Handler built
// without explicit durations (as in tests) still behaves sensibly.
type Handler struct {
	Store            Store
	Keys             func() auth.Keys
	BodyMax          int64
	IdleTimeout      time.Duration
	AbsoluteTimeout  time.Duration
	RememberIdle     time.Duration
	RememberAbsolute time.Duration
}

func (h Handler) idleTTL(remember bool) time.Duration {
	if remember {
		if h.RememberIdle > 0 {
			return h.RememberIdle
		}
		return defaultRememberIdle
	}
	if h.IdleTimeout > 0 {
		return h.IdleTimeout
	}
	return defaultIdleTimeout
}

func (h Handler) absoluteTTL(remember bool) time.Duration {
	if remember {
		if h.RememberAbsolute > 0 {
			return h.RememberAbsolute
		}
		return defaultRememberAbsolute
	}
	if h.AbsoluteTimeout > 0 {
		return h.AbsoluteTimeout
	}
	return defaultAbsoluteTimeout
}

// Routes registers the unauthenticated login, recovery, refresh, and logout
// operations. RefreshToken and Logout are unauthenticated at the bearer layer
// by design (see approved-deviations.json v2-api-account-refresh-token-cookie-*):
// they authenticate against the yaginx-refresh cookie instead, which is what
// lets a session survive the access token's own short expiry.
func (h Handler) Routes(r chi.Router) {
	r.Post("/account/login", h.Login)
	r.Post("/account/password_recovery/start", h.PasswordRecoveryStart)
	r.Post("/account/password_recovery/verify_email", h.PasswordRecoveryVerifyEmail)
	r.Post("/account/password_recovery/reset", h.PasswordRecoveryReset)
	r.Get("/account/refresh_token", h.RefreshToken)
	r.Post("/account/logout", h.Logout)
}

// AuthenticatedRoutes registers the operations that require a bearer token.
func (h Handler) AuthenticatedRoutes(r chi.Router) {
	r.Get("/account/profile", h.GetProfile)
	r.Post("/account/update_profile", h.UpdateProfile)
	r.Post("/account/register", h.Register)
	r.Get("/account/sessions", h.ListSessions)
	r.Post("/account/sessions/{id}/revoke", h.RevokeSession)
	r.Post("/account/sessions/revoke_others", h.RevokeOtherSessions)
}

func (h Handler) Login(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.LoginRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	acct, err := h.Store.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		management.FailStoreError(w, err)
		return
	}

	secret, hash, err := newRefreshSecret()
	if err != nil {
		management.Fail(w, http.StatusServiceUnavailable, "authentication unavailable")
		return
	}
	idleTTL, absoluteTTL := h.idleTTL(input.Remember), h.absoluteTTL(input.Remember)
	sessionID, err := h.Store.CreateAccountSession(r.Context(), acct.ID, hash, input.Remember, r.UserAgent(), clientIP(r), idleTTL, absoluteTTL)
	if err != nil {
		management.Fail(w, http.StatusServiceUnavailable, "authentication unavailable")
		return
	}

	issued := time.Now()
	keys := h.Keys()
	token, err := keys.IssueIdentity(strconv.FormatInt(acct.ID, 10), acct.Email, acct.Email, strconv.FormatInt(sessionID, 10), issued)
	if err != nil {
		management.Fail(w, http.StatusServiceUnavailable, "authentication unavailable")
		return
	}
	management.SetTokenCookie(w, token, issued, keys.Session)
	management.SetRefreshCookie(w, sessionID, secret, issued.Add(idleTTL))
	management.OK(w, management.LoginResult{Token: token, ExpiresIn: int(keys.Session.Seconds()), Name: acct.Email, UserID: acct.Email})
}

// RefreshToken authenticates the yaginx-refresh cookie (not a bearer token —
// see the Routes doc comment), rotates the refresh secret, and issues a fresh
// access token. A missing, malformed, invalid, revoked, or expired cookie
// clears both cookies and returns 401, so the browser stops retrying a dead
// session instead of looping on it.
func (h Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	sessionID, oldSecret, ok := management.ParseRefreshCookie(r)
	if !ok {
		management.ClearAuthCookies(w)
		management.Fail(w, http.StatusUnauthorized, "missing or invalid refresh session")
		return
	}
	oldHash := hashSecret(oldSecret)
	newSecret, newHash, err := newRefreshSecret()
	if err != nil {
		management.Fail(w, http.StatusServiceUnavailable, "authentication unavailable")
		return
	}
	// The rotated session keeps whatever "remembered" idle window it was
	// created with (RotateAccountSession looks that up itself and tells us,
	// so the cookie's own Max-Age can match); it never touches
	// absolute_expires_at, so the absolute cap from login is never extended by
	// refreshing.
	accountID, email, remembered, err := h.Store.RotateAccountSession(r.Context(), sessionID, oldHash, newHash, h.idleTTL(false), h.idleTTL(true))
	if err != nil {
		management.ClearAuthCookies(w)
		management.Fail(w, http.StatusUnauthorized, "missing or invalid refresh session")
		return
	}

	issued := time.Now()
	keys := h.Keys()
	token, err := keys.IssueIdentity(strconv.FormatInt(accountID, 10), email, email, strconv.FormatInt(sessionID, 10), issued)
	if err != nil {
		management.Fail(w, http.StatusServiceUnavailable, "authentication unavailable")
		return
	}
	management.SetTokenCookie(w, token, issued, keys.Session)
	management.SetRefreshCookie(w, sessionID, newSecret, issued.Add(h.idleTTL(remembered)))
	management.OK(w, management.LoginResult{Token: token, ExpiresIn: int(keys.Session.Seconds()), Name: email, UserID: email})
}

// Logout is best-effort and always succeeds: it revokes the session the
// refresh cookie names if the cookie is present and valid, then always clears
// both cookies regardless. There is nothing a caller can do wrong here, so
// there is no failure response to distinguish.
func (h Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if sessionID, secret, ok := management.ParseRefreshCookie(r); ok {
		_ = h.Store.RevokeAccountSessionByToken(r.Context(), sessionID, hashSecret(secret))
	}
	management.ClearAuthCookies(w)
	management.OK(w, management.EmptyResponse{})
}

func (h Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	identity := management.Identity(r.Context())
	accountID, err := strconv.ParseInt(identity.UID, 10, 64)
	if err != nil {
		management.Fail(w, http.StatusUnauthorized, "invalid session identity")
		return
	}
	sessions, err := h.Store.ListAccountSessions(r.Context(), accountID)
	if err != nil {
		management.FailStoreError(w, err)
		return
	}
	out := make([]management.SessionSummary, len(sessions))
	for i, s := range sessions {
		s.Current = strconv.FormatInt(int64(s.ID), 10) == identity.SSID
		out[i] = s
	}
	management.OK(w, out)
}

func (h Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	identity := management.Identity(r.Context())
	accountID, err := strconv.ParseInt(identity.UID, 10, 64)
	if err != nil {
		management.Fail(w, http.StatusUnauthorized, "invalid session identity")
		return
	}
	sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid session id")
		return
	}
	err = h.Store.RevokeAccountSession(r.Context(), accountID, sessionID)
	management.RespondStore(w, management.EmptyResponse{}, err)
}

func (h Handler) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	identity := management.Identity(r.Context())
	accountID, err := strconv.ParseInt(identity.UID, 10, 64)
	if err != nil {
		management.Fail(w, http.StatusUnauthorized, "invalid session identity")
		return
	}
	currentSessionID, err := strconv.ParseInt(identity.SSID, 10, 64)
	if err != nil {
		management.Fail(w, http.StatusUnauthorized, "invalid session identity")
		return
	}
	err = h.Store.RevokeOtherAccountSessions(r.Context(), accountID, currentSessionID)
	management.RespondStore(w, management.EmptyResponse{}, err)
}

func (h Handler) PasswordRecoveryStart(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.PasswordRecoveryStartRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.Store.StartPasswordRecovery(r.Context(), input.AccountEmail)
	management.RespondStore(w, out, err)
}

func (h Handler) PasswordRecoveryVerifyEmail(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.PasswordRecoveryVerifyEmailRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	management.RespondStore(w, true, h.Store.VerifyPasswordRecovery(r.Context(), input.RecoveryID, input.Email))
}

func (h Handler) PasswordRecoveryReset(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.PasswordRecoveryResetRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	err = h.Store.ResetPasswordRecovery(r.Context(), input.RecoveryID, input.Code, input.NewPassword, input.ConfirmPassword)
	management.RespondStore(w, true, err)
}

func (h Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.Store.GetProfile(r.Context(), management.Identity(r.Context()).Email)
	management.RespondStore(w, profile, err)
}

func (h Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.ProfileUpdateRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	err = h.Store.UpdateProfile(r.Context(), management.Identity(r.Context()).Email, input)
	management.RespondStore(w, true, err)
}

func (h Handler) Register(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.LoginRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	err = h.Store.RegisterAccount(r.Context(), input.Email, input.Password)
	management.RespondStore(w, management.EmptyResponse{}, err)
}

// newRefreshSecret returns a URL-safe opaque secret and the SHA-256 hash that
// is the only copy ever persisted server-side.
func newRefreshSecret() (secret string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	return secret, hashSecret(secret), nil
}

func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// clientIP strips the port from RemoteAddr for display in the active-sessions
// list. The management listener is CIDR-restricted and reached directly, not
// through a reverse proxy, so there is no X-Forwarded-For to trust here.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
