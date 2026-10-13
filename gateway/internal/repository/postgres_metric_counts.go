package repository

import (
	"encoding/json"
	"sort"
	"strconv"
)

// Count-map primitives shared by every aggregate query in this package.

func decodeReportCounts(raw []byte) map[string]int64 {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(values))
	for key, value := range values {
		var number int64
		if err := json.Unmarshal(value, &number); err == nil {
			out[key] = number
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			if parsed, parseErr := strconv.ParseInt(text, 10, 64); parseErr == nil {
				out[key] = parsed
			}
		}
	}
	return out
}

func addCounts(target, values map[string]int64) {
	for key, value := range values {
		target[key] += value
	}
}

func ratio(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func metricItems(values map[string]int64) []map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		if key == "total" {
			continue
		}
		items = append(items, map[string]any{"type": key, "value": values[key]})
	}
	return items
}
