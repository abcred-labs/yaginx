---
sidebar_position: 7
title: Troubleshooting
---

# Troubleshooting

## The service is live but not ready

Check the readiness response and journal first:

```bash
curl -i http://127.0.0.1:9090/health/ready
journalctl -u yaginx --since "15 minutes ago" --no-pager
```

Common causes are an unreachable PostgreSQL DSN, incomplete migration lineage,
an unapproved node, or a configuration snapshot that failed validation. Run
`yaginx doctor --config /etc/yaginx/config.json` and then
`yaginx database status --config /etc/yaginx/config.json`.

## A management request fails

Management responses use the `code`, `msg`, `errMsg`, `tid` and `data` envelope.
Capture the HTTP status, endpoint, UTC time and `Act-Trace-Id`; search the
journal by the same trace ID. Do not share Authorization, Cookie, DSN,
password, private key or request body values.

The management listener is CIDR-restricted. A request from a non-allowed
address is expected to fail before authentication. A missing or expired bearer
token returns `401`; a valid token without the required capability returns
`403`.

## Proxy traffic fails

Check the selected host and child-rule priority, destination URL, upstream TLS
settings, DNS resolution and the request-history status classification. A
candidate conflict or invalid transform leaves the previous validated snapshot
active. If PostgreSQL is unavailable, management writes may fail while already
loaded proxy traffic continues.

## Report a defect

Search existing [issues](https://github.com/abcred-labs/yaginx/issues), reproduce
with a disposable configuration, and attach redacted logs plus the release
version. Use the [security policy](https://github.com/abcred-labs/yaginx/blob/main/SECURITY.md)
for suspected vulnerabilities instead of a public issue.
