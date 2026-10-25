# AI agent guidance

## Releasing a new version

Full process: `docs/release-packaging.md`. Releases are triggered by pushing an
annotated `vMAJOR.MINOR.PATCH` tag created from the latest `main`.

Version bump rules:

- **Default: bump only the third number (PATCH)**, e.g. `v0.3.1` -> `v0.3.2`.
  This applies to every routine release, even if it contains new features.
- Bump MAJOR or MINOR **only when the user explicitly asks** for it (e.g. "release
  v0.4.0", "bump the minor version"). Never decide to bump them on your own, and
  ask if unsure.
- Bumping MINOR resets PATCH to 0; bumping MAJOR resets MINOR and PATCH to 0.
- Find the latest version with `git tag --sort=-v:refname | head -1`.
- Never move, delete or reuse a published tag; fix problems with a new PATCH release.
