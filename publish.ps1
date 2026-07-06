<#
Compile l'agent puis publie une nouvelle release GitHub avec l'exe en
pièce jointe. Le lien "latest" ne change jamais d'une publication à
l'autre :
https://github.com/<owner>/<repo>/releases/latest/download/depanpc-agent.exe

Prérequis : `gh auth login` déjà fait, dépôt GitHub déjà créé (remote
"origin" configuré).
#>

$ErrorActionPreference = "Stop"

$root = $PSScriptRoot

& (Join-Path $root "build.ps1")
if ($LASTEXITCODE -ne 0) {
    throw "build.ps1 a echoue (code $LASTEXITCODE), publication annulee"
}

$version = Get-Content (Join-Path $root "dist\VERSION.txt")
$exe = Join-Path $root "dist\depanpc-agent.exe"
$tag = "v$version"

Write-Host "Publication de la release $tag sur GitHub..."
gh release create $tag $exe --title "depanpc-agent $version" --notes "Build automatique ($version)."
if ($LASTEXITCODE -ne 0) {
    throw "gh release create a echoue (code $LASTEXITCODE)"
}

$repo = gh repo view --json nameWithOwner -q .nameWithOwner
if ($LASTEXITCODE -ne 0) {
    throw "gh repo view a echoue (code $LASTEXITCODE)"
}
$latestUrl = "https://github.com/$repo/releases/latest/download/depanpc-agent.exe"

Write-Host "OK. Lien stable (toujours la derniere version publiee) :"
Write-Host $latestUrl
