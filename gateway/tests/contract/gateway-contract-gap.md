# Gateway-to-contract baseline gap report

Generated baseline date: 2026-07-26. Re-run the machine-readable portion with
`go run ./cmd/contractcheck` from `gateway/`.

| Contract area | Required baseline | Current gateway evidence | Status |
| --- | --- | --- | --- |
| Management inventory | OpenAPI is the v2 source of truth | The checker parses the canonical inventory and rejects removed legacy endpoints. | Closed |
| API fixtures | Every fixture-required v2 operation has success and failure coverage | `contractcheck` verifies coverage and every approved adapter against its named evidence. | Closed with approved exceptions |
| Password recovery repeatability | Fixture capture must be repeatable | `AccountPasswordRecoveryRepository.AddAsync` explicitly inserts `recovery.Id`; the service never assigns it, so the first row has `id=0` and every subsequent start returns PostgreSQL `23505`. The approved adapter uses prefix-scoped data and cleanup. | Approved test adapter |
| Password recovery verify/reset success | Real golden success fixture without external mail | `verify_email` generates a random code then calls the enabled SMTP provider; this host has SMTP disabled and returns 500. `reset` requires that unobservable random code. The approved adapter uses an isolated deterministic SMTP sink. | Approved test adapter |
| SMTP API authentication | All protected settings actions reject missing or invalid Bearer consistently | v2 returns the normal `401` envelope for GET and POST rather than preserving the historic middleware failure. | Closed |
| Management listener topology | Matrix requires an independent management listener | `YaginxApplicationOptions` binds only ports 8080/9080/8443/9443. `YaginxAppConfigure` maps `MapAdminConsole()` into that same endpoint pipeline. The legacy captures remain evidence; Go isolates all management endpoints on 127.0.0.1:9090 and returns 404 on business listeners. | Approved security deviation |
| Missing route-value transforms | Matrix requires Go to reject a candidate snapshot when `PathPattern`/`QueryRouteParameter` refers to an absent route value | The default .NET website route has no named `remainder` value. Golden `PathPattern: /mapped/{**remainder}` still forwards as `/mapped`; `QueryRouteParameter: route, Set: remainder` silently leaves the query unchanged. The Go fail-closed candidate behavior is an approved safety deviation. | Approved security deviation |
| Proxy pipeline exception | Matrix row 173 requires a classified explicit pipeline/configuration exception | A disposable website with an unrecognised YARP transform does not expose a 609 path in the .NET host: YARP omits the invalid route and the request receives 404. The named contract fixture records the Go classification. | Approved exception |
| Proxy fixtures | One fixture for every compatibility-matrix row | The 81-row checker rejects metadata-only entries and requires listener/config/client/upstream/history evidence. 70 real .NET captures cover the observable legacy behavior; 13 named approved contract fixtures cover Go-only safety behavior or unobservable legacy paths. Large payloads retain byte counts and SHA-256 rather than raw data. | Closed with approved exceptions |
| Fixture provenance | Fixed PostgreSQL fixture and .NET HTTP host; secrets/PII redacted | `contractcapture` records and validates redacted HTTP exchanges, but must be run against a writable disposable fixture. | Open |
| Database lifecycle | Empty databases initialize from a clean v2 baseline; only the complete stable Go v1 tracker may bridge | Integration coverage rejects partial and legacy schemas, validates marker lineage, and verifies config events. | In progress |
| Gateway v2 contract | Management router, proxy runtime, migrations, configuration and API types conform to v2 contracts | The functional gate remains open until the PostgreSQL lifecycle CI job and generated frontend API drift check run on every release. | In progress |

The read-only shared test database may be used for schema inspection and
read-only baseline checks. It is not an acceptable W0 fixture source because
success/error workflows require controlled, mutable fixture data.

Run `YAGINX_CONTRACT_DATABASE_URL=<secret> go run ./cmd/contractdbinspect`
with a secret supplied only through the process environment. The command opens
a PostgreSQL `READ ONLY` transaction and reports only server version, role,
supported v1 table/column coverage, and table privileges.
