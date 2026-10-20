---
sidebar_position: 2
title: Help system
---

# Help system

The public help site is this documentation portal. The Go runtime does not
serve a `/helps` page; package installations serve the React management console
at `/` on the management listener.

For a deployment-specific procedure, start with the
[installation runbook](https://github.com/abcred-labs/yaginx/blob/main/docs/go-version-new-install-runbook.md).
It covers the first-start setup token, PostgreSQL roles, listener protection,
node approval, health checks, upgrades, rollback and MCP credentials.

When reporting a problem, include the release version, operating system,
configuration schema version, UTC timestamp, endpoint, HTTP status and the
`Act-Trace-Id` value. Remove credentials, tokens, private keys, DSNs, request
bodies and private hostnames before sharing logs.
