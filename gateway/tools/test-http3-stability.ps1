[CmdletBinding()]
param(
    [ValidateRange(1, 100)]
    [int]$Count = 20
)

$ErrorActionPreference = "Stop"
$gateway = Split-Path -Parent $PSScriptRoot

Push-Location $gateway
try {
    $countArgument = "-count=$Count"
    go test $countArgument ./internal/listeners -run '^TestStartServesHTTP3OnlyOnConfiguredUDPAddress$'
    if ($LASTEXITCODE -ne 0) {
        throw "HTTP/3 listener stability gate failed after $Count runs"
    }
}
finally {
    Pop-Location
}
