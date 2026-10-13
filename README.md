# Yaginx

Yaginx is a Go HTTP/HTTPS gateway with a React management console and
PostgreSQL-backed configuration, certificates, telemetry, and node state.

## Repository layout

- `gateway/`: the production Go service, migrations, packages, and contract tests.
- `adminui/`: the React management console.
- `docs/go-version-new-install-runbook.md`: Debian installation and operational guidance.
- `planning/go-rewrite/`: the compatibility record and the R7 runtime roadmap.

## Development

```bash
cd gateway
go test -count=1 ./...
go vet ./...
go run ./cmd/contractlint
go run ./cmd/contractcheck

cd ../adminui
pnpm run build
```

## Deployment and migration

The production release pipeline builds the full `yaginx` binary/package and a
separately versioned `yaginx-core` Debian rescue package. The standard installer
installs core only when it is absent; normal `yaginx` upgrades never replace it.
Configure PostgreSQL 15 or later and run:

```bash
/usr/bin/yaginx database init --config /etc/yaginx/config.json
/usr/bin/yaginx database status --config /etc/yaginx/config.json
/usr/bin/yaginx serve --validate --config /etc/yaginx/config.json
```

See [the Go installation runbook](docs/go-version-new-install-runbook.md) for
the required permissions, runtime configuration, and service verification.

For an existing current Go deployment, back up the database and run
`yaginx database migrate --config /etc/yaginx/config.json` instead. Older v1 and .NET schemas follow [the compatibility procedure](docs/migrations.md); unsafe or ambiguous schemas are rejected without writes.
