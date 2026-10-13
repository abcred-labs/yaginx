# Database migration contract

Yaginx 1.x has one public bootstrap schema: the v2 baseline in
[`gateway/migrations/public/baseline-v2.sql`](../gateway/migrations/public/baseline-v2.sql).
The executable embeds the same baseline at `gateway/migrations/v2/00001_v2_baseline.sql`
and applies later numbered files in order. New installations never replay the
old v1 or .NET migration history.

The migration tracker is `yaginx_schema_migration` and the v2 marker is
`yaginx_schema_version`. Migration files are forward-only and append-only after
release. The runner takes a PostgreSQL advisory lock, verifies the complete
lineage, and fails closed for an ambiguous or partially modified schema.

## Supported upgrade routes

- Empty database: run `yaginx database init`.
- Existing v2 database: run `yaginx database migrate`; the operation is
  repeatable after interruption.
- Current v1 database: keep a backup and run the final 0.1.x compatibility
  migrator, then upgrade to 1.x and run `database migrate`. The 1.x binary does
  not guess at v1 data or rewrite it implicitly.
- .NET database: create a read-only compatibility report with
  `gateway/cmd/contractdbinspect`, export a sanitized backup, and perform an
  offline, reviewed import into a new public baseline. The import must map
  accounts, websites, domains, certificates, traffic, history, and settings;
  it must never copy passwords or private keys. A failed or ambiguous report
  stops before any write.

The compatibility work belongs in [`gateway/migrations/compat`](../gateway/migrations/compat)
and must be versioned as a separate release artifact. Production credentials,
real databases, and unredacted exports are never checked in.

## Required validation

Before a release, run the database integration suite against PostgreSQL 15 or
newer and verify empty initialization, every v2 upgrade point, retry after an
interrupted migration, backup/restore, table/index/constraint/trigger/function
counts, row counts for a sanitized import, and least-privilege roles. Assert
that plaintext passwords are absent from logs and exported artifacts.
