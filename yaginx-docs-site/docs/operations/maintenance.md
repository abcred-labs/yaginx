---
sidebar_position: 6
title: Operations and upgrades
---

# Operations and upgrades

## Daily checks

```bash
systemctl status yaginx --no-pager
curl --fail http://127.0.0.1:9090/health/live
curl --fail http://127.0.0.1:9090/health/ready
```

Review the service journal, `/metrics`, request history and the management
diagnostics page. Keep PostgreSQL backups and the most recent successful
restore test outside the application host.

## Upgrade

An administrator chooses the version and maintenance window. Check the target
version before installing it:

```bash
sudo yaginx upgrade --check --version <semver>
sudo yaginx upgrade --version <semver>
```

The runner verifies the immutable release manifest, downloads the package,
restarts the service, waits for readiness and rolls back the package if the
new service cannot become ready. Use `--no-restart` for package staging and
`--package-only` only when configuration is unavailable and the package itself
must be repaired.

For an existing v2 database with pending migrations, add
`--migrate-database`. Database migrations are forward-only; a failed migration
can require manual backup restoration and schema verification even when the
package is rolled back.

Automatic upgrades have been removed. Before installing the new package on an
older host, disable the project-provided `yaginx-upgrade.timer` with
`systemctl disable --now yaginx-upgrade.timer`, then confirm the existing
`yaginx-upgrade.service` worker and its package installation have completed.
Do not stop the worker. The new package retires only the old project timer;
administrator units, configuration, logs and rollback packages are preserved.
Legacy automatic-upgrade configuration keys are ignored with configuration
warnings and omitted from new exports.

## Rescue runtime

`yaginx-core` is a database-free, static rescue data plane. It is intentionally
manual-only and does not provide management APIs, telemetry, ACME,
configuration synchronization or upgrades. Stop the normal service before
taking over its business ports, and restore the normal service after recovery.

See the [full runbook](https://github.com/abcred-labs/yaginx/blob/main/docs/go-version-new-install-runbook.md)
for package ownership, legacy timer migration, logs, rollback evidence and firewall changes.
