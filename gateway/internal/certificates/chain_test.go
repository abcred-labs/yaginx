package certificates

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestCompleteChainAddsVerifiedAIAIssuer(t *testing.T) {
	rootDER, rootKey := testIssuer(t)
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	issuerDER, issuerKey := testIntermediate(t, root, rootKey, "http://issuer.example.test/root.der")
	issuer, err := x509.ParseCertificate(issuerDER)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, leafKey := testLeaf(t, issuer, issuerKey, "http://issuer.example.test/issuer.der")
	entry := testEntry(t, leafDER, leafKey)

	completed, err := completeChain(context.Background(), entry, func(_ context.Context, rawURL string) ([]byte, error) {
		switch rawURL {
		case "http://issuer.example.test/issuer.der":
			return issuerDER, nil
		case "http://issuer.example.test/root.der":
			return rootDER, nil
		default:
			t.Fatalf("issuer URL = %q", rawURL)
			return nil, nil
		}
	})
	if err != nil {
		t.Fatalf("complete chain: %v", err)
	}
	if len(completed.Certificate.Certificate) != 3 {
		t.Fatalf("certificate count = %d, want 3", len(completed.Certificate.Certificate))
	}
	if string(completed.Certificate.Certificate[1]) != string(issuerDER) {
		t.Fatal("appended certificate is not the verified issuer")
	}
	if string(completed.Certificate.Certificate[2]) != string(rootDER) {
		t.Fatal("appended certificate is not the verified issuer root")
	}
}

func TestCompleteChainKeepsLeafWhenAIAIssuerDoesNotMatch(t *testing.T) {
	issuerDER, issuerKey := testIssuer(t)
	issuer, err := x509.ParseCertificate(issuerDER)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, leafKey := testLeaf(t, issuer, issuerKey, "http://issuer.example.test/issuer.der")
	entry := testEntry(t, leafDER, leafKey)
	otherDER, _ := testIssuer(t)

	completed, err := completeChain(context.Background(), entry, func(context.Context, string) ([]byte, error) {
		return otherDER, nil
	})
	if err == nil {
		t.Fatal("expected issuer validation failure")
	}
	if len(completed.Certificate.Certificate) != 1 {
		t.Fatalf("certificate count = %d, want original leaf only", len(completed.Certificate.Certificate))
	}
}

func testEntry(t *testing.T, leafDER []byte, leafKey *ecdsa.PrivateKey) Entry {
	t.Helper()
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ParsePEM(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func testIssuer(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test issuer"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

func testIntermediate(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, issuerURL string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "test intermediate"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IssuingCertificateURL: []string{issuerURL},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

func testLeaf(t *testing.T, issuer *x509.Certificate, issuerKey *ecdsa.PrivateKey, issuerURL string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "leaf.example.test"},
		DNSNames:              []string{"leaf.example.test"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IssuingCertificateURL: []string{issuerURL},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, &key.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}
