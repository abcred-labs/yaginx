---
sidebar_position: 9
title: Release readiness
---

# Release readiness

The public release boundary is documented in the repository's
[release readiness and support matrix](https://github.com/abcred-labs/yaginx/blob/main/docs/release-readiness.md).
It records supported operating systems, PostgreSQL versions, amd64-only
artifacts, unsigned release status, upgrade behavior and the external gates
that require an isolated Linux environment.

Before a release tag, CI must pass Go, PostgreSQL integration, contract,
Admin UI, documentation, security scan, SBOM and container jobs. The remaining
external acceptance items are dual-runtime comparison, ACME/Pebble, Debian
install/upgrade/rollback, performance and database outage soak, and any
production trust or canary exercise approved for that release.
