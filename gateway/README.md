# Yaginx Gateway

`gateway/` is the production implementation of the Yaginx gateway, including
the full PostgreSQL-backed service and the independent Debian rescue runtime.

## Development commands

```powershell
go run ./cmd/yaginx version
go run ./cmd/yaginx doctor --config config/yaginx.example.json
go test ./...
go vet ./...
go generate ./...
go build ./cmd/yaginx
```

SQL query code is generated reproducibly with
`./tools/generate-sqlc.ps1` (sqlc v1.29.0).

## Commands

- `serve`: validates runtime configuration and starts the management listener,
  four business ports, HTTP/3, config synchronization and asynchronous
  telemetry. `--validate` does not start listeners; it validates the runtime
  configuration and database, and initializes the shared PostgreSQL JWT
  material on its first successful run.
- `database init|migrate|status`: initializes an empty v2 database, upgrades
  only an existing v2 schema, or reports the database lifecycle state.
- `report backfill --from YYYY-MM-DD --to YYYY-MM-DD`: materializes daily
  reports from existing hourly reports one UTC day per transaction. It starts
  no listeners and can be repeated safely after an interrupted historical
  backfill; `--statement-timeout` defaults to `5m`.
- `doctor`: validates static configuration and can optionally test PostgreSQL.
- `user`: `yaginx user --config /etc/yaginx/config.json` lists account IDs and
  email addresses. `yaginx user password --id <account-id> --config
  /etc/yaginx/config.json` displays the selected account and prompts twice for
  a hidden replacement password. It never accepts a password command-line
  argument or reads one from standard input.
- `version`: prints build metadata.
- `yaginx-core --snapshot /var/lib/yaginx/core/runtime.json`: starts the
  database-free static rescue data plane. `--validate` validates its sole local
  input without binding listeners; it never serves management APIs, telemetry,
  ACME, config synchronization, or upgrades.
- `upgrade --check` verifies the latest immutable GitHub Release Debian release; `sudo
  yaginx upgrade` verifies and installs it. When `yaginx.service` is active,
  this is the supported online upgrade path: it restarts the service, waits for
  readiness, and automatically rolls back if readiness fails. Upgrades are
  initiated by an administrator. Each install emits timestamped phase diagnostics
  to the invoking terminal and to the root-only rolling
  `/var/log/yaginx/upgrade.log`; readiness failures include
  the health endpoint's safe diagnostic reasons and append the relevant
  `yaginx.service` journal tail before rollback.
  For an existing V2 database with pending migrations, use
  `sudo yaginx upgrade --migrate-database`; the target version's migration
  runs after package installation and before the service restart. A migration
  failure rolls back the package without restarting the candidate, but the
  database may require operator verification because schema changes are not
  automatically reversed.
  To install a specific published release, use `sudo yaginx upgrade --version
  <semver>` (or combine `--check` with `--version`). This fetches the immutable
  version manifest, refuses downgrades, and keeps the same readiness and rollback
  checks. Check the chosen version with `--check --version <semver>` before
  installing it during a maintenance window.
  `upgrade verify --candidate <file>` prints a local candidate SHA-256 and
  `upgrade rollback --current <file> --previous <file>` retains the prior
  atomic rollback behavior.

  For a broken old service, `--skip-preflight-health` skips only the check of
  the old readiness endpoint and still verifies the new service after restart.
  `--no-restart` installs the package without restarting or readiness checks.
  `--package-only` does not read `config.json`, does not contact PostgreSQL,
  and never touches systemd; it is intended for repairing hosts with a
  missing or invalid configuration. It accepts `--manifest-url`,
  `--state-directory`, and `--lock-file` overrides and cannot run database
  migrations.

  Automatic upgrades have been removed. Before migrating an old installation,
  disable its timer and confirm any running worker has completed; follow the
  [migration procedure](../docs/go-version-new-install-runbook.md#removing-the-legacy-upgrade-timer).

## Release assets

GitHub Actions publishes the full `yaginx` package and a separately versioned
`yaginx-core` Debian rescue package. The normal upgrade runner updates only
`yaginx`; core updates require the explicit core installer. Each release has
immutable packages, checksums, SBOM, installer, and manifest. Releases are
currently unsigned; upgrade verification relies on HTTPS, immutable object
names, and SHA-256.
The GitHub Release assets include versioned Debian packages, checksums, SPDX SBOMs, installers, and manifests. Releases currently target Linux amd64 and are not signed; verification relies on HTTPS, immutable object names, and SHA-256. The container build is:

```powershell
docker build -f gateway/deploy/docker/Dockerfile -t yaginx:<version> .
docker run --rm yaginx:<version> version
```

## PostgreSQL 18 integration tests

On Windows development hosts, use the Debian WSL Docker daemon with the local
Windows Go toolchain. The script creates one disposable
`postgres:18` container, runs the database-backed
management/config-sync/serve gates using isolated schemas, then stops and
removes the container:

```powershell
Set-Location gateway
.\tools\test-postgres-integration.ps1 -Distro Debian
```

The script does not connect to production infrastructure. Pebble, public DNS
HTTP-01, release signing and production traffic tests remain Manual Testing
Items.

For Debian/systemd installs, use `deploy/systemd/yaginx.service` for normal
operation. `yaginx-core.service` is deliberately manual-only and conflicts with
the main service so it can safely take over the same business ports.

The full architecture and migration constraints live in
`../planning/go-rewrite/`.

For the public support boundary and the release acceptance checklist, see
`../docs/release-readiness.md`. The operator-facing installation, migration,
upgrade, recovery and MCP procedures are maintained in
`../docs/go-version-new-install-runbook.md`.
