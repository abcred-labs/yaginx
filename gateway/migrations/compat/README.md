# Legacy compatibility boundary

This directory contains public documentation and reviewed adapters for legacy
database imports. It intentionally contains no customer data, credentials,
private certificates, or production DSNs.

The runtime migration package supports empty and v2 databases. v1 and .NET
schemas are inspected and rejected by default when their lineage is incomplete.
For a .NET/PostgreSQL export, the reviewed `gateway/cmd/legacyimport` adapter
can import the public data tables into a v2 database. It opens the source with
`default_transaction_read_only=on`, validates the known schema, writes the
target in one transaction, repairs identity sequences, and uses upserts so an
operator can retry safely after an interrupted run.

The adapter intentionally never selects the legacy `account.password` column,
recovery code hashes/salts, HTTP headers/bodies, exception text/stack traces,
or certificate and private-key material. Passwords must be reset through the
public setup flow; ACME certificates must be re-issued or configured after
cutover. The export must still be sanitized before it is supplied to the
tool because arbitrary JSON metrics may contain application-specific data.
Any future adapter must remain idempotent, transactional, restartable, and
covered by a fixture that contains synthetic values only.
