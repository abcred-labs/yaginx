# Yaginx documentation site

This directory contains the public Yaginx documentation site built with
Docusaurus.

## Local development

```bash
npx --yes pnpm@8.15.9 install --frozen-lockfile
npx --yes pnpm@8.15.9 start
```

Open the URL printed by Docusaurus and use the documentation sidebar. The
content covers installation, configuration, websites and certificates,
operations, troubleshooting, the management API/MCP endpoint and release
support boundaries.

## Verification and build

```bash
npx --yes pnpm@8.15.9 run typecheck
npx --yes pnpm@8.15.9 run build
npx --yes pnpm@8.15.9 run serve
```

The GitHub Actions documentation workflow publishes the generated `build/`
directory to GitHub Pages from the default branch. Configure Pages to use the
GitHub Actions source before the first deployment.
