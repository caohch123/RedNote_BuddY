param([string]$Address = '127.0.0.1:8080', [string]$ApiAddress = '127.0.0.1:8082', [string]$DataDir = 'data', [switch]$NoBrowser, [switch]$SkipEngine)
. (Join-Path $PSScriptRoot 'common.ps1')
if ($Address -notmatch '^127\.0\.0\.1:[0-9]+$') { throw 'This launcher only binds to 127.0.0.1.' }
if ($ApiAddress -notmatch '^127\.0\.0\.1:[0-9]+$' -or $ApiAddress -eq $Address) { throw 'Frontend and backend need separate loopback addresses.' }
$dataPath = if ([IO.Path]::IsPathRooted($DataDir)) { [IO.Path]::GetFullPath($DataDir) } else { [IO.Path]::GetFullPath((Join-Path $WorkbenchRoot $DataDir)) }
$logPath = Join-Path $dataPath 'logs'
$tools = Get-WorkbenchTools
$env:DO_NOT_TRACK = '1'
$enginePython = Join-Path $WorkbenchRoot 'engine\.venv\Scripts\python.exe'
if ($SkipEngine -and -not (Wait-WorkbenchURL 'http://127.0.0.1:5556/health' 2)) {
    Write-Warning 'The existing collector is unavailable. Starting the project collector instead of leaving collection disconnected.'
    $SkipEngine = $false
}
if (-not $SkipEngine) {
    if (Test-Path -LiteralPath $enginePython) {
        $engine = Start-WorkbenchProcess 'collector' $enginePython @((Join-Path $WorkbenchRoot 'engine\server.py'), '--data-dir', (Join-Path $dataPath 'engine'), '--port', '5556') $logPath
        if (-not (Wait-WorkbenchURL 'http://127.0.0.1:5556/health' 45)) { Write-Warning "Collector not ready. Check $logPath\collector.err.log. The workbench can still open." }
    } else { Write-Warning 'Collector not installed. Run scripts\setup.ps1. The workbench can still open for manual editing.' }
}
$frontendPort = ($Address -split ':')[-1]
$origins = "http://$Address,http://localhost:$frontendPort,http://127.0.0.1:5173,http://localhost:5173"
& (Join-Path $PSScriptRoot 'start-backend.ps1') -Address $ApiAddress -DataDir $dataPath -AllowedOrigins $origins
& (Join-Path $PSScriptRoot 'start-frontend.ps1') -Address $Address -ApiAddress $ApiAddress -DataDir $dataPath -NoBrowser:$NoBrowser
