# Proxy golden fixtures

Proxy fixtures are captured E2E cases rather than synthetic unit expectations.
Each matrix row in `planning/go-rewrite/contracts/proxy-compatibility-matrix.md`
needs at least one JSON fixture containing the input listener/configuration,
upstream observation, client observation, and expected request-history status.

Each JSON fixture must include `listener` (port/transport), JSON `config`,
the client request/result, whether and how the isolated upstream was reached,
and the expected request-history classification. Metadata-only entries are
rejected. `matrixLine` refers to the data-row line in the authoritative
Markdown matrix and is checked by `go run ./cmd/proxycheck`. Use
`source: "contract"` only for an explicit entry in
`planning/go-rewrite/contracts/approved-deviations.json`; it must also carry
the matching `approvalId`. This includes approved Go-only safety behavior and
legacy paths that cannot emit a safe, durable golden exchange. The checker
rejects an unapproved, unused, or mismatched ID.

When the golden exchange uses HTTP trailers, the completed response trailers
are recorded under `client.trailers`; a declared trailer with an empty value is
evidence and must not be silently omitted.

For an explicitly configured large streaming response, `client.bodyBytes` and
`client.bodySha256` retain byte-accurate evidence without committing the full
test payload. Request-stream evidence is recorded on `upstream.bodyBytes` and
`upstream.bodySha256` by the isolated upstream.

Capture a safe plan against the local golden host and the isolated echo
upstream with:

```powershell
go run ./cmd/proxyfixturecapture -base-url http://127.0.0.1:18080 -plan tests/contract/proxy/capture-plan.template.json -out tests/contract/proxy
```

`proxyfixtureupstream` is the local echo upstream used by the template. It
returns only method/Host/path so the committed fixture proves forwarding and
query preservation without retaining external traffic. Do not mark W0 complete
until every matrix row has a captured fixture.
