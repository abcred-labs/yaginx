```bash
# On a first start, Yaginx serves a temporary setup page on port 9090 when the
# runtime configuration or database is not ready: http://<server>:9090/.
# The setup page itself is public, but every setup API requires the one-time
# token printed in the root-only systemd journal:
#
#   journalctl -u yaginx -n 50 --no-pager
#
# Restrict TCP/9090 to the administrator's IP in the cloud security group or
# host firewall. The token expires after 15 minutes and is invalidated after
# initialization; close the firewall rule when setup completes.
# First Debian installation enables and starts yaginx on systemd hosts;
# upgrades preserve the existing service state. Restart yaginx to generate a
# new token if it expires. HTTP sends credentials without encryption: use an
# SSH tunnel or HTTPS ingress when traversing untrusted networks.

# Upgrade modes:
#   yaginx upgrade                         online upgrade and readiness rollback
#   yaginx upgrade --skip-preflight-health  skip old-service readiness only
#   yaginx upgrade --no-restart              install package, do not restart
#   yaginx upgrade --package-only            package-only repair when config is bad
#
# Removing config.json cannot produce a usable default by itself: the database
# DSN is required. The setup page writes a complete, validated config.json only
# after the operator submits and verifies the runtime DSN. In unattended or
# restricted environments, write that file with mode 0600 and use the explicit
# CLI flow below instead.
# The browser wizard collects PostgreSQL host, port, database, username,
# password, and SSL mode separately and assembles the DSN on the server. It also
# offers first-node listener options, ACME certificate email, and creates the
# first administrator account.
# The page checks the database lineage and only offers initialization for an
# empty database or migration for an existing v2 schema. V1 databases must be
# migrated with the final 0.1.x release before installing 1.0.x.
# The DBA must create the empty PostgreSQL database and application role before
# opening the wizard. The wizard initializes the Yaginx schema; it never runs
# CREATE DATABASE or asks for a temporary PostgreSQL administrator connection.

# The explicit CLI flow remains available for controlled deployments:
/usr/bin/yaginx database init --config /etc/yaginx/config.json
psql "$YAGINX_DBA_DSN" -f /usr/share/yaginx/deploy/postgres/roles.sql
/usr/bin/yaginx database status --config /etc/yaginx/config.json
/usr/bin/yaginx serve --validate --config /etc/yaginx/config.json

# Optional: rebuild daily reports from hourly reports after a historical repair.
/usr/bin/yaginx report backfill --config /etc/yaginx/config.json \
  --from 2026-01-01 --to 2026-08-03 --statement-timeout 5m

# The management listener defaults to allowing IPv4 and IPv6 sources through
# the local CIDR layer (0.0.0.0/0 and ::/0). Configure
# management.allowedCIDRs in this node's /etc/yaginx/config.json and restart
# the node after changing it. This node-local setting is intentionally not part
# of PostgreSQL shared runtime, because management exposure can differ by node.

```
