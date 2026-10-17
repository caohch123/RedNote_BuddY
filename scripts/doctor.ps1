. (Join-Path $PSScriptRoot 'common.ps1')
$tools = Get-WorkbenchTools
Write-Host "Project: $WorkbenchRoot"
Write-Host 'Build / installation tools (system discovery):'
foreach ($name in @('Npm','Go','Python')) {
    if ($tools.$name) { Write-Host "[OK] $name : $($tools.$name)" } else { Write-Warning "[MISSING] $name" }
}
Write-Host 'Runtime tools (saved Settings, same checks as the workbench):'
try {
    $runtimeRecord = @(Get-ProcessRecords | Where-Object { $_.role -eq 'workbench' -and (Test-WorkbenchProcess $_) }) | Select-Object -First 1
    if (-not $runtimeRecord -or $runtimeRecord.argument_line -notmatch '(?:"--addr"|--addr)\s+"?(127\.0\.0\.1:\d+)') { throw 'Workbench address unavailable' }
    $runtimeAddress = $Matches[1]
    $runtimeHealth = Invoke-RestMethod -Uri "http://$runtimeAddress/api/v1/health" -TimeoutSec 10
    foreach ($name in @('node','ffmpeg','ffprobe')) {
        $check = $runtimeHealth.dependencies.$name
        if ($check.ok) { Write-Host "[OK] $name : $($check.path) $($check.version)" }
        else { Write-Warning "$name : $($check.message)" }
    }
} catch {
    Write-Host 'Workbench not available. Saved runtime paths can be checked after startup.'
    foreach ($name in @('Node','FFmpeg','FFprobe')) {
        if ($tools.$name) { Write-Host "[DISCOVERED] $name : $($tools.$name)" }
    }
}
foreach ($file in @('bin\xhs-workbench.exe','backend\go.mod','frontend\dist\index.html','frontend\server.mjs','engine\.venv\Scripts\python.exe','engine\upstream.lock.json','publisher\.venv\Scripts\python.exe','publisher\bridge.py','publisher\upstream.lock.json','publisher\vendor\Spider_XHS\node_modules\crypto-js\package.json')) {
    if (Test-Path -LiteralPath (Join-Path $WorkbenchRoot $file)) { Write-Host "[OK] $file" } else { Write-Warning "[MISSING] $file" }
}
foreach ($record in @(Get-ProcessRecords)) { Write-Host "$($record.role) PID $($record.pid): $(Test-WorkbenchProcess $record)" }
Write-Host 'API credentials are checked in the workbench Settings page, not by this script.'
Write-Host 'XHS QR login/publishing uses an isolated Python/Node adapter started by the workbench; no additional network listener.'
