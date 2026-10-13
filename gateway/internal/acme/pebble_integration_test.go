//go:build integration

package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

type pebbleUser struct {
	email        string
	registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *pebbleUser) GetEmail() string                        { return u.email }
func (u *pebbleUser) GetRegistration() *registration.Resource { return u.registration }
func (u *pebbleUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func TestPebbleHTTP01Issue(t *testing.T) {
	directory := os.Getenv("YAGINX_PEBBLE_DIRECTORY")
	if directory == "" {
		t.Skip("YAGINX_PEBBLE_DIRECTORY is not set")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	user := &pebbleUser{email: "goal@example.invalid", key: key}
	config := lego.NewConfig(user)
	config.CADirURL = directory
	config.HTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	client, err := lego.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	user.registration, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":200,"data":"ok"}`)) }))
	defer server.Close()
	if err := client.Challenge.SetHTTP01Provider(KeyValueProvider{BaseURL: server.URL, SetPath: "/set", GetPath: "/get"}); err != nil {
		t.Fatal(err)
	}
	issued, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: []string{"goal-acme.example.invalid"}, Bundle: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Certificate) == 0 || len(issued.PrivateKey) == 0 {
		t.Fatal("empty issued certificate")
	}
}
