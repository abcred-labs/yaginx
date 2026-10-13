# R0 acceptance report — 2026-07-27

## Evidence inventory

- OpenAPI inventory: 41 operations, comprising 36 legacy operations and five
  Go-only `gateway_node` extensions.
- API evidence: 66 redacted .NET exchanges plus six named
  `approved-test-adapter` fixtures. Each adapter fixture is linked to one
  entry in `planning/go-rewrite/contracts/approved-deviations.json`.
- Proxy evidence: 70 .NET E2E exchanges plus 15 named `source: "contract"`
  fixtures for approved Go safety behavior or legacy paths that cannot emit a
  durable exchange.
- The approval manifest contains 21 entries. Both checkers reject an unknown,
  unused, mismatched, or evidence-less approval ID.

## Approved compatibility decisions

- Management UI/API, health, and metrics are isolated on the CIDR-restricted
  management listener (default `127.0.0.1:9090`); business listeners return
  404 for `/yaginx_console`.
- Missing route values reject the candidate Go snapshot; .NET's silent
  forwarding behavior remains retained as golden evidence only.
- Password-recovery and SMTP controller gaps use isolated deterministic test
  adapters only. They never connect to production SMTP or retain credentials.

## Validation

Validation ran from `gateway/` against this working tree, based on Git commit
`04a3cded95914314ac99ad335da1718a13199889`:

```text
CGO_ENABLED=1 CC=C:\msys64\ucrt64\bin\gcc.exe go test -race ./...  PASS
CGO_ENABLED=0 go test ./...                                      PASS
CGO_ENABLED=0 go vet ./...                                       PASS
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" ./cmd/yaginx  PASS
go run ./cmd/contractlint                                        PASS
go run ./cmd/contractcheck                                      PASS (72 API fixtures, 21 approvals)
go run ./cmd/proxycheck                                         PASS (85 proxy fixtures, 21 approvals)
```

No database migration or disposable database row was needed for this contract
closure. Future adapter capture runs must use the approved `yaginx_dev`
database and clean all prefix-scoped rows before reporting completion.
