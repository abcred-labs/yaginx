# Yaginx Go Rewrite

This directory is the design and delivery record for the Yaginx migration to
Go. The current production contract is v2. Historical v1 compatibility
materials are retained only as archived design evidence and are not operating
instructions.

## Status

- R0-R6 implementation and cutover records are complete. External release
  gates that require production credentials, a writable legacy host, fixed
  hardware or real systemd remain tracked in the R5 evidence report and are
  not claimed by local CI.
- Go is the only production runtime in this repository. The pre-Go release
  artifact and `migration-pre-Go` tag are retained in controlled external
  storage for rollback evidence.
- Frozen API/proxy fixtures, compatibility observations, and migration records
  remain in the repository as behavior provenance. They do not require a legacy
  runtime or build dependency.
- v2 uses the OpenAPI contract and `database init|migrate|status` lifecycle.
  New databases are created by a DBA and initialized by the application only
  after database access has been granted.

## Runtime

- Go 1.25, PostgreSQL 16, React static assets, and one `yaginx` binary.
- `gateway/` is the production module; deployment scripts and acceptance
  evidence follow the machine contracts in this directory.
- Only the complete stable Go v1 schema can be bridge-migrated to v2. New
  databases use the clean v2 baseline; legacy import and root-level migration
  commands are not supported.

## Documents

1. [Management API OpenAPI](contracts/management-api.openapi.yaml)
2. [v2 PostgreSQL baseline](../../gateway/migrations/v2/00001_v2_baseline.sql)
3. [v2 database roles](../../gateway/deploy/postgres/roles.sql)
4. [Runtime configuration JSON Schema](contracts/runtime-config.schema.json)
5. [Proxy compatibility matrix](contracts/proxy-compatibility-matrix.md)
6. [Manual upgrade contract](contracts/upgrade-policy.md)
7. [Archived v1 design records](archive/v1-design-records/README.md)

## Machine Contracts

- [Management API OpenAPI](contracts/management-api.openapi.yaml)
- [Runtime configuration JSON Schema](contracts/runtime-config.schema.json)
- [Proxy compatibility matrix](contracts/proxy-compatibility-matrix.md)

When narrative documentation conflicts with a machine contract, the machine
contract takes precedence. The v2 OpenAPI contract and v2 baseline are the
only current management and database contracts.
