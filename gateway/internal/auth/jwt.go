// Package auth implements the legacy-compatible HS256 bearer token boundary.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims keeps the wire-claim names separate from the handler-friendly
// identity fields. Subject is never serialized: legacy tokens use uid/uname,
// not the standard JWT sub claim.
type Claims struct {
	UID       string `json:"uid"`
	Uname     string `json:"uname"`
	Email     string `json:"email"`
	SSID      string `json:"ssid,omitempty"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
	Subject   string `json:"-"`
}

type Keys struct {
	Current  []byte
	Previous []byte
	Issuer   string
	Audience string
	Session  time.Duration
}

// Issue retains the old small helper API for callers that only know an
// account name. Production login uses IssueIdentity so uid remains the stable
// account ID from PostgreSQL.
func (k Keys) Issue(subject string, now time.Time) (string, error) {
	return k.IssueIdentity(subject, subject, subject, "", now)
}

func (k Keys) IssueIdentity(uid, uname, email, ssid string, now time.Time) (string, error) {
	if len(k.Current) < 32 {
		return "", fmt.Errorf("JWT signing key must be at least 32 bytes")
	}
	if uid == "" || uname == "" || email == "" {
		return "", fmt.Errorf("JWT uid, uname, and email are required")
	}
	if k.Session <= 0 {
		return "", fmt.Errorf("JWT session must be positive")
	}
	issued := now.UTC()
	claims := Claims{
		UID: uid, Uname: uname, Email: email, SSID: ssid,
		Issuer: k.Issuer, Audience: k.Audience,
		IssuedAt: issued.Unix(), NotBefore: issued.Add(-30 * time.Second).Unix(),
		ExpiresAt: issued.Add(k.Session + 30*time.Second).Unix(), Subject: email,
	}
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := b64(header) + "." + b64(payload)
	return unsigned + "." + b64(sign(k.Current, []byte(unsigned))), nil
}

func (k Keys) Verify(token string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("malformed bearer token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("malformed bearer signature")
	}
	unsigned := parts[0] + "." + parts[1]
	valid := hmac.Equal(raw, sign(k.Current, []byte(unsigned))) || (len(k.Previous) > 0 && hmac.Equal(raw, sign(k.Previous, []byte(unsigned))))
	if !valid {
		return Claims{}, fmt.Errorf("invalid bearer signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("malformed bearer claims")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("malformed bearer claims")
	}
	if claims.UID == "" || claims.Uname == "" || claims.Email == "" || claims.Issuer != k.Issuer || claims.Audience != k.Audience || now.UTC().Unix() < claims.NotBefore || now.UTC().Unix() > claims.ExpiresAt {
		return Claims{}, fmt.Errorf("expired or invalid bearer claims")
	}
	claims.Subject = claims.Email
	return claims, nil
}

func sign(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }
