---
sidebar_position: 5
title: Websites and certificates
---

# Websites and certificates

Create websites and domains in the management console, then publish a fully
validated configuration snapshot. A route can select a destination host, match
the request host/path, apply request or response transforms, and enable CORS,
maintenance mode or access-control rules.

## Typical flow

1. Create a web domain and point DNS to the public HTTP/HTTPS listener.
2. Create a website with an upstream destination and host aliases.
3. Add child rules only after the default route works; route priority is
   explicit and route IDs are not priorities.
4. Verify the generated snapshot and reload from the management console.
5. Exercise both HTTP and HTTPS traffic, including redirects and WebSocket or
   SSE paths when those protocols are used.

## ACME HTTP-01

Configure the ACME email and directory, then request a certificate from the
domain view. The challenge value is stored in PostgreSQL and served through
the node-local key/value endpoint so that every approved node can answer the
challenge. Keep the management listener reachable only from the configured
administrative CIDRs; the public challenge path is handled by the business
listener.

Use the staging directory and a disposable domain while testing. Certificate
private keys, account keys and SMTP credentials must never be committed or
included in fixture captures.

## Failure behavior

If a candidate configuration is invalid or conflicting, Yaginx keeps the last
validated proxy snapshot active. During a database outage, the data plane can
continue serving the loaded snapshot while management writes return an error;
observe `/health/ready`, request history and the system event log before making
another configuration change.
