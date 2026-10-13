package repository

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Accessors for the untyped maps the JSONB columns force on us. Typed read
// models are preferred; these remain where the column itself is schemaless.

func pageValues(input map[string]any) (int, int) {
	pageIndex, pageSize := intFrom(input, "pageIndex", 1), intFrom(input, "pageSize", 50)
	if raw, ok := input["pageIndex"].(string); ok {
		if parsed, err := strconv.Atoi(raw); err == nil {
			pageIndex = parsed
		}
	}
	if raw, ok := input["pageSize"].(string); ok {
		if parsed, err := strconv.Atoi(raw); err == nil {
			pageSize = parsed
		}
	}
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return pageIndex, pageSize
}

func stringValue(v map[string]any, key, def string) string {
	if s, ok := v[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return def
}

func boolValue(v map[string]any, key string) bool { b, _ := v[key].(bool); return b }

func intFrom(v map[string]any, key string, def int) int {
	if number, ok := intValue(v, key); ok {
		return int(number)
	}
	return def
}

func intValue(v map[string]any, key string) (int64, bool) {
	switch n := v[key].(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case string:
		i, e := strconv.ParseInt(n, 10, 64)
		return i, e == nil
	case json.Number:
		i, e := n.Int64()
		return i, e == nil
	}
	return 0, false
}
