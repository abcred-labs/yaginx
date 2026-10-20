---
sidebar_position: 6
title: Migrations
---

# Migrations

Back up PostgreSQL and verify that the backup can be restored before every
schema change. The application never performs a destructive down migration.

## New installations

Create the database and application role as a DBA, then initialize the clean
v2 baseline:

```bash
sudo yaginx database init --config /etc/yaginx/config.json
sudo yaginx database status --config /etc/yaginx/config.json
```

## Existing installations

Run `yaginx database migrate` for an existing v2 database. A failed migration
is forward-only: keep the failed schema for diagnosis, verify the backup and
restore it only through the approved database recovery procedure.

Current v1 databases must first pass through the final compatible 0.1.x
release. Ambiguous or partial lineage is rejected without writes.

For .NET imports, use the reviewed read-only importer against a new target
database. Reset passwords and re-issue ACME certificates after cutover.

See the repository's [migration contract](https://github.com/abcred-labs/yaginx/blob/main/docs/migrations.md)
and [public migration runbook](https://github.com/abcred-labs/yaginx/blob/main/docs/public-migration-runbook.md)
for the complete checks and rollback boundary.
