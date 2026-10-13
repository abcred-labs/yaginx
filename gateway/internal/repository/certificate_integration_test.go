//go:build integration

package repository

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"software.sslmate.com/src/go-pkcs12"
	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/database"
)

func TestPostgresStoresAndLoadsACMEPEMCertificate(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_cert" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolated)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	chain, key := selfSignedPEM(t, "acme.example.invalid")
	store := New(pool)
	fingerprint, err := store.StoreACMECertificate(ctx, chain, key)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" {
		t.Fatal("missing fingerprint")
	}
	entries, err := store.LoadCertificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Names) != 1 || entries[0].Names[0] != "acme.example.invalid" {
		t.Fatalf("loaded entries=%#v", entries)
	}
	manager := certificates.NewManager()
	if err := manager.Replace(entries); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresImportsPFXIdempotentlyAfterContractAlignment(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_pfx" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolated)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pfx := selfSignedPFX(t, "legacy.example.invalid")
	store := New(pool)
	first, err := store.ImportPFX(ctx, pfx, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ImportPFX(ctx, pfx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("fingerprints = %q, %q", first, second)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gateway_certificate WHERE fingerprint=$1 AND fingerprint_sha256=$2", first, first).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("certificate rows = %d, want 1", count)
	}
	entries, err := store.LoadCertificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Names) != 1 || entries[0].Names[0] != "legacy.example.invalid" {
		t.Fatalf("loaded entries=%#v", entries)
	}
}

func TestPostgresImportsLegacyACMEAccountOnlyWhenAbsent(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_account" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, key := selfSignedPEM(t, "account.example.invalid")
	store := New(pool)
	directory := "https://acme-v02.api.letsencrypt.org/directory"
	created, err := store.ImportACMEAccountIfAbsent(ctx, directory, "operator@example.com", key)
	if err != nil || !created {
		t.Fatalf("first import created=%t err=%v", created, err)
	}
	account, exists, err := store.LoadACMEAccount(ctx, directory)
	if err != nil || !exists || account.Email != "operator@example.com" || string(account.PrivateKeyPEM) != string(key) {
		t.Fatalf("loaded=%#v exists=%t err=%v", account, exists, err)
	}
	if _, err := store.SaveACMEAccount(ctx, directory, account.Email, "", key, nil); err != nil {
		t.Fatalf("save unregistered account: %v", err)
	}
	account, exists, err = store.LoadACMEAccount(ctx, directory)
	if err != nil || !exists || account.AccountURL != "" || len(account.RegistrationJSON) != 0 {
		t.Fatalf("unregistered account=%#v exists=%t err=%v", account, exists, err)
	}
	if _, err := store.SaveACMEAccount(ctx, directory, account.Email, "https://acme.example.invalid/acct/7", key, []byte(`{"uri":"https://acme.example.invalid/acct/7"}`)); err != nil {
		t.Fatal(err)
	}
	created, err = store.ImportACMEAccountIfAbsent(ctx, directory, "other@example.invalid", []byte("not-used"))
	if err != nil || created {
		t.Fatalf("repeat import created=%t err=%v", created, err)
	}
	account, exists, err = store.LoadACMEAccount(ctx, directory)
	if err != nil || !exists || account.Email != "operator@example.com" || account.AccountURL != "https://acme.example.invalid/acct/7" || len(account.RegistrationJSON) == 0 {
		t.Fatalf("Go account was overwritten: %#v exists=%t err=%v", account, exists, err)
	}
}

func selfSignedPEM(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
}

func selfSignedPFX(t *testing.T, name string) []byte {
	t.Helper()
	chain, private := selfSignedPEM(t, name)
	certBlock, _ := pem.Decode(chain)
	keyBlock, _ := pem.Decode(private)
	if certBlock == nil || keyBlock == nil {
		t.Fatal("invalid self-signed PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pfx, err := pkcs12.Encode(rand.Reader, key, cert, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return pfx
}
