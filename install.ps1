# AnubisWatch Installation Script for Windows
# Usage: powershell -Command "iwr -useb https://anubis.watch/install.ps1 | iex"

param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\AnubisWatch",
    [switch]$AddToPath
)

function Write-Banner {
    Write-Host @"
╔════════════════════════════════════════════════════════════════╗
║   ⚖️  AnubisWatch — The Judgment Never Sleeps                  ║
║   Windows Installation Script                                  ║
╚════════════════════════════════════════════════════════════════╝
"@ -ForegroundColor Cyan
}

function Write-Info { param($msg) Write-Host "ℹ  $msg" -ForegroundColor Blue }
function Write-Success { param($msg) Write-Host "✓  $msg" -ForegroundColor Green }
function Write-Error { param($msg) Write-Host "✗  $msg" -ForegroundColor Red }

function Test-Admin {
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    return $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

$Repo = "AnubisWatch/anubiswatch"

function Get-Platform {
    # Releases publish windows/amd64 only.
    if (-not [Environment]::Is64BitOperatingSystem) {
        throw "AnubisWatch publishes 64-bit Windows binaries only"
    }
    return "windows", "amd64"
}

function Resolve-Version {
    param([string]$Requested)
    if ($Requested -ne "latest") {
        return $Requested
    }
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing
    if (-not $release.tag_name) {
        throw "Could not resolve the latest release tag"
    }
    return $release.tag_name
}

# Test-Checksum checks the archive against the release's checksums.txt.
# Set ANUBIS_SKIP_CHECKSUM=1 to install without it.
function Test-Checksum {
    param([string]$Dir, [string]$File, [string]$Tag)
    if ($env:ANUBIS_SKIP_CHECKSUM -eq "1") {
        Write-Info "Skipping checksum verification (ANUBIS_SKIP_CHECKSUM=1)"
        return
    }
    $sumsUrl = "https://github.com/$Repo/releases/download/$Tag/checksums.txt"
    $sumsPath = Join-Path $Dir "checksums.txt"
    Invoke-WebRequest -Uri $sumsUrl -OutFile $sumsPath -UseBasicParsing
    $expected = $null
    foreach ($line in Get-Content $sumsPath) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1].TrimStart('*') -eq $File) {
            $expected = $parts[0].ToLowerInvariant()
        }
    }
    if (-not $expected) {
        throw "No checksum listed for $File"
    }
    $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $Dir $File)).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for ${File}: expected $expected, got $actual"
    }
    Write-Success "Checksum verified"
}

function Install-Anubis {
    $platform, $arch = Get-Platform
    $tag = Resolve-Version $Version
    $name = "anubis-$platform-$arch"
    $filename = "$name.zip"
    $url = "https://github.com/$Repo/releases/download/$tag/$filename"
    $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())

    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    try {
        Write-Info "Downloading AnubisWatch $tag..."
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tempDir $filename) -UseBasicParsing
        Test-Checksum -Dir $tempDir -File $filename -Tag $tag

        Write-Info "Extracting..."
        Expand-Archive -Path (Join-Path $tempDir $filename) -DestinationPath $tempDir -Force

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        Copy-Item (Join-Path $tempDir "$name.exe") (Join-Path $InstallDir "anubis.exe") -Force
    } finally {
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }

    Write-Success "Installed to $InstallDir"
}

function Add-ToPath {
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable("Path", "$currentPath;$InstallDir", "User")
        Write-Success "Added to PATH"
        Write-Info "Restart your terminal to use 'anubis' command"
    }
}

function Initialize-Config {
    $dataDir = if (Test-Admin) { 
        "$env:ProgramData\AnubisWatch" 
    } else { 
        "$env:LOCALAPPDATA\AnubisWatch" 
    }
    
    New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
    Write-Success "Data directory: $dataDir"
    
    # Create start.bat
    $startBat = @"
@echo off
cd /d "$dataDir"
"$InstallDir\anubis.exe" serve --config anubis.json
pause
"@
    $startBat | Out-File -FilePath "$InstallDir\start.bat" -Encoding ASCII
}

function Main {
    $ErrorActionPreference = "Stop"
    Write-Banner
    
    if (!(Test-Admin)) {
        Write-Info "Installing for current user only (run as admin for system-wide install)"
    }
    
    try {
        Install-Anubis
    } catch {
        Write-Error "Installation failed: $_"
        # return, not exit: under `iwr | iex` exit would close the caller's shell.
        return
    }
    Initialize-Config
    
    if ($AddToPath) {
        Add-ToPath
    }
    
    Write-Host ""
    Write-Success "Installation Complete!"
    Write-Host ""
    Write-Host "Next steps:"
    Write-Host "  1. cd $InstallDir"
    Write-Host "  2. .\anubis.exe init --interactive"
    Write-Host "  3. .\anubis.exe serve"
    Write-Host ""
    Write-Host "Or run: $InstallDir\start.bat"
    Write-Host ""
}

Main
