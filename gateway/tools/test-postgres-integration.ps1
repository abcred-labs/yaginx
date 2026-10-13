[CmdletBinding()]
param(
    [string]$Distro = "Debian",
    [string]$Image = "postgres:18"
)

$ErrorActionPreference = "Stop"

if ($Image -notmatch '^[A-Za-z0-9._/-]+:[A-Za-z0-9._-]+$') {
    throw "PostgreSQL image must be a public image reference: $Image"
}

function Get-FreeTcpPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try {
        $listener.Start()
        return $listener.LocalEndpoint.Port
    }
    finally {
        $listener.Stop()
    }
}

function Invoke-Checked([string[]]$Arguments) {
    & wsl.exe -d $Distro -- @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "WSL command failed: $($Arguments -join ' ')"
    }
}

$gateway = Split-Path -Parent $PSScriptRoot
$repo = Split-Path -Parent $gateway
$name = "yaginx-it-" + [Guid]::NewGuid().ToString("N").Substring(0, 12)
$port = Get-FreeTcpPort
$user = "yaginx_test"
$password = "yaginx_test_pw"
$database = "yaginx_test"
$dsn = "postgres://$user`:$password@127.0.0.1`:$port/$database`?sslmode=disable"

try {
    Invoke-Checked @("docker", "run", "--rm", "-d", "--name", $name, "-e", "POSTGRES_USER=$user", "-e", "POSTGRES_PASSWORD=$password", "-e", "POSTGRES_DB=$database", "-p", "127.0.0.1`:$port`:5432", $Image)

    $ready = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        & wsl.exe -d $Distro -- docker exec $name pg_isready -U $user -d $database *> $null
        if ($LASTEXITCODE -eq 0) {
            $ready = $true
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) {
        & wsl.exe -d $Distro -- docker logs $name
        throw "PostgreSQL container did not become ready"
    }

    Push-Location $gateway
    try {
        npm --prefix "$repo\adminui" run build
        if ($LASTEXITCODE -ne 0) { throw "adminui build failed" }

        $env:YAGINX_TEST_DATABASE_URL = $dsn
        # Database-wide migration locking is intentional; run the schema-owning
        # packages serially so separate disposable schemas cannot form a DDL /
        # advisory-lock wait cycle during a single shared PostgreSQL harness.
        go test -p 1 -count=1 -tags integration ./internal/database ./internal/configsync ./internal/repository ./internal/management ./internal/app ./internal/acme
        if ($LASTEXITCODE -ne 0) { throw "PostgreSQL integration tests failed" }

        $schema = "yaginx_goal_" + [Guid]::NewGuid().ToString("N").Substring(0, 12)
        go run ./cmd/schematest --schema $schema
        if ($LASTEXITCODE -ne 0) { throw "schema test failed" }

        Get-Content -Raw "$gateway\deploy\postgres\roles.sql" | & wsl.exe -d $Distro -- docker exec -i $name psql -v ON_ERROR_STOP=1 -U $user -d $database
        if ($LASTEXITCODE -ne 0) { throw "runtime role grant script failed" }
        $roleCheck = & wsl.exe -d $Distro -- docker exec -i $name psql -tA -v ON_ERROR_STOP=1 -U $user -d $database -c "SELECT has_schema_privilege('yaginx_gateway', 'public', 'CREATE'), has_table_privilege('yaginx_gateway', 'website', 'SELECT')"
        if ($LASTEXITCODE -ne 0 -or $roleCheck.Trim() -ne "f|t") { throw "runtime role privileges are not least-privilege: $roleCheck" }
    }
    finally {
        Remove-Item Env:YAGINX_TEST_DATABASE_URL -ErrorAction SilentlyContinue
        Pop-Location
    }
}
finally {
    & wsl.exe -d $Distro -- docker stop $name *> $null
}
