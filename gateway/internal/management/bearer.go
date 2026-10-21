package management

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"yaginx-gateway/internal/auth"
)

// RefreshCookieName is the HttpOnly cookie that carries the refresh session.
// It is scoped to /api/account so it is never sent to proxied traffic or any
// other API group.
const RefreshCookieName = "yaginx-refresh"

// RefreshCookiePath keeps the refresh secret out of every request that does
// not need it.
const RefreshCookiePath = "/api/account"

type subjectKey struct{}

// RequireBearerSource guards the authenticated operation group. The key source
// is a function because the signing keys are rotated while the server runs.
func RequireBearerSource(keys func() auth.Keys) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			if len(rawHeader) < len("Bearer ") || !strings.EqualFold(rawHeader[:len("Bearer ")], "Bearer ") {
				Fail(w, http.StatusUnauthorized, LegacyAuthorizationError(r.URL.RequestURI()))
				return
			}
			raw := strings.TrimSpace(rawHeader[len("Bearer "):])
			claims, err := keys().Verify(raw, time.Now())
			if err != nil {
				Fail(w, http.StatusUnauthorized, LegacyAuthorizationError(r.URL.RequestURI()))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), subjectKey{}, claims)))
		})
	}
}

// Identity returns the claims RequireBearerSource attached to the request.
func Identity(ctx context.Context) auth.Claims {
	claims, _ := ctx.Value(subjectKey{}).(auth.Claims)
	return claims
}

// LegacyAuthorizationError reproduces the legacy console's authorization message
// verbatim, including its loopback address, because the console matches on it.
func LegacyAuthorizationError(path string) string {
	return "地址http://127.0.0.1:18080/yaginx_console" + path + "授权认证失败, 请检查Authorization并传入有效的认证信息"
}

// SetTokenCookie mirrors the issued bearer token into the session cookie the
// console reads on a full page load.
func SetTokenCookie(w http.ResponseWriter, token string, issued time.Time, session time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     "yaginx-token",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  issued.Add(session),
		MaxAge:   int(session.Seconds()),
	})
}

// SetRefreshCookie stores "<sessionID>.<secret>" so RefreshToken/Logout can
// look the session up without a bearer token. Only the SHA-256 of secret is
// ever persisted server-side; the cookie value itself is the only copy of the
// plaintext secret.
func SetRefreshCookie(w http.ResponseWriter, sessionID int64, secret string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    fmt.Sprintf("%d.%s", sessionID, secret),
		Path:     RefreshCookiePath,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

// ParseRefreshCookie extracts the session id and secret from the refresh
// cookie. ok is false when the cookie is absent or malformed.
func ParseRefreshCookie(r *http.Request) (sessionID int64, secret string, ok bool) {
	cookie, err := r.Cookie(RefreshCookieName)
	if err != nil || cookie.Value == "" {
		return 0, "", false
	}
	idPart, secretPart, found := strings.Cut(cookie.Value, ".")
	if !found || secretPart == "" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	return id, secretPart, true
}

// ClearAuthCookies expires both the JWT mirror cookie and the refresh cookie.
// Used on logout and whenever a refresh attempt fails, so a stale cookie does
// not keep re-triggering failed refreshes.
func ClearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: "yaginx-token", Value: "", Path: "/", Secure: true, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: time.Unix(0, 0), MaxAge: -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name: RefreshCookieName, Value: "", Path: RefreshCookiePath, Secure: true, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: time.Unix(0, 0), MaxAge: -1,
	})
}
