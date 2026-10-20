---
sidebar_position: 3
title: Getting started
---

# Getting started

## Requirements

- Linux amd64 host with systemd for Debian package installations.
- PostgreSQL 15 or newer and an empty application database.
- A DNS name pointing to the proxy listener when using ACME HTTP-01.
- A protected management network for the management listener (9090 by
  default).

## Install a release

Download the installer from the tagged [GitHub Release](https://github.com/abcred-labs/yaginx/releases)
or build a package from the repository. The public release currently publishes
Linux amd64 packages only. Verify the downloaded package with the matching
SHA-256 checksum before installing it.

## First start

1. Create the PostgreSQL database and application role as described in the
   [installation runbook](https://github.com/abcred-labs/yaginx/blob/main/docs/go-version-new-install-runbook.md).
2. Start `yaginx`. When configuration or schema initialization is incomplete,
   it serves the setup wizard on the management port.
3. Read the one-time setup token from the root-only systemd journal, restrict
   the port to the administrator's network, and complete the wizard.
4. Approve the first node and confirm `/health/ready` returns success.

The wizard never creates a PostgreSQL database. The DBA must create the empty
database and application role first. Store `/etc/yaginx/config.json` as
`root:root` with mode `0600`.

## Verify the service

```bash
sudo yaginx database status --config /etc/yaginx/config.json
sudo yaginx serve --validate --config /etc/yaginx/config.json
curl --fail http://127.0.0.1:9090/health/ready
```

For Docker, build the admin UI first and use
`gateway/deploy/docker/Dockerfile`. The container runs as the unprivileged
`yaginx` user and expects the runtime configuration and PostgreSQL connection
to be supplied by the deployment.
