$ErrorActionPreference = 'Stop'
$WorkbenchRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$RuntimeDirectory = Join-Path $WorkbenchRoot 'data\runtime'
$ProcessRegistry = Join-Path $RuntimeDirectory 'processes.json'

function Find-WorkbenchTool([string]$Name, [string[]]$Candidates = @()) {
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    foreach ($candidate in $Candidates) { if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) { return [IO.Path]::GetFullPath($candidate) } }
    return $null
}

function Get-WorkbenchTools {
    $bundle = Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies'
    [PSCustomObject]@{
        Node = Find-WorkbenchTool 'node.exe' @((Join-Path $env:ProgramFiles 'nodejs\node.exe'), (Join-Path $bundle 'node\bin\node.exe'))
        Npm = Find-WorkbenchTool 'npm.cmd' @((Join-Path $env:ProgramFiles 'nodejs\npm.cmd'))
        Python = Find-WorkbenchTool 'python.exe' @((Join-Path $bundle 'python\python.exe'))
        Go = Find-WorkbenchTool 'go.exe' @('D:\go\bin\go.exe', (Join-Path $env:ProgramFiles 'Go\bin\go.exe'))
        FFmpeg = Find-WorkbenchTool 'ffmpeg.exe' @((Join-Path $WorkbenchRoot 'tools\ffmpeg\bin\ffmpeg.exe'))
        FFprobe = Find-WorkbenchTool 'ffprobe.exe' @((Join-Path $WorkbenchRoot 'tools\ffmpeg\bin\ffprobe.exe'))
    }
}

function ConvertTo-NativeArgument([string]$Value) {
    $quoted = [regex]::Replace($Value, '(\\*)"', '$1$1\"')
    $quoted = [regex]::Replace($quoted, '(\\+)$', '$1$1')
    return '"' + $quoted + '"'
}

function ConvertTo-FlatProcessRecords($Records) {
    foreach ($record in @($Records)) {
        if ($null -eq $record) { continue }
        $roleProperty = $record.PSObject.Properties['role']
        $pidProperty = $record.PSObject.Properties['pid']
        $processNumber = 0
        if ($roleProperty -and $pidProperty -and [int]::TryParse([string]$record.pid, [ref]$processNumber) -and $processNumber -gt 0) {
            # New objects remove PowerShell 5 array metadata (value/Count) that
            # ConvertFrom-Json can otherwise preserve across repeated saves.
            $startedAt = if ($record.started_at -is [DateTime]) { $record.started_at.ToUniversalTime().ToString('o') } elseif ($record.started_at -is [DateTimeOffset]) { $record.started_at.ToUniversalTime().ToString('o') } else { [string]$record.started_at }
            [PSCustomObject]@{ role = [string]$record.role; pid = $processNumber; executable = [string]$record.executable; started_at = $startedAt; argument_line = [string]$record.argument_line }
        } elseif ($record.PSObject.Properties['value']) {
            ConvertTo-FlatProcessRecords -Records $record.value
        } elseif ($record -is [System.Collections.IEnumerable] -and $record -isnot [string]) {
            foreach ($entry in $record) { ConvertTo-FlatProcessRecords -Records $entry }
        }
    }
}

function Get-ProcessRecords {
    if (Test-Path -LiteralPath $ProcessRegistry) {
        $decoded = ConvertFrom-Json -InputObject (Get-Content -LiteralPath $ProcessRegistry -Raw)
        ConvertTo-FlatProcessRecords -Records $decoded
    }
}

function Test-WorkbenchProcess($Record) {
    if (-not $Record -or -not $Record.pid -or -not $Record.executable -or -not $Record.started_at) { return $false }
    $process = Get-CimInstance Win32_Process -Filter "ProcessId = $($Record.pid)" -ErrorAction SilentlyContinue
    if (-not $process) { return $false }
    $started = $process.CreationDate.ToUniversalTime().ToString('o')
    # A PID can be reused. Both executable identity and process creation time must match.
    return ($process.ExecutablePath -ieq $Record.executable) -and ($started -eq $Record.started_at) -and ($process.CommandLine.IndexOf($WorkbenchRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0)
}

function Save-ProcessRecords($Records) {
    New-Item -ItemType Directory -Path $RuntimeDirectory -Force | Out-Null
    $flat = @(ConvertTo-FlatProcessRecords -Records $Records)
    $json = if ($flat.Count -eq 0) { '[]' } else { ConvertTo-Json -InputObject $flat -Depth 5 }
    Set-Content -LiteralPath $ProcessRegistry -Value $json -Encoding UTF8
}

function Start-WorkbenchProcess([string]$Role, [string]$Executable, [string[]]$Arguments, [string]$LogDirectory) {
    $records = @(Get-ProcessRecords)
    $argumentLine = ($Arguments | ForEach-Object { ConvertTo-NativeArgument $_ }) -join ' '
    $current = @($records | Where-Object { $_.role -eq $Role -and (Test-WorkbenchProcess $_) })
    if ($current.Count) {
        if ($current[0].argument_line -and $current[0].argument_line -ne $argumentLine) { throw "$Role is already running with different arguments. Run scripts\stop.ps1 before changing the data directory or port." }
        return $current[0]
    }
    New-Item -ItemType Directory -Path $LogDirectory -Force | Out-Null
    $process = Start-Process -FilePath $Executable -ArgumentList $argumentLine -WorkingDirectory $WorkbenchRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $LogDirectory "$Role.out.log") -RedirectStandardError (Join-Path $LogDirectory "$Role.err.log")
    $native = Get-CimInstance Win32_Process -Filter "ProcessId = $($process.Id)"
    if (-not $native) { throw "$Role exited immediately. Check $LogDirectory\$Role.err.log." }
    $record = [PSCustomObject]@{ role = $Role; pid = $process.Id; executable = $native.ExecutablePath; started_at = $native.CreationDate.ToUniversalTime().ToString('o'); argument_line = $argumentLine }
    Save-ProcessRecords @(@($records | Where-Object { $_.role -ne $Role -and (Test-WorkbenchProcess $_) }) + @($record))
    return $record
}

function Wait-WorkbenchURL([string]$URL, [int]$Seconds = 35) {
    $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
    while ([DateTime]::UtcNow -lt $deadline) {
        try { $response = Invoke-WebRequest -Uri $URL -TimeoutSec 2 -UseBasicParsing; if ($response.StatusCode -eq 200) { return $true } } catch { }
        Start-Sleep -Milliseconds 500
    }
    return $false
}
