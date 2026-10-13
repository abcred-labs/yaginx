param([string]$Version = "dev")
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$gateway = Join-Path $root 'gateway'
$out = Join-Path $root "artifacts/$Version"
New-Item -ItemType Directory -Force -Path $out | Out-Null
foreach ($target in @(@('linux','amd64'), @('linux','arm64'))) {
  $env:GOOS = $target[0]; $env:GOARCH = $target[1]; $env:CGO_ENABLED = '0'
  $name = "yaginx-$($target[0])-$($target[1])"
  Push-Location $gateway
  go build -trimpath -buildvcs=false -ldflags "-s -w -X yaginx-gateway/internal/app.Version=$Version" -o (Join-Path $out $name) ./cmd/yaginx
  Pop-Location
}
Get-ChildItem -File $out | Where-Object { $_.Name -notin @('SHA256SUMS', 'sbom.spdx.json', 'SIGNING.txt') } | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } | Set-Content (Join-Path $out 'SHA256SUMS')
Push-Location $gateway
try {
  $modules = @(go list -m -f '{{.Path}}|{{.Version}}' all)
} finally {
  Pop-Location
}
$packages = @()
for ($i = 0; $i -lt $modules.Count; $i++) {
  $parts = $modules[$i] -split '\|', 2
  $modulePath = $parts[0]
  $moduleVersion = if ($parts.Count -gt 1) { $parts[1] } else { '' }
  $packages += @{
    SPDXID = "SPDXRef-Package-$i"
    name = $modulePath
    versionInfo = if ($moduleVersion) { $moduleVersion } else { $Version }
    downloadLocation = if ($modulePath -eq 'yaginx-gateway') { "NOASSERTION" } else { "https://pkg.go.dev/$modulePath@$moduleVersion" }
    filesAnalyzed = $false
    licenseConcluded = 'NOASSERTION'
    licenseDeclared = 'NOASSERTION'
    supplier = 'NOASSERTION'
  }
}
@{spdxVersion='SPDX-2.3';dataLicense='CC0-1.0';SPDXID='SPDXRef-DOCUMENT';name='yaginx';documentNamespace="https://yaginx.invalid/sbom/$Version";creationInfo=@{created=(Get-Date).ToUniversalTime().ToString('o');creators=@('Tool: yaginx build-release.ps1')};packages=$packages} | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $out 'sbom.spdx.json')
Set-Content (Join-Path $out 'SIGNING.txt') 'UNSIGNED: no release private key was supplied; no development or production signing key was generated.'
