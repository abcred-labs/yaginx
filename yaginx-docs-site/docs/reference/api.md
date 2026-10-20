---
sidebar_position: 8
title: Management API and MCP
---

# Management API and MCP

The versioned management contract is the repository's
[OpenAPI specification](https://github.com/abcred-labs/yaginx/blob/main/planning/go-rewrite/contracts/management-api.openapi.yaml).
The React console generates its TypeScript client from the same file and CI
fails if the generated client drifts.

## API access

The API is served on the management listener under `/api/...`. Login issues
short-lived access credentials and an HttpOnly refresh cookie. Long-lived MCP
or automation access uses a purpose-scoped API credential created in **System →
API credentials**.

Every response uses the documented envelope. Preserve `Act-Trace-Id` when
opening a support request. Do not put tokens in URLs or source control.

## MCP discovery

Give an MCP client this URL:

```text
https://<management-host>:<management-port>/.well-known/mcp.json
```

The discovery document contains the `/mcp` endpoint but no token. Configure the
client with:

```text
Authorization: Bearer <purpose-scoped-mcp-token>
```

The `diagnose_request` tool can query request history and related system
errors. Restrict the management listener and rotate the credential if it is
ever exposed.

## Contract and fixture checks

From `gateway/`, run:

```bash
go run ./cmd/contractlint
go run ./cmd/contractcheck
go run ./cmd/proxycheck
```

These checks validate the OpenAPI inventory, approved compatibility deviations,
fixture redaction metadata and proxy matrix coverage.
