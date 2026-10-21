# Contributing to Yaginx

1. Fork the repository and create a focused branch.
2. Keep changes first-party or document the public source, version, and license
   of adapted code.
3. Do not commit credentials, certificates, private endpoints, local paths,
   generated dependency directories, or production data.
4. Run the relevant checks before opening a pull request:

```bash
cd gateway
go test ./...
go vet ./...
cd ../adminui
pnpm install --frozen-lockfile
pnpm run api:check
pnpm test
pnpm run build
pnpm exec playwright install chromium   # once
pnpm exec playwright test
cd ../yaginx-docs-site
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 run typecheck
npx --yes pnpm@8.15.9 run build
cd ..
node scripts/check-doc-links.mjs
```

The admin UI uses the Modra component library, installed from
`https://npm.feinian.net/` (read access needs no credentials; see
`adminui/.npmrc`). The Playwright smoke tests run against the production build
and intercept `/api` in the browser, so they need no backend. To run the dev
server against a gateway, set `VITE_PROXY_HTTP` (default
`http://127.0.0.1:9090/`).

Migration files are append-only after release. A schema change must include a
forward migration, rollback or recovery notes where applicable, and tests for
empty, current, and interrupted upgrade states. Pull requests must pass the
GitHub Actions secret, dependency, license, and SBOM checks.
