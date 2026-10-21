# Database migration contract

Yaginx 1.x has one public bootstrap schema: the v2 baseline in
[`gateway/migrations/public/baseline-v2.sql`](../gateway/migrations/public/baseline-v2.sql).
The executable embeds the same baseline at `gateway/migrations/v2/00001_v2_baseline.sql`
and applies later numbered files in order.

The migration tracker is `yaginx_schema_migration` and the v2 marker is
`yaginx_schema_version`. Migration files are forward-only and append-only after
release. The runner takes a PostgreSQL advisory lock, verifies the complete
lineage, and fails closed for an ambiguous or partially modified schema.

## Supported upgrade routes

- Empty database: run `yaginx database init`.
- Existing v2 database: run `yaginx database migrate`; the operation is
  repeatable after interruption.
- Any other schema (including a partially initialized or modified one): the
  command refuses to continue. Keep a backup and inspect the schema before
  retrying; the binary never guesses at unknown data or rewrites it implicitly.

Production credentials, real databases, and unredacted exports are never
checked in.

## Required validation

Before a release, run the database integration suite against PostgreSQL 15 or
newer and verify empty initialization, every v2 upgrade point, retry after an
interrupted migration, backup/restore, table/index/constraint/trigger/function
counts, and least-privilege roles. Assert
that plaintext passwords are absent from logs and exported artifacts.
