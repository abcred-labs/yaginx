# Management API golden fixtures

Each committed fixture is one redacted .NET HTTP exchange using the schema in
`internal/contract.Fixture`. File names are
`<operationId>.<scenario>.json`; scenarios are `success`, `auth`,
`parameter_error`, `business_error`, `missing_row`, and `db_failure`.

Capture against the fixed PostgreSQL fixture, never a developer database. Run
the prefix-scoped cleanup and seed commands first; they create only synthetic
`w0-fixture-<run>` rows and remove them after collection:

```powershell
go run ./cmd/contractcapture -base-url http://127.0.0.1:8080 -plan C:\secure\yaginx-capture-plan.json -out tests/contract/fixtures
```

The capture plan is deliberately outside the repository because it may contain
credentials or authentication headers. The command rejects unredacted secret
and PII fields before writing a fixture. Dynamic values must be represented as
`${TOKEN}`, `${EMAIL}`, `${TID}`, `${NOW}`, or another `${...}` marker.

For a legitimate per-run value that is not a secret, add a `normalize` entry to
the external capture-plan case, for example
`{"location":"response.body","pointer":"/tid","value":"${TID}"}`.
The pointer must exist in the actual response; capture refuses to silently
normalize a different response shape. All planned exchanges are validated in
memory before any fixture file is written.

Do not hand-author expected response bodies: fixtures are captured from the
.NET golden runtime and reviewed before being committed.

The current .NET `password_recovery/start` repository inserts a literal
`id = 0`, so the test cleanup is mandatory before each start capture. The
remaining recovery success paths cannot be captured without a disposable SMTP
sink and a backend fix for the fixed-ID insert; see `gateway-contract-gap.md`.
