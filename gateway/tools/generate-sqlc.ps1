$ErrorActionPreference = 'Stop'
$sqlc = Join-Path $env:USERPROFILE 'go\bin\sqlc.exe'
if (-not (Test-Path $sqlc)) { throw 'sqlc v1.29.0 is required; install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0' }
$version = & $sqlc version
if ($version -ne 'v1.29.0') { throw "sqlc version must be v1.29.0, got $version" }
& $sqlc generate
