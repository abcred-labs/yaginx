# Yaginx Go Design Notes

This directory holds the design notes and machine contracts for the Yaginx Go
gateway. The current production contract is v2.

## Status

- Go is the only production runtime in this repository.
- v2 uses the OpenAPI contract and the `database init|migrate|status`
  lifecycle. New databases are created by a DBA and initialized by the
  application only after database access has been granted.

## Runtime

- Go 1.25, PostgreSQL 15 or later, React static assets, and one `yaginx` binary.
- `gateway/` is the production module; deployment scripts follow the machine
  contracts in this directory.
- Only the v2 schema is supported. New databases use the clean v2 baseline.

## Documents

1. [Management API OpenAPI](contracts/management-api.openapi.yaml)
2. [v2 PostgreSQL baseline](../../gateway/migrations/v2/00001_v2_baseline.sql)
3. [v2 database roles](../../gateway/deploy/postgres/roles.sql)
4. [Runtime configuration JSON Schema](contracts/runtime-config.schema.json)
5. [Manual upgrade contract](contracts/upgrade-policy.md)

## Machine Contracts

- [Management API OpenAPI](contracts/management-api.openapi.yaml)
- [Runtime configuration JSON Schema](contracts/runtime-config.schema.json)

When narrative documentation conflicts with a machine contract, the machine
contract takes precedence. The v2 OpenAPI contract and v2 baseline are the
only current management and database contracts.
