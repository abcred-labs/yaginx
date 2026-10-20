---
sidebar_position: 10
title: Security
---

# Security

- Restrict the management listener with `management.allowedCIDRs` and a host
  or cloud firewall. Do not expose it on an unrestricted public interface.
- Store `/etc/yaginx/config.json` as `root:root` with mode `0600`.
- Keep PostgreSQL DSNs, JWT material, API credentials, SMTP credentials,
  certificate keys and backups in a protected secret store. Never place them
  in shell history, source control, logs or fixture captures.
- Use purpose-scoped API credentials for MCP and automation, rotate them after
  suspected exposure, and never put bearer tokens in URLs.
- Verify release SHA-256 checksums over HTTPS. Public artifacts are currently
  unsigned; signing is tracked as a future release enhancement.

Report suspected vulnerabilities through the repository's
[security policy](https://github.com/abcred-labs/yaginx/blob/main/SECURITY.md)
instead of a public issue.
