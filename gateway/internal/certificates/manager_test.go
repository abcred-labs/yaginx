package certificates

import (
	"crypto/tls"
	"errors"
	"testing"
)

func TestEmptyManagerFailsClosed(t *testing.T) { m:=NewManager(); if _,err:=m.GetCertificate(nil);err==nil{t.Fatal("expected no-SNI failure")} }

func TestHostFilterRejectsUnknownBeforeLookup(t *testing.T) {
	m := NewManager()
	m.SetHostFilter(func(string) bool { return false })
	_, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "portal.nextep-smart-order.feinian.net"})
	if !errors.Is(err, ErrUnknownSNI) {
		t.Fatalf("err = %v", err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = m.GetCertificate(&tls.ClientHelloInfo{ServerName: "asia.chineselearning.com"})
	})
	if allocs > 1 {
		t.Fatalf("unknown SNI allocated %v times", allocs)
	}
}
