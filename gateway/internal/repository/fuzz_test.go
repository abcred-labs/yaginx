package repository

import (
	"encoding/json"
	"testing"
)

func FuzzSnapshotTransformValidation(f *testing.F) {
	f.Add([]byte(`{"PathPattern":""}`))
	f.Add([]byte(`{"QueryRouteParameter":"{id}"}`))
	f.Add([]byte("not-json"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		_ = validateSnapshotTransforms(raw)
		var value map[string]any
		if json.Unmarshal(raw, &value) == nil {
			_, _ = simpleResponseHeaders(value)
		}
	})
}
