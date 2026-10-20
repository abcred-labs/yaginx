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
go run ./cmd/contractlint
cd ../adminui
pnpm install --frozen-lockfile
pnpm run api:check
pnpm test
pnpm run build
cd ../yaginx-docs-site
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 run typecheck
npx --yes pnpm@8.15.9 run build
cd ..
node scripts/check-doc-links.mjs
```

Migration files are append-only after release. A schema change must include a
forward migration, rollback or recovery notes where applicable, and tests for
empty, current, and interrupted upgrade states. Pull requests must pass the
GitHub Actions secret, dependency, license, and SBOM checks.
