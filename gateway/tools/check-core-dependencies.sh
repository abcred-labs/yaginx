#!/usr/bin/env sh
set -eu

deps=$(go list -deps -f '{{if .Module}}{{.ImportPath}}{{end}}' ./cmd/yaginx-core)
forbidden='github.com/jackc/pgx|go.opentelemetry.io/otel|yaginx-gateway/internal/(acme|auth|bootstrap|config|configsync|database|management|observability|repository|telemetry|upgrade)'
if printf '%s\n' "$deps" | grep -E "$forbidden" >/dev/null; then
  echo "yaginx-core has a forbidden dependency:" >&2
  printf '%s\n' "$deps" | grep -E "$forbidden" >&2
  exit 1
fi
