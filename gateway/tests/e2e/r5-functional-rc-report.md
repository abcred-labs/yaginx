# R5 Functional RC verification report

Date: 2026-07-28

This is an evidence report, not an R5 completion declaration. Local functional
RC implementation is complete; R5 is `BLOCKED` in the roadmap until its
external Manual Testing Items are satisfied.

## Passed evidence

| Area | Command / environment | Result |
| --- | --- | --- |
| Go unit and contract suite | `go test -count=1 ./...` | PASS |
| Static analysis | `go vet ./...` | PASS |
| .NET compatibility baseline | `dotnet test backend/Yaginx.Core.Tests/Yaginx.Core.Tests.csproj --no-restore --nologo` | PASS; 76/76 tests passed against the retained pre-cutover backend |
| Decoder fuzz | Debian WSL Go 1.25: config, configsync, certificates, repository and management targets; plus listeners PROXY parser | PASS; mandatory decoder set and extra PROXY run completed without crash, panic or failure corpus |
| Filesystem hardening | Debian WSL: `go test ./internal/platform/atomicfile` | PASS; direct and parent symlink rejection, atomic output mode and file/parent fsync paths covered |
| PostgreSQL 16 baseline and role harness | `go test -p 1 -tags integration ./internal/database ./internal/configsync ./internal/repository ./internal/management ./internal/app ./internal/acme` | Current v2 gate: clean initialization, exact v1-to-v2 bridge eligibility, role permissions and config-event behavior run against PostgreSQL 16 in CI. |
| Traffic/report compatibility | PostgreSQL 16 harness: `go test -tags integration ./internal/repository -run TestTrafficContributionMaterializesAbsoluteMultiProcessTotals -count=1` | PASS; two process snapshots aggregate idempotently into minute/hour/day reports with status, integer duration average/max/buckets, `uap-go` browser/spider/OS dimensions and final-period persistence |
| OTLP trace exporter | `go test ./internal/observability -run TestTracingExportsRedactedHTTPSpan` | PASS; local HTTP/protobuf collector received a span while payload omitted Host, raw path, Authorization and token |
| Contract inventory | `go run ./cmd/contractlint`; `go run ./cmd/contractcheck` | Current v2 inventory has 57 operations; removed account aliases and legacy JSON wrappers are rejected. |
| sqlc reproducibility | `./tools/generate-sqlc.ps1` | PASS (sqlc v1.29.0) |
| React production build | `npm --prefix adminui run build` | PASS |
| React management deployment path | `go test -tags integration ./internal/app -run TestServeHostsBuiltReactAndManagementAPI -count=1 -v` | PASS in `yaginx_goal_ui4f8ea32047`; a real `serve` process loaded the built `adminui/dist` from `/yaginx_console/` and accepted login through its actual management listener against a newly migrated isolated schema |
| Management routing | `internal/management` and `internal/app` tests | v2 verifies that `/api/account/user_info` and `/api/account/auth_token` are absent, while `/api/account/refresh_token`, `/api`, `/yaginx_console/api`, and SPA deep links remain covered. |
| Management API fixture replay | `go test ./internal/management -run TestAccountFixturesReplayThroughGoRouter -count=1` | v2 fixtures replay through the Go router and validate status, JSON content type, envelopes, dynamic markers, and login/refresh Secure HttpOnly cookies. |
| Management error compatibility | `go test ./internal/management -run 'TestLegacyInvalidJSONAndStoreErrorsUse500Envelope|TestMissingWebsiteDomainAndNodeResponsesAreNull200|TestDatabaseGuardReturns503OnlyForAvailabilityFailure'` | PASS; legacy decode/business errors use 500, website/domain/node missing reads use 200 null, and only a failed live database availability check returns 503 |
| Management DB outage and LKG | `go test -tags integration ./internal/app -run TestServeRetainsLKGAndRejectsManagementWritesDuringDatabaseOutage -count=1 -v` | PASS in the PostgreSQL 16 harness; a test-owned TCP forwarder was closed after online startup, so existing/reconnect database sockets failed. Management writes returned a 503 envelope while the HTTP listener continued serving the loaded simple-response LKG. |
| LKG decode | `internal/app` plaintext snapshot test | PASS; empty snapshot rejected and valid plaintext snapshot accepted |
| Three-node config sync | `go test -tags integration ./internal/configsync -run 'TestPostgres(NotifyReachesThreeListeners|RevisionPollRecoversAfterConnectionTermination)' -count=1 -v` | PASS in `yaginx_goal_nt43a0d803f4` and `yaginx_goal_nt5b3b98d76e`; all three dedicated LISTEN connections received a website-triggered NOTIFY within two seconds, and a real PostgreSQL backend termination was recovered by revision polling without NOTIFY |
| Three-node serve config sync | PostgreSQL 16 harness: `go test -tags integration ./internal/app -run TestServePropagatesConfigToThreeApprovedNodes -count=1` | PASS; three independently approved `serve` instances with separate listeners, identities, snapshots and pools each observed 20 database-driven website response revisions through their public HTTP data-plane listener; the sorted P95 was below two seconds. |
| Isolated PostgreSQL migration | `go run ./cmd/schematest --schema <isolated-schema>` | v2 initializes a clean schema and verifies `yaginx_schema_migration` plus `yaginx_schema_version` in the named schema. |
| Management PostgreSQL flow | `go test -tags integration ./internal/management -run TestPostgresManagementReachableFlows -count=1 -v` | PASS in an isolated PostgreSQL 16 schema; router login upgrades legacy plaintext to salt/hash, registration/profile updates retain no plaintext, SMTP-backed recovery/reset, website/domain CRUD, node approval, operation-aware traffic/history/report DTOs and async request history all exercised against a newly migrated schema; persisted simple-response extra headers and history classification/list/detail fields were loaded and verified |
| Pebble ACME | `YAGINX_PEBBLE_DIRECTORY=<redacted test directory> go test -tags integration ./internal/acme -run TestPebbleHTTP01Issue -v` | PASS; HTTP-01 provider wired and certificate obtained |
| Race suite | Debian WSL Go 1.25.0 + gcc: `go test -race ./...` | PASS after the 2026-07-28 runtime-contract audit changes; every module package completed under the race detector. Windows Go remains `CGO_ENABLED=0`, so this Linux result is the authoritative local race evidence. |
| Proxy runtime high-risk E2E | `go test ./internal/proxy -run 'TestRuntimeE2E(StreamsLargeUploadSSEAndContinue|WebSocketUpgradeAndFrame)' -count=1` | PASS; real TCP listeners cover >3 MiB upload, `100-Continue`, flushed SSE, WebSocket upgrade/subprotocol and frame relay |
| Proxy runtime-config enforcement | `go test ./internal/proxy ./internal/listeners ./internal/app -run 'Test(ConfiguredRuntime|StartAppliesConfiguredLimits|RuntimeProxySettings|SPAStaticUsesContractCacheHeaders)' -count=1` | PASS; configured transport/listener/HTTP3 limits are applied, selective upstream `Expect: 100-continue` follows the host/path/method/body/Upgrade contract, legacy first-XFF blacklist behavior is fail-closed, and React index/assets cache headers follow the deployment contract |
| Proxy golden runtime fixtures | `go test ./internal/proxy -run TestRuntimeExecutesGoldenFixtures -count=1` | PASS; executes ordinary .NET HTTP fixtures through `proxy.Runtime`, including aliases, child-rule priority, request/response transforms, CORS, simple responses, redirect, query preservation, headers and upstream history classification. The test has an explicit registry for every fixture requiring a raw listener or fault injector. |
| Fixed listener topology | `go test ./internal/listeners -run 'TestStart(AcceptsProxyV1V2AndTLS|ServesHTTP3OnlyOnConfiguredUDPAddress)' -count=1` | PASS; real sockets verify PROXY v1/v2, PROXY-before-TLS, missing-header transport rejection on both proxy listeners, direct HTTP/3 and that the 9443-equivalent listener does not bind UDP. |
| Runtime contract audit regression | `go test -count=1 ./...`; `go vet ./...`; `go run ./cmd/contractlint`; `go run ./cmd/contractcheck`; `go run ./cmd/proxycheck`; `./tools/test-postgres-integration.ps1 -Distro Debian` | PASS; PROXY required-header enforcement, tracked request body limits, HTTP/3 body limit wrapper, LISTEN debounce/exponential retry, traffic excluded paths/current-period persistence, database-clock node stale detection/graceful drain timeout, configurable report-lock namespace and delayed minute/hour/day report reconciliation, active-node-watermark config-event retention, and expired-event incremental fallback all passed local unit, contract and PostgreSQL 16 regression gates. |
| Snapshot reload isolation | `go test ./internal/proxy -run TestRuntimeRetainsSnapshotForInFlightRequest -count=1` | PASS; an in-flight request completes against its entry snapshot while the next request uses the atomically swapped revision. |
| Proxy history classification | `go test ./internal/proxy ./internal/repository -count=1` and PostgreSQL management integration | PASS; no destination, upstream error and refused upstream map to 603/604 or upstream history classifications without changing client HTTP semantics; list/detail retain the React history fields |
| Proxy error primitives | `go test ./internal/proxy -run 'TestRuntimeClassifies(CanceledRequestContext|UpstreamResponseHeaderTimeout|RequestBodyReadFailure|RawUpstreamHeaderAndBodyFailures|PipelineExceptionAndInvalidTransport|UnknownRoundTripError)|TestResponseCaptureClassifiesClientWriteFailure' -count=1` | PASS; Runtime maps actual canceled contexts to 601, response-header timeout to 602, raw upstream close-before-headers to 605, client request body read failure to 607, declared-length truncated body to 606, client write failure to 608, recovered pipeline/transport exceptions to 609 and an injected non-network round-trip error to 610 without wrapping WebSocket `101` upgrade streams. |
| Container | Superseded by the current Hub-based `gateway/deploy/docker/Dockerfile` pipeline | Manual Testing Item: rebuild and verify CA/tzdata/adminui/non-root runtime layout |
| Release assets | Superseded by the current GitHub Actions GitHub Releases release path | Manual Testing Item: verify versioned deb/checksum/SBOM/installer upload and manifest-last ordering |
| Debian package | Superseded by the current `deploy/build-deb.sh` package layout | Manual Testing Item: verify adminui, conffile, `0600 root:root` config ownership, and all three systemd units |
| Fresh Debian installation and service | Superseded by the current package | Manual Testing Item: verify first-install config bootstrap and non-root ready health |
| Upgrade logic | Superseded by remote `upgrade --check`/install implementation | Manual Testing Item: verify shared flock, strict download, readiness and automatic rollback on real systemd |

## Artifacts

Historical `artifacts/r5-rc-20260727/` files are not evidence for the current
GHCR/GitHub Releases release pipeline. The current release format uses versioned checksum
and SPDX filenames and is intentionally unsigned; no release private key is
created in this repository.

Historical SHA-256 values are recorded in `artifacts/r5-rc-20260727/SHA256SUMS`:
the delivered amd64 Debian package and Linux binary were rebuilt after
the LKG compatibility repair.

## External ACME HTTP-01 acceptance

Date: 2026-07-28

`ACME-REAL-HTTP01` passed against the public test domain
`acme.example.invalid`. The run used two disposable PostgreSQL 16 containers with
separate PostgreSQL databases, accounts and certificates for staging and production; all
containers, test processes, secrets and unredacted logs were removed after
collection.

- The callback endpoint is `https://configurable.example.invalid`: HTTP currently redirects
  to HTTPS, so the runtime keeps redirects disabled and calls the HTTPS origin
  directly. A preflight set/get returned HTTP 200 with `code=200`.
- Two approved staging nodes obtained one valid order and both hot-loaded the
  same certificate (serial `2C9B8CE8DC3EE84CACBC8EE5255D1669EDED`). The staging
  CA log recorded `The server validated our request`.
- Production initial issuance hot-loaded trusted certificate serial
  `0616EED0E17FDAD155B176656AB906E5940C`. A transient secondary CAA lookup
  `SERVFAIL` was recorded as `retry_wait/temporary`; the worker retried and
  completed normally.
- After only the isolated production row was placed in the renewal window,
  a second real production issuance hot-loaded serial
  `06764EB892D170A316C2BB1125468F3FA69D`. There are two valid production
  orders; the certificate store reports one active certificate and one
  superseded certificate with `replaced_by_id` (`1|1`).

No DSN, key authorization, private key, runtime JWT material or complete ACME
account data was retained in this report.

## Open R5 gates

> **Historical status.** As of 2026-08-03, the mainline reports that Go has
> completed full production cutover and R6 observation is in progress. This
> report remains the immutable local-RC evidence record; production cutover
> facts, daily checks, and links to the completed external gates must be kept
> in [`r6-cutover-observation.md`](r6-cutover-observation.md). Do not edit the
> result rows below to fabricate production evidence.

- All remaining gates below are **Manual Testing Items**. The user requested that
  external-environment blockers not prevent later local implementation work;
  the corresponding local code and automated evidence are complete. These
  items remain release and cutover gates and are not approved by this report.
- **Manual Testing Item: API-DUAL-RUNTIME-GOLDEN.** Start the retained .NET
  host and Go management listener against separately reset, writable copies of
  the same synthetic PostgreSQL fixture. Supply the identical ASCII HS256 key
  and a deterministic SMTP sink, then compare every frozen API exchange. The
  local 72-fixture router replay is a strong handler boundary check, not a
  substitute for this two-runtime exercise. Do not use production credentials,
  database rows or SMTP.
- The local proxy matrix now has executable evidence for ordinary fixtures,
  listener transport, transport isolation, streaming/upgrade, snapshot reload
  and 601-610 classification. R5 still requires the documented performance,
  heap-stability and external traffic comparison gates; this evidence alone is
  not a release declaration.
  `TestRuntimeExecutesGoldenFixtures` deliberately skips these cases rather
  than treating its ordinary HTTP harness as proof.
- **Manual Testing Item: PROXY-PERFORMANCE-SOAK and DB-OUTAGE-RECOVERY-SOAK.**
  Run the fixed same-hardware benchmark, 30-minute heap/connection soak and
  10-minute database outage/recovery drill using
  [`../performance/README.md`](../performance/README.md). The local real-TCP
  outage integration test proves the behavior boundary but does not establish
  those production SLOs.
- **Manual Testing Item: PRODUCTION-TRUST-AND-SECRETS and
  SHADOW-CANARY-CUTOVER.** These require controlled production credentials or
  traffic-control permissions. Independent release signing is a v1.1+
  enhancement; do not create private keys, add production DSNs or perform a
  traffic switch from this worktree.
