---
sidebar_position: 4
title: Configuration
---

# Configuration

The runtime configuration is JSON and is validated against the authoritative
[runtime JSON Schema](https://github.com/abcred-labs/yaginx/blob/main/planning/go-rewrite/contracts/runtime-config.schema.json).
The complete field table is maintained in the
[installation runbook](https://github.com/abcred-labs/yaginx/blob/main/docs/go-version-new-install-runbook.md).

## Important sections

| Section | Controls |
| --- | --- |
| `database` | PostgreSQL DSN, pool limits and statement timeouts |
| `ports` and `listeners` | Business, proxy, management, HTTP/3 and PROXY protocol listeners |
| `management` | Node-local CIDR allowlist for the console, API, health, metrics and MCP |
| `proxy` | Timeouts, body/header limits, concurrency and connection pools |
| `configSync` | PostgreSQL notification and polling behavior for multi-node reloads |
| `acme` | ACME directory, renewal window, HTTP-01 and cross-node key/value settings |
| `trafficAggregation` and `requestHistory` | Sampling, queue bounds, retention and redaction limits |
| `observability` | JSON logs, metrics, health paths and optional OTLP tracing |
| `upgrade` | Manual upgrade manifest URL, package component and readiness timeout |

## Management exposure

The management listener is intentionally separate from public business
listeners. Set `management.allowedCIDRs` to the administrator networks and
also restrict the TCP port in the host or cloud firewall. A configuration
change requires a service restart because this is node-local security policy,
not shared PostgreSQL state.

## Secrets

Do not put credentials in shell history, Git, logs or issue reports. PostgreSQL
stores runtime JWT material, certificate keys and SMTP credentials; access to
the database and its backups must be protected as strongly as the configuration
file. Use environment variables or a secret manager for migration and import
DSNs.

Automatic upgrades have been removed. Legacy `upgrade.enabled`,
`upgrade.minPublishedAge` and `upgrade.checkInterval` are stripped before
validation with configuration warnings. Loading does not rewrite the local
file; generated and exported configuration omits these keys. `configVersion`
remains `1`, and other unknown fields still fail validation.
