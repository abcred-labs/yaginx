package repository

import (
	"encoding/json"
	"testing"
)

func TestNormalizeLegacyRuleIDsPreservesLargeDecimalDigits(t *testing.T) {
	const id = "2066836992053874700"
	value, exists, err := decodeWebsiteCollection([]byte(`[{"ruleId":` + id + `}]`))
	if err != nil || !exists {
		t.Fatalf("decode exists=%v err=%v", exists, err)
	}
	normalized := normalizeLegacyRuleIDs(value)
	body, err := json.Marshal(normalized)
	if err != nil || string(body) != `[{"ruleId":"`+id+`"}]` {
		t.Fatalf("normalized=%s err=%v", body, err)
	}
}
