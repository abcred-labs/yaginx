//go:build integration

package acme

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/repository"
)

func TestWorkerPersistsSingleCrossNodeACMEOrder(t *testing.T) {
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
	schema := "yaginx_goal_acme" + uuid.NewString()[:10]
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
	store := repository.New(pool)
	nodes := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, node := range nodes {
		if _, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{NodeID: node, MachineFingerprint: strings.Repeat("a", 64), DisplayName: node}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ApproveNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	var domainID int64
	if err := pool.QueryRow(ctx, "INSERT INTO web_domain(name,is_use_free_cert,is_verified) VALUES('worker.example.invalid',true,false) RETURNING id").Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	orderID, orderStarted, err := store.StartACMEOrder(ctx, "worker.example.invalid", nodes[0])
	if err != nil || !orderStarted || orderID == 0 {
		t.Fatalf("start initial order id=%d started=%t err=%v", orderID, orderStarted, err)
	}
	if err := store.RetryACMEOrder(ctx, orderID, "temporary", "fixture retry", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if duplicateOrderID, started, err := store.StartACMEOrder(ctx, "worker.example.invalid", nodes[0]); err != nil || started || duplicateOrderID != 0 {
		t.Fatalf("future retry created duplicate id=%d started=%t err=%v", duplicateOrderID, started, err)
	}
	var pendingOrders int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gateway_certificate_order WHERE domain_name='worker.example.invalid' AND status IN ('pending','retry_wait')").Scan(&pendingOrders); err != nil {
		t.Fatal(err)
	}
	if pendingOrders != 1 {
		t.Fatalf("pending orders=%d, want 1", pendingOrders)
	}
	if err := store.RetryACMEOrder(ctx, orderID, "temporary", "fixture retry", time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	chain, key := workerSelfSignedPEM(t, "worker.example.invalid")
	settings := Settings{
		DirectoryURL:      "https://acme.example.invalid/directory",
		Email:             "operator@example.com",
		ScanInterval:      time.Hour,
		RenewBefore:       35 * 24 * time.Hour,
		LockRetryBase:     time.Minute,
		TemporaryRetryMax: 6 * time.Hour,
		// This fixture tests cross-node order locking and deduplicated
		// issuance. ACME preflight is covered separately and would require a
		// real key/value provider and HTTP-01 endpoint for this synthetic
		// domain.
		Preflight: func(context.Context, string, KeyValueProvider) error {
			return nil
		},
	}
	workers := make([]*Worker, 0, len(nodes))
	for _, node := range nodes {
		worker, err := NewWorker(store, pool, node, settings, nil)
		if err != nil {
			t.Fatal(err)
		}
		workers = append(workers, worker)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	for _, worker := range workers {
		worker.issue = func(context.Context, string) (issuedCertificate, error) {
			calls.Add(1)
			once.Do(func() { close(started) })
			<-release
			return issuedCertificate{ChainPEM: chain, KeyPEM: key}, nil
		}
	}
	// This is a first-time issuance (no certificate exists yet for the domain),
	// so it is exercised the same way the console's "立即申请" action would
	// drive it: processDomain directly, not the renewal-only ScanOnce ticker.
	target := repository.ACMEDomain{ID: domainID, Name: "worker.example.invalid"}
	var wait sync.WaitGroup
	wait.Add(1)
	go func() { defer wait.Done(); workers[0].processDomain(ctx, target, repository.ACMETriggerManual) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("first ACME worker did not enter issuance")
	}
	for _, worker := range workers[1:] {
		wait.Add(1)
		go func(worker *Worker) { defer wait.Done(); worker.processDomain(ctx, target, repository.ACMETriggerManual) }(worker)
	}
	time.Sleep(200 * time.Millisecond)
	close(release)
	wait.Wait()
	if calls.Load() != 1 {
		t.Fatalf("external issuance calls = %d, want 1", calls.Load())
	}
	var status string
	var certificateID *int64
	if err := pool.QueryRow(ctx, "SELECT status,certificate_id FROM gateway_certificate_order WHERE domain_name='worker.example.invalid' AND status='valid' ORDER BY id DESC LIMIT 1").Scan(&status, &certificateID); err != nil {
		t.Fatal(err)
	}
	if status != "valid" || certificateID == nil {
		t.Fatalf("order status=%q certificate=%v", status, certificateID)
	}
	var retryRows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gateway_certificate_order WHERE domain_name='worker.example.invalid' AND status='retry_wait'").Scan(&retryRows); err != nil {
		t.Fatal(err)
	}
	if retryRows != 0 {
		t.Fatalf("retry orders remain after a successful issuance: %d", retryRows)
	}
	entries, err := store.LoadCertificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manager := certificates.NewManager()
	if err := manager.Replace(entries); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := pool.QueryRow(ctx, "SELECT is_use_free_cert FROM web_domain WHERE id=$1", domainID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("successful ACME order disabled free certificate")
	}
}

func workerSelfSignedPEM(t *testing.T, name string) ([]byte, []byte) {
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
