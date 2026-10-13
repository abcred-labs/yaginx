# Legacy compatibility boundary

This directory contains public documentation and reviewed adapters for legacy
database imports. It intentionally contains no customer data, credentials,
private certificates, or production DSNs.

The runtime migration package supports empty and v2 databases. v1 and .NET
schemas are inspected and rejected by default when their lineage is incomplete;
an operator must produce a sanitized export and run a separately reviewed
offline import into the public v2 baseline. Any adapter added here must be
idempotent, transactional, restartable, and covered by a fixture that contains
synthetic values only.
