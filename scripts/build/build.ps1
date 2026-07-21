# Jianmen build script (Windows PowerShell)
# Builds the embedded frontend, Windows binary, and Linux Lite/RDP variants.

$ErrorActionPreference = "Stop"

function Resolve-WslDockerDistribution {
    $wsl = Get-Command "wsl.exe" -ErrorAction SilentlyContinue
    if (-not $wsl) {
        throw "WSL Docker is required to build the Linux RDP package"
    }
    $distributions = & $wsl.Source --list --quiet 2>$null
    foreach ($distribution in $distributions) {
        $name = ([string]$distribution).Replace([string][char]0, "").Trim()
        if ([string]::IsNullOrWhiteSpace($name)) {
            continue
        }
        & $wsl.Source -d $name -e docker info --format "{{.ServerVersion}}" *> $null
        if ($LASTEXITCODE -eq 0) {
            return $name
        }
    }
    throw "No working Docker Engine was found in WSL; it is required for the Linux RDP package"
}

function Convert-ToWslPath($Distribution, $Path) {
    $converted = (& wsl.exe -d $Distribution -e wslpath -u $Path).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($converted)) {
        throw "Unable to convert path for WSL: $Path"
    }
    return $converted
}

function Get-FileSizeMB($Path) {
    return [math]::Round((Get-Item $Path).Length / 1MB, 1)
}

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$frontendDist = Join-Path $root "web\dist"
$embedDir = Join-Path $root "internal\frontend\dist"
$outputDir = Join-Path $root "dist"

Write-Host "============================================"
Write-Host "  Jianmen Build (Linux Lite + RDP)"
Write-Host "============================================"
Write-Host ""

Write-Host "[1/6] Building frontend..." -ForegroundColor Cyan
Push-Location (Join-Path $root "web")
try {
    $frontendTools = @("vitest.cmd", "vue-tsc.cmd", "vite.cmd")
    $missingFrontendTool = $frontendTools | Where-Object {
        -not (Test-Path (Join-Path "node_modules\.bin" $_))
    } | Select-Object -First 1
    if ($missingFrontendTool) {
        Write-Host "  Installing locked frontend dependencies..." -ForegroundColor Cyan
        npm ci
        if ($LASTEXITCODE -ne 0) { throw "Frontend dependency installation failed" }
    }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
} finally {
    Pop-Location
}
Write-Host "  OK frontend built" -ForegroundColor Green

Write-Host "[2/6] Copying frontend to embed directory..." -ForegroundColor Cyan
if (Test-Path $embedDir) {
    Remove-Item -Recurse -Force $embedDir
}
Copy-Item -Recurse $frontendDist $embedDir
$fileCount = (Get-ChildItem -Recurse $embedDir | Where-Object { -not $_.PSIsContainer }).Count
Write-Host "  OK $fileCount files copied" -ForegroundColor Green

New-Item -ItemType Directory -Force $outputDir | Out-Null
Push-Location $root
try {
    Write-Host "[3/6] Building Windows amd64..." -ForegroundColor Cyan
    $windowsOutput = Join-Path $outputDir "jianmen-windows-amd64.exe"
    go build -o $windowsOutput ./cmd/jianmen/
    if ($LASTEXITCODE -ne 0) { throw "Windows build failed" }
    Write-Host "  OK jianmen-windows-amd64.exe ($(Get-FileSizeMB $windowsOutput)MB)" -ForegroundColor Green

    $previousGOOS = $env:GOOS
    $previousGOARCH = $env:GOARCH
    $previousCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = "linux"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"

        Write-Host "[4/6] Building Linux amd64 Lite..." -ForegroundColor Cyan
        $liteOutput = Join-Path $outputDir "jianmen-linux-amd64-lite"
        go build -o $liteOutput ./cmd/jianmen/
        if ($LASTEXITCODE -ne 0) { throw "Linux Lite build failed" }
        Write-Host "  OK jianmen-linux-amd64-lite ($(Get-FileSizeMB $liteOutput)MB)" -ForegroundColor Green

        Write-Host "[5/6] Preparing embedded guacd runtime..." -ForegroundColor Cyan
        $distribution = Resolve-WslDockerDistribution
        $prepareScript = Convert-ToWslPath $distribution (Join-Path $root "scripts\build\prepare-guacd-runtime.sh")
        $runtimeAsset = Convert-ToWslPath $distribution (Join-Path $root "internal\guacdruntime\assets\guacd-linux-amd64.tar.gz")
        & wsl.exe -d $distribution -e bash $prepareScript amd64 $runtimeAsset
        if ($LASTEXITCODE -ne 0) { throw "Embedded guacd runtime preparation failed" }

        Write-Host "[6/6] Building Linux amd64 RDP..." -ForegroundColor Cyan
        $rdpOutput = Join-Path $outputDir "jianmen-linux-amd64-rdp"
        go build -tags embedded_guacd -o $rdpOutput ./cmd/jianmen/
        if ($LASTEXITCODE -ne 0) { throw "Linux RDP build failed" }
        Write-Host "  OK jianmen-linux-amd64-rdp ($(Get-FileSizeMB $rdpOutput)MB)" -ForegroundColor Green
    } finally {
        $env:GOOS = $previousGOOS
        $env:GOARCH = $previousGOARCH
        $env:CGO_ENABLED = $previousCGO
    }
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "============================================"
Write-Host "  Build complete"
Write-Host "============================================"
Get-ChildItem $outputDir -File | ForEach-Object {
    Write-Host "  $($_.Name) ($(Get-FileSizeMB $_.FullName)MB)"
}
Write-Host ""
Write-Host "Linux default: dist/jianmen-linux-amd64-rdp" -ForegroundColor Yellow
Write-Host "Linux without Web RDP: dist/jianmen-linux-amd64-lite" -ForegroundColor Yellow
