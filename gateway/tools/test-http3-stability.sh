#!/bin/sh
set -eu

count="${1:-20}"
case "$count" in
  *[!0-9]*|'') echo "count must be a positive integer" >&2; exit 2 ;;
esac
[ "$count" -gt 0 ]

cd "$(dirname "$0")/.."
go test -count="$count" ./internal/listeners -run '^TestStartServesHTTP3OnlyOnConfiguredUDPAddress$'
