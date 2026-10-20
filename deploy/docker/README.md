# Docker Compose quick start

Runs PostgreSQL and Yaginx with one command (linux/amd64).

```bash
cd deploy/docker
cp .env.example .env     # set POSTGRES_PASSWORD (letters and digits only)
docker compose up -d
```

Then open the Admin UI at <http://127.0.0.1:9090> and complete setup.
`docker compose up` is safe to re-run: the `init` service initializes an empty
database, applies pending migrations, and does nothing otherwise.

- Pin `YAGINX_VERSION` to a release (for example `0.2.0`) for production.
- The management port is bound to loopback. Only change `YAGINX_MANAGEMENT_BIND`
  behind a firewall.
- Runtime config lives in the `yaginx-data` volume (`config.json`, mode 0600) and
  contains the database password. Database data lives in `postgres-data`.
- Behind a registry mirror, set `YAGINX_IMAGE` / `POSTGRES_IMAGE` in `.env`.
- Upgrade: bump `YAGINX_VERSION`, then `docker compose pull && docker compose up -d`.
