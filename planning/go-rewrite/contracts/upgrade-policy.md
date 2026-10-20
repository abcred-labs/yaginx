# Administrator-initiated upgrades

The only upgrade execution entry point is the administrator's existing
`yaginx upgrade` CLI. There is no automatic scheduler, worker, wrapper script,
or management API for upgrades. Administrators choose the release, maintenance
window, database migration, and order of nodes.

## CLI behavior

- Check the target with `yaginx upgrade --check --version <semver>` before
  installing it with `sudo yaginx upgrade --version <semver>`.
- Preserve `--migrate-database`, `--skip-preflight-health`, `--no-restart`,
  `--package-only` and its manifest/state/lock overrides, and `--config`.
  Database migration requires explicit administrator selection.
- Preserve HTTPS manifest validation, immutable package URLs, SHA-256,
  download/install, same-version no-op, downgrade rejection, the shared upgrade
  lock, readiness validation, and package rollback after readiness failure.
  Lock contention is a visible error.
- Retired `upgrade auto [status|on|off]` and `--auto-worker` invocations return
  usage error `2` before loading configuration or invoking any external work.
- There is no release publication-age gate. Manifest metadata validation
  continues to apply. Database migrations remain forward-only.

## Configuration compatibility

The authoritative and embedded runtime schemas and defaults contain only
manual upgrade settings. Before validating local or shared configuration,
discard `upgrade.enabled`, `upgrade.minPublishedAge`, and
`upgrade.checkInterval`, regardless of their values or types. Report the
retirement through configuration warnings on local and shared startup loads.
These fields never schedule work, including when `enabled` was `true`.

Compatibility covers loading, applying, validating, saving and exporting shared
runtime settings, and local loading/setup/saving. Generated and exported values
omit the retired fields. Reading configuration does not rewrite disk files for
this compatibility change. `configVersion` stays `1`; all other unknown fields
retain the existing validation rules.

## Debian migration

Packages contain neither `yaginx-upgrade.timer` nor `yaginx-upgrade.service`.
The `preinst` stops and disables only the package-provided legacy timer before
unpack removes its unit. Cleanup is repeatable, tolerates absent units and
hosts without systemd, and fails visibly if an existing project timer cannot
be stopped or disabled. A dangling project enablement link may be removed;
administrator-supplied units are preserved.

No install hook stops or waits for an upgrade worker: it may be the process
running `dpkg`. Already-started work may finish. `postinst` reloads systemd and
preserves the existing initial-install and main-service lifecycle policy.
Configuration, logs and rollback packages are preserved.

Before migrating, the administrator disables the old timer, confirms an
existing worker and its installation have finished, then installs the chosen
release. See the [migration runbook](../../../docs/go-version-new-install-runbook.md#removing-the-legacy-upgrade-timer).

## Scope limitation

Recovery from `dpkg` installation failure is an existing independent risk and
is not changed by this removal. Package rollback does not reverse database
migrations. Record and resolve those risks separately from timer retirement.
