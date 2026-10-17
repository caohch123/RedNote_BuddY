. (Join-Path $PSScriptRoot 'common.ps1')
$records = @(Get-ProcessRecords)
foreach ($record in $records) {
    if (Test-WorkbenchProcess $record) {
        # Windows venv launchers and video render workers spawn descendants. The
        # validated root PID owns its whole tree, including interpreter proxies.
        & taskkill.exe /PID ([string]$record.pid) /T /F
        if ($LASTEXITCODE -eq 0) { Write-Host "Stopped $($record.role) tree (PID $($record.pid))." }
        else { Write-Warning "Could not stop $($record.role) (PID $($record.pid))." }
    } else { Write-Host "Skipped inactive or unrelated PID $($record.pid)." }
}
Save-ProcessRecords @()
