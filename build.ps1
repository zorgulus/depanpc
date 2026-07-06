<#
Compile l'agent DEPAN PC en un binaire unique, prêt à être copié sur une clé
USB et lancé par double-clic sur le PC à dépanner, sans aucune installation.
#>

$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
$agentDir = Join-Path $root "agent"
$distDir = Join-Path $root "dist"

New-Item -ItemType Directory -Force -Path $distDir | Out-Null

$version = Get-Date -Format "yyyy.MM.dd-HHmm"
$output = Join-Path $distDir "depanpc-agent.exe"

Write-Host "Build depanpc-agent.exe (version $version)..."

Push-Location $agentDir
try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    go build -ldflags "-s -w -X main.buildVersion=$version" -o $output .
}
finally {
    Pop-Location
}

Copy-Item (Join-Path $root "docs\MODE_EMPLOI.txt") $distDir -Force
Set-Content -Path (Join-Path $distDir "VERSION.txt") -Value $version -NoNewline

Write-Host "OK -> $output"
Get-Item $output | Select-Object Name, Length, LastWriteTime
