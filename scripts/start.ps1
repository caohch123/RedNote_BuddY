param([string]$Address = '127.0.0.1:8080', [string]$DataDir = 'data', [switch]$NoBrowser, [switch]$SkipEngine)
. (Join-Path $PSScriptRoot 'common.ps1')
if ($Address -notmatch '^127\.0\.0\.1:[0-9]+$') { throw 'This launcher only binds to 127.0.0.1.' }
$dataPath = if ([IO.Path]::IsPathRooted($DataDir)) { [IO.Path]::GetFullPath($DataDir) } else { [IO.Path]::GetFullPath((Join-Path $WorkbenchRoot $DataDir)) }
$logPath = Join-Path $dataPath 'logs'
$tools = Get-WorkbenchTools
# Runtime tools may live at custom paths saved in Settings. The backend reports
# and resolves those paths; launching the compiled workbench doesn't need Node.
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
$executable = Join-Path $WorkbenchRoot 'bin\xhs-workbench.exe'
if (-not (Test-Path -LiteralPath $executable)) { throw 'Backend executable missing. Run scripts\setup.ps1 first.' }
$server = Start-WorkbenchProcess 'workbench' $executable @('--root', $WorkbenchRoot, '--addr', $Address, '--data-dir', $dataPath) $logPath
$url = "http://$Address"
if (-not (Wait-WorkbenchURL "$url/api/v1/session" 35)) { throw "Workbench did not start. Check $logPath\workbench.err.log." }
if (-not (Test-WorkbenchProcess $server)) { throw "The recorded workbench exited; another process may occupy $Address. Check $logPath\workbench.err.log." }
Write-Host "Workbench: $url (PID $($server.pid))"
if (-not $NoBrowser) { Start-Process $url }
