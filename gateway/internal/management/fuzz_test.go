package management

import (
	"io"
	"strings"
	"testing"
)

// FuzzDecodeTypedJSONBody guards the one decoder every domain handler shares.
// A login request stands in for the general shape: a flat object of strings.
func FuzzDecodeTypedJSONBody(f *testing.F) {
	f.Add([]byte(`{"email":"user@example.invalid","password":"pass"}`))
	f.Add([]byte(`[]`))
	f.Add([]byte("not-json"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		_, _ = DecodeTypedJSONBody[LoginRequest](io.NopCloser(strings.NewReader(string(raw))), 64<<10)
	})
}
