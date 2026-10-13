package management

import (
	"errors"
	"strings"
)

var ErrInvalidStatusCodePattern = errors.New("statusCodePattern must contain exactly three digits or x characters")

// NormalizeStatusCodePattern validates the three-character HTTP status matcher
// and normalizes wildcard characters to lowercase. An empty value disables the
// filter.
func NormalizeStatusCodePattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", nil
	}
	if len(pattern) != 3 {
		return "", ErrInvalidStatusCodePattern
	}
	for _, char := range pattern {
		if (char < '0' || char > '9') && char != 'x' && char != 'X' {
			return "", ErrInvalidStatusCodePattern
		}
	}
	return strings.ToLower(pattern), nil
}
