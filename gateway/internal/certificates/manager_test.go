package certificates

import "testing"

func TestEmptyManagerFailsClosed(t *testing.T) { m:=NewManager(); if _,err:=m.GetCertificate(nil);err==nil{t.Fatal("expected no-SNI failure")} }
