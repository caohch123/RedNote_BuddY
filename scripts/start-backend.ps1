param([string]$Address = '127.0.0.1:8082', [string]$DataDir = 'data', [string]$AllowedOrigins = 'http://127.0.0.1:8080,http://localhost:8080,http://127.0.0.1:5173,http://localhost:5173')
. (Join-Path $PSScriptRoot 'common.ps1')
if ($Address -notmatch '^127\.0\.0\.1:[0-9]+$') { throw 'Backend must bind to 127.0.0.1.' }
$dataPath = if ([IO.Path]::IsPathRooted($DataDir)) { [IO.Path]::GetFullPath($DataDir) } else { [IO.Path]::GetFullPath((Join-Path $WorkbenchRoot $DataDir)) }
$executable = Join-Path $WorkbenchRoot 'bin\xhs-workbench.exe'
if (-not (Test-Path -LiteralPath $executable)) { throw 'Backend executable missing. Run scripts\build-backend.ps1.' }
$server = Start-WorkbenchProcess 'workbench' $executable @('--root', $WorkbenchRoot, '--addr', $Address, '--data-dir', $dataPath, '--allowed-origins', $AllowedOrigins) (Join-Path $dataPath 'logs')
if (-not (Wait-WorkbenchURL "http://$Address/api/v1/session" 35) -or -not (Test-WorkbenchProcess $server)) { throw 'Backend did not start. Check data\logs\workbench.err.log.' }
Write-Host "Backend API: http://$Address/api/v1 (PID $($server.pid))"
