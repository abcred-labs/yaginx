---
sidebar_position: 1
title: Yaginx documentation
---

# Yaginx documentation

Yaginx is an open-source HTTP/HTTPS gateway with a React management console
and PostgreSQL-backed configuration, certificates, telemetry and node state.

## Choose a path

- [Install and start a new node](./getting-started.md)
- [Help system and support information](./help-system.md)
- [Configure runtime limits and listeners](./configuration.md)
- [Publish a website and request an ACME certificate](./proxy-and-certificates.md)
- [Operate, upgrade and recover a deployment](./operations/maintenance.md)
- [Migrate a database](./migrations.md)
- [Monitor health, metrics and traces](./monitoring.md)
- [Troubleshoot management API and proxy errors](./operations/troubleshooting.md)
- [Use the management API and MCP endpoint](./reference/api.md)
- [Apply the security rules](./security.md)
- [Contribute changes](./contributing.md)
- [Check the release support boundary](./release-readiness.md)

The repository also contains the detailed
[Debian installation runbook](https://github.com/abcred-labs/yaginx/blob/main/docs/go-version-new-install-runbook.md),
[migration contract](https://github.com/abcred-labs/yaginx/blob/main/docs/migrations.md),
and [PostgreSQL operations standard](https://github.com/abcred-labs/yaginx/blob/main/docs/postgresql-operations-standard.md).

## Runtime endpoints

| Endpoint | Purpose |
| --- | --- |
| `/` | React management console |
| `/api/...` | Authenticated management API |
| `/.well-known/mcp.json` | MCP client discovery |
| `/mcp` | MCP tools; use an API credential with purpose `mcp` |
| `/health/live` | Process liveness |
| `/health/ready` | Database, node and configuration readiness |
| `/metrics` | Prometheus-format metrics |

The management listener is separate from business listeners and should be
restricted with the node-local `management.allowedCIDRs` setting and a host or
cloud firewall. The old `/yaginx_console/`, `/swagger`, `/hangfire` and
`/helps` paths belong to the retired runtime and are not supported here.
