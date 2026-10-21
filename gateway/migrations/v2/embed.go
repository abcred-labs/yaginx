package v2

import "embed"

// Files contains the v2 migration lineage applied to every database.
//
//go:embed *.sql
var Files embed.FS
