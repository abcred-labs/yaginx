package certificates

import "testing"

func FuzzCertificateDecoders(f *testing.F) {
	f.Add([]byte("not a certificate"), []byte("not a private key"), []byte{})
	f.Add([]byte("-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n"), []byte(""), []byte(""))

	f.Fuzz(func(t *testing.T, chain, key, password []byte) {
		if len(chain)+len(key)+len(password) > 1<<20 {
			t.Skip()
		}
		_, _ = ParsePEM(chain, key, nil)
		_, _ = ParsePFX(chain, password)
	})
}
