# Rewrite contract CI

This directory contains only development-time parsers for the Go rewrite
contracts; it is not part of the gateway runtime or release artifact.

From `gateway/`, run the full local contract lint:

```powershell
go test ./internal/contract ./tests/contract
go run ./cmd/contractlint
Push-Location tools/contract-lint
npm ci --ignore-scripts
npm run check-mermaid
npm audit --omit=dev --audit-level=high
Pop-Location
```

The R0 fixture gates are intentionally separate and fail until actual .NET
golden captures are available:

```powershell
go run ./cmd/contractcheck
go run ./cmd/proxycheck
```
