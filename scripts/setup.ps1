param([switch]$SkipEngine, [switch]$SkipPublisher, [switch]$SkipBuild, [string]$PythonPath)
. (Join-Path $PSScriptRoot 'common.ps1')
$tools = Get-WorkbenchTools
if ($PythonPath) { $tools.Python = (Resolve-Path -LiteralPath $PythonPath).Path }
foreach ($name in @('Node','Npm','Go')) { if (-not $tools.$name) { throw "Missing $name. Install Node.js >= 22 and Go >= 1.26, then run setup again." } }
$nodeMajor = [int]((& $tools.Node --version).TrimStart('v').Split('.')[0])
if ($nodeMajor -lt 22) { throw 'Node.js >= 22 is required.' }
Push-Location $WorkbenchRoot
try {
    if (-not $SkipEngine) {
        if (-not $tools.Python) { throw 'Python 3.12 is required; use -PythonPath to choose an interpreter.' }
        & $tools.Python (Join-Path $WorkbenchRoot 'engine\prepare.py')
        if ($LASTEXITCODE -ne 0) { throw 'Collector preparation failed.' }
    }
    if (-not $SkipPublisher) {
        if (-not $tools.Python) { throw 'Python 3.12 or newer is required for QR login/publishing; use -PythonPath to choose an interpreter.' }
        & $tools.Python (Join-Path $WorkbenchRoot 'publisher\prepare.py')
        if ($LASTEXITCODE -ne 0) { throw 'Publisher preparation failed.' }
    }
    $dependencyDirectories = @('web')
    foreach ($directory in $dependencyDirectories) {
        Push-Location (Join-Path $WorkbenchRoot $directory)
        try {
            if (Test-Path -LiteralPath 'package-lock.json') { & $tools.Npm ci --no-audit --no-fund } else { & $tools.Npm install --no-audit --no-fund }
            if ($LASTEXITCODE -ne 0) { throw "Dependency installation failed in $directory." }
        } finally { Pop-Location }
    }
    if (-not $SkipBuild) {
        Push-Location (Join-Path $WorkbenchRoot 'web')
        try { & $tools.Npm run build; if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' } } finally { Pop-Location }
        New-Item -ItemType Directory -Path (Join-Path $WorkbenchRoot 'bin') -Force | Out-Null
        $previousCGO = $env:CGO_ENABLED; $previousArch = $env:GOARCH
        try {
            $env:CGO_ENABLED = '0'; $env:GOARCH = 'amd64'
            & $tools.Go build -trimpath -o (Join-Path $WorkbenchRoot 'bin\xhs-workbench.exe') ./cmd/workbench
            if ($LASTEXITCODE -ne 0) { throw 'Go backend build failed.' }
        } finally { $env:CGO_ENABLED = $previousCGO; $env:GOARCH = $previousArch }
    }
} finally { Pop-Location }
& (Join-Path $PSScriptRoot 'doctor.ps1')
Write-Host 'Setup finished. Run scripts\start.ps1.'
