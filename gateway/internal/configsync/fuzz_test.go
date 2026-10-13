package configsync

import (
	"errors"
	"testing"
)

func FuzzLoadLKG(f *testing.F) {
	f.Add([]byte(`{"version":1,"snapshot":{}}`), false)
	f.Add([]byte("not-json"), false)
	f.Add([]byte("ignored"), true)

	f.Fuzz(func(t *testing.T, data []byte, readError bool) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = LoadLKG("/unused", func(string) ([]byte, error) {
			if readError {
				return nil, errors.New("injected read failure")
			}
			return data, nil
		})
	})
}
