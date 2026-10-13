# Component reuse and provenance

Yaginx does not import private repositories or external `internal/...` packages.
Reusable behavior must be implemented in this repository or depend on a public
package with a documented license. Any adapted code must record its public
source, commit, license, and the local differences in the target file.

Before a public release, maintainers must confirm that every adapted file has a
traceable public source or is first-party code. Code with unverifiable private
provenance must be removed or rewritten.

The runtime remains PostgreSQL-backed. Filesystem storage, private control
planes, private telemetry services, Redis-only components, and internal CI
services are not runtime requirements.

## Public dependency rules

- Go modules must resolve from public module proxies or public repositories.
- Node packages must resolve from the public npm registry.
- Docker images must use public registries and pinned versions.
- Secrets, private endpoints, internal hostnames, and local machine paths are
  prohibited in source, tests, fixtures, and documentation.
