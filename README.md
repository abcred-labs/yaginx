# Yaginx

English | [简体中文](README.zh-CN.md)

Yaginx is a Go HTTP/HTTPS gateway with a React management console and
PostgreSQL-backed configuration, certificates, telemetry, and node state.
It is designed for Debian/systemd deployments and also ships as a container
image and a database-free rescue runtime.

## What is included

- HTTP/1.1, HTTP/2, HTTP/3, WebSocket, SSE, streaming uploads and downloads.
- Host and path routing, request/response transforms, CORS, maintenance mode,
  proxy protocol, access control and ACME HTTP-01 certificates.
- A management console with websites, domains, certificates, analytics,
  request history, IP blacklist, node control, runtime settings and API
  credentials.
- PostgreSQL-backed migrations, multi-node configuration synchronization,
  health and metrics endpoints, OpenTelemetry traces, and safe last-known-good
  proxy snapshots during database failures.
- Debian packages, Docker images, a `yaginx-core` rescue binary, checksums,
  SPDX SBOMs and an administrator-run online upgrade/rollback command.

Upgrades are manual: first run `yaginx upgrade --check --version <semver>`, then
`sudo yaginx upgrade --version <semver>` during a maintenance window. Database
migrations require the administrator to explicitly add `--migrate-database`.
Behind the Great Firewall, pass download mirrors with
`--github-mirror https://github-mirror.abc.red --release-assets-mirror https://gh-release-assets.abc.red`
(these override `YAGINX_GITHUB_MIRROR` / `YAGINX_RELEASE_ASSETS_MIRROR` and the
`upgrade.*Mirror` settings).
Automatic upgrade timers and CLI controls have been removed; see the
[migration runbook](docs/go-version-new-install-runbook.md#removing-the-legacy-upgrade-timer)
for older installations.

The public management contract is the
[OpenAPI document](planning/go-rewrite/contracts/management-api.openapi.yaml).
The runtime configuration contract is the
[JSON Schema](planning/go-rewrite/contracts/runtime-config.schema.json).

## Quick start

Requirements: Linux amd64, PostgreSQL 15 or newer, and a DNS name that points
to the proxy listener when ACME certificates are required. The current public
release does not publish arm64 packages and release artifacts are not signed.

1. Install the latest Debian release with
   [`install-yaginx.sh`](https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh),
   or build the package from source. Run as root (or prefix `bash` with
   `sudo -E` to keep the environment variables).

   **Global (default):**

   ```bash
   curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | bash
   ```

   **Mainland China:** set the two download mirrors first, then run the same
   installer:

   ```bash
   export YAGINX_GITHUB_MIRROR=https://github-mirror.abc.red
   export YAGINX_RELEASE_ASSETS_MIRROR=https://gh-release-assets.abc.red
   curl -fsSL https://github.com/abcred-labs/yaginx/releases/latest/download/install-yaginx.sh | bash
   ```

   The mirrors only speed up downloads; package URLs and SHA-256 checksums are
   still verified against the GitHub release. See the
   [installation guide](docs/installation-guide.md) for details.
2. Create an empty PostgreSQL database and application role. The setup wizard
   is available on the management port (9090 by default) on first start.
3. Restrict the management port to administrators, complete the wizard, and
   approve the first node.
4. Verify the service and database:

   ```bash
   sudo yaginx database status --config /etc/yaginx/config.json
   sudo yaginx serve --validate --config /etc/yaginx/config.json
   curl --fail http://127.0.0.1:9090/health/ready
   ```

The full procedure, permissions, firewall guidance and recovery commands are
in the [installation runbook](docs/go-version-new-install-runbook.md).

## Documentation

- [Public documentation site](https://abcred-labs.github.io/yaginx/)
- [Installation guide](docs/installation-guide.md)
- [Installation and operations runbook](docs/go-version-new-install-runbook.md)
- [Migration contract](docs/migrations.md)
- [Public migration runbook](docs/public-migration-runbook.md)
- [PostgreSQL operations standard](docs/postgresql-operations-standard.md)
- [Release readiness and support matrix](docs/release-readiness.md)
- [Release packaging and publishing](docs/release-packaging.md)
- [Contributing](CONTRIBUTING.md) and [security policy](SECURITY.md)

The management listener serves the React console at `/`, the API at `/api/...`,
health at `/health/live` and `/health/ready`, metrics at `/metrics`, and MCP
discovery at `/.well-known/mcp.json`. The retired `/yaginx_console/`,
`/swagger`, `/hangfire` and `/helps` paths are not part of the Go runtime.

## Development and verification

```bash
cd gateway
go test -count=1 ./...
go vet ./...

cd ../adminui
pnpm install --frozen-lockfile
pnpm run api:check
pnpm test -- --run
pnpm run build
pnpm exec playwright test

cd ../yaginx-docs-site
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 run typecheck
npx --yes pnpm@8.15.9 run build
```

The PostgreSQL integration suite, ACME/Pebble tests, systemd installation and
upgrade drills, performance soak, and production traffic exercises require an
isolated Linux environment and are tracked in the release record.

## License

Yaginx is released under the [GNU GPL v3](LICENSE).
