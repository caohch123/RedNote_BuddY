. (Join-Path $PSScriptRoot 'common.ps1')
$tools = Get-WorkbenchTools
if (-not $tools.Npm) { throw 'Node.js >= 22 and npm are required.' }
Push-Location (Join-Path $WorkbenchRoot 'frontend')
try { & $tools.Npm run build; if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' } } finally { Pop-Location }
