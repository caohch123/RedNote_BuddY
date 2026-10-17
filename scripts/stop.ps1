param([ValidateSet('all','frontend','backend','collector')][string]$Service = 'all')
. (Join-Path $PSScriptRoot 'common.ps1')
$records = @(Get-ProcessRecords)
$keep = @()
$targetRole = if ($Service -eq 'backend') { 'workbench' } else { $Service }
foreach ($record in $records) {
    if ($Service -ne 'all' -and $record.role -ne $targetRole) { $keep += $record; continue }
    if (Test-WorkbenchProcess $record) {
        # Windows venv launchers and video render workers spawn descendants. The
        # validated root PID owns its whole tree, including interpreter proxies.
        & taskkill.exe /PID ([string]$record.pid) /T /F
        if ($LASTEXITCODE -eq 0) { Write-Host "Stopped $($record.role) tree (PID $($record.pid))." }
        else { Write-Warning "Could not stop $($record.role) (PID $($record.pid))."; $keep += $record }
    } else { Write-Host "Skipped inactive or unrelated PID $($record.pid)." }
}
Save-ProcessRecords $keep
