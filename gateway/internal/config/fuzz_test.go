package config

import (
	"encoding/json"
	"testing"
)

func FuzzRuntimeConfigurationJSON(f *testing.F) {
	f.Add([]byte(`{"configVersion":1}`))
	f.Add([]byte(`{"ports":{"http":0}}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"security":{"jwtIssuer":null}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			return
		}
		if err := applyRuntimeDefaults(value); err != nil {
			return
		}
		_ = applyRuntimeEnvironment(value)
		_ = validateRuntime(value)
	})
}
