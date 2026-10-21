# Public GitHub migration runbook

This runbook is the operator sequence for the public repository and release
line. Run destructive or production steps only after the preceding evidence is
archived. Never place a real DSN, export, certificate, or credential in Git.

## Phase 0 — freeze and evidence

- Keep `D:\gitrepos\yaginx` unchanged and work from the reviewed staging tree.
- Record the source commit, release tag, database version, backup location, and rollback owner.
- Take a PostgreSQL backup of the database used for release verification and record its location.

## Phase 1 — validate the public tree

```powershell
cd gateway
go test -count=1 ./...
go vet ./...
go mod verify
cd ..
Run the exact sensitive-pattern scanner from the migration audit outside the
repository. Treat any match for an internal domain, real email, credential,
or private-key marker as a release blocker; do not copy those scanner literals
into this public runbook.
```

Run the Admin UI and documentation checks from `.github/workflows/ci.yml`.
The repository must contain one new root commit, no private remote, and no
ignored build output in the staged file list.

## Phase 2 — verify a clean install

- Initialize a new empty database with `yaginx database init` and confirm `yaginx database status` reports `current-v2`.
- Check primary keys, unique indexes, foreign keys, triggers, functions, migration markers, and identity sequences.
- Run the application health check and a login using a newly created account.
- Exercise website/domain changes, proxy traffic, request-history metadata, and backup/restore.

## Phase 3 — GitHub staging and release candidate

- Keep the repository private while running CI and the release candidate.
- Require all protected `main` checks and CODEOWNERS review.
- Confirm `ghcr.io/abcred-labs/yaginx` is **Public** in the package settings and verify anonymously. GHCR first returns a normal Bearer challenge; acquire the pull token anonymously, then request the manifest:

```powershell
$token = (Invoke-RestMethod `
  -Uri 'https://ghcr.io/token?service=ghcr.io&scope=repository:abcred-labs/yaginx:pull').token
$h = @{
  Authorization = "Bearer $token"
  Accept = 'application/vnd.docker.distribution.manifest.v2+json'
}
$response = Invoke-WebRequest `
  -Uri https://ghcr.io/v2/abcred-labs/yaginx/manifests/0.1.1 `
  -Headers $h -UseBasicParsing
if ($response.StatusCode -ne 200) { throw "GHCR manifest verification failed" }
$response.Headers['Docker-Content-Digest']
```

- Verify anonymous Release downloads, SHA-256 checksums, SBOM, installer HTTPS/version/architecture checks, and a clean Docker pull by digest.

## Phase 4 — public release gate

- Confirm the repository has no private domains, credentials, certificates, customer data, or private dependency sources.
- Confirm the database lifecycle and migration tests pass in CI.
- Confirm the old private repository is backed up and made read-only. Keep it as a recovery archive; do not use it as a release source.
- Change the GitHub repository visibility to Public only after the package, Release, CI, and security gates are green.
- Create the version tag and GitHub Release. Published migration files are append-only and must never be rewritten.

## Phase 5 — first seven days

- Monitor Secret Scanning, Push Protection, Dependabot, Actions, Release downloads, and GHCR pulls daily.
- Validate anonymous clone/install on a clean machine, including a digest-pinned GHCR pull and a basic health check.
- Record migration and installation feedback and ship a patch release for confirmed public-environment defects.
- Keep private integrations disabled unless explicitly configured by the operator.
