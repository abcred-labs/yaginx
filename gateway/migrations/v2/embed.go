package v2

import "embed"

// Files contains the clean v2 baseline lineage. Empty databases never use
// the historic Goose bridge migrations.
//
//go:embed *.sql
var Files embed.FS
