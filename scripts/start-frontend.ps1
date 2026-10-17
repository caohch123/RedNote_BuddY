param([string]$Address = '127.0.0.1:8080', [string]$ApiAddress = '127.0.0.1:8082', [string]$DataDir = 'data', [switch]$NoBrowser)
. (Join-Path $PSScriptRoot 'common.ps1')
if ($Address -notmatch '^127\.0\.0\.1:([0-9]+)$') { throw 'Frontend must bind to 127.0.0.1.' }
$frontendPort = $Matches[1]
if ($ApiAddress -notmatch '^127\.0\.0\.1:[0-9]+$' -or $ApiAddress -eq $Address) { throw 'Use a separate loopback API address.' }
$tools = Get-WorkbenchTools
if (-not $tools.Node) { throw 'Node.js >= 22 is required to serve the frontend.' }
$nodeMajor = [int]((& $tools.Node --version).TrimStart('v').Split('.')[0])
if ($nodeMajor -lt 22) { throw 'Node.js >= 22 is required.' }
if (-not (Test-Path -LiteralPath (Join-Path $WorkbenchRoot 'frontend\dist\index.html'))) { throw 'Frontend build missing. Run scripts\build-frontend.ps1.' }
$dataPath = if ([IO.Path]::IsPathRooted($DataDir)) { [IO.Path]::GetFullPath($DataDir) } else { [IO.Path]::GetFullPath((Join-Path $WorkbenchRoot $DataDir)) }
$server = Start-WorkbenchProcess 'frontend' $tools.Node @((Join-Path $WorkbenchRoot 'frontend\server.mjs'), '--host', '127.0.0.1', '--port', $frontendPort, '--api-target', "http://$ApiAddress") (Join-Path $dataPath 'logs')
if (-not (Wait-WorkbenchURL "http://$Address/healthz" 15) -or -not (Test-WorkbenchProcess $server)) { throw 'Frontend did not start. Check data\logs\frontend.err.log.' }
Write-Host "Frontend: http://$Address (PID $($server.pid))"
if (-not $NoBrowser) { Start-Process "http://$Address" }
