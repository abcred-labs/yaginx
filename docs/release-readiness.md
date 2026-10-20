# Release readiness and support matrix

This page describes the support boundary for the public Yaginx releases and
the evidence required before calling a release production ready.

## Supported deployment matrix

| Area | Supported public scope |
| --- | --- |
| Operating system | Linux packages and systemd units on Debian based hosts |
| CPU architecture | `linux/amd64`; arm64 packages are not published yet |
| Database | PostgreSQL 15 or newer; CI and the release gate use PostgreSQL 18 |
| Runtime modes | Full PostgreSQL-backed `yaginx` and database-free `yaginx-core` rescue runtime |
| Proxy protocols | HTTP/1.1, HTTP/2, HTTP/3 on the HTTPS listener, WebSocket, SSE, and PROXY protocol where configured |
| Packaging | Versioned Debian packages, Docker image, checksums, SPDX SBOM and installer scripts |
| Upgrade | Online upgrade with readiness verification and package rollback; database migrations remain forward-only |
| Release trust | HTTPS, immutable release names and SHA-256 checksums; release artifacts are currently unsigned |

The management listener is separate from business listeners. The console is at
`/`, management APIs are under `/api/...`, health is exposed at
`/health/live` and `/health/ready`, metrics at `/metrics`, and MCP discovery at
`/.well-known/mcp.json`. The old `/yaginx_console/`, `/swagger`, `/hangfire`
and `/helps` paths are not supported by the Go runtime.

## Evidence required for a release

Every release tag must pass the repository CI jobs:

- Go build, unit tests, vet, contract lint, management fixture coverage and
  proxy fixture coverage.
- PostgreSQL integration tests for clean initialization, migration lineage,
  config synchronization, permissions, ACME state and legacy import.
- Admin UI OpenAPI drift check, tests and production build.
- Documentation typecheck and production build.
- Secret scan, dependency scan, SBOM generation and container build.

The following checks need an isolated Linux environment and are not safely
reproducible on every contributor workstation:

- .NET-to-Go dual-runtime comparison against a writable synthetic database.
- Pebble/ACME HTTP-01 acceptance, certificate renewal and multi-node locking.
- Fresh Debian install, non-root service startup, upgrade, readiness failure,
  migration failure and automatic rollback.
- Fixed-hardware performance and heap/connection soak, plus the ten-minute
  PostgreSQL outage/recovery drill.
- Production trust, release-signing and shadow-canary exercises.

These checks must be recorded in
[`gateway/tests/e2e/r5-functional-rc-report.md`](../gateway/tests/e2e/r5-functional-rc-report.md)
and [`gateway/tests/e2e/r6-cutover-observation.md`](../gateway/tests/e2e/r6-cutover-observation.md)
before a release is described as having completed the external acceptance
gates. Automated CI success alone is not a production traffic declaration.

## Upgrade and recovery rules

- Back up PostgreSQL before every migration or import.
- New installations use the v2 baseline and `yaginx database init`.
- Existing v2 databases use `yaginx database migrate`.
- Current v1 databases must pass through the final 0.1.x compatibility release
  before installing 1.x. The 1.x binary refuses ambiguous or partial lineage.
- .NET imports use the read-only source adapter and a new target database;
  passwords must be reset and ACME certificates must be re-issued after cutover.
- A failed package readiness check can roll back the package, but a failed
  database migration is forward-only and requires operator verification.

## Security boundaries

Keep the management port restricted to administrators. Store the runtime
configuration with mode `0600`, never put DSNs or tokens in shell history, and
do not commit credentials, private keys, production data or unredacted fixture
captures. Report suspected vulnerabilities through GitHub private vulnerability
reporting as described in [`SECURITY.md`](../SECURITY.md).
