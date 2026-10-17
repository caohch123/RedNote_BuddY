. (Join-Path $PSScriptRoot 'common.ps1')
$tools = Get-WorkbenchTools
if (-not $tools.Go) { throw 'Go >= 1.26 is required.' }
New-Item -ItemType Directory -Path (Join-Path $WorkbenchRoot 'bin') -Force | Out-Null
$previousCGO = $env:CGO_ENABLED; $previousArch = $env:GOARCH
Push-Location (Join-Path $WorkbenchRoot 'backend')
try {
    $env:CGO_ENABLED = '0'; $env:GOARCH = 'amd64'
    & $tools.Go build -trimpath -o (Join-Path $WorkbenchRoot 'bin\xhs-workbench.exe') ./cmd/workbench
    if ($LASTEXITCODE -ne 0) { throw 'Backend build failed. Stop the running backend before replacing its executable.' }
} finally { Pop-Location; $env:CGO_ENABLED = $previousCGO; $env:GOARCH = $previousArch }
