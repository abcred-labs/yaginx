---
sidebar_position: 11
title: Contributing
---

# Contributing

Start with the repository's [contribution guide](https://github.com/abcred-labs/yaginx/blob/main/CONTRIBUTING.md)
and keep changes focused on a documented behavior or contract.

Before opening a pull request, run the checks for the area you changed:

```bash
cd gateway
go test -count=1 ./...
go vet ./...
go run ./cmd/contractlint
go run ./cmd/contractcheck
go run ./cmd/proxycheck

cd ../adminui
pnpm install --frozen-lockfile
pnpm run api:check
pnpm test -- --run
pnpm run build

cd ../yaginx-docs-site
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 run typecheck
npx --yes pnpm@8.15.9 run build
```

Do not commit credentials, private keys, production data or unredacted
captures. Changes to the OpenAPI contract must update the generated Admin UI
client and its drift check in the same pull request.
