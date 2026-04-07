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

function Get-Platform {
    $arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
    return "windows", $arch
}

function Install-Anubis {
    $platform, $arch = Get-Platform
    $filename = "anubis_${platform}_${arch}.zip"
    $url = "https://github.com/AnubisWatch/anubiswatch/releases/download/$Version/$filename"
    $tempDir = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()
    
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    
    Write-Info "Downloading AnubisWatch..."
    try {
        Invoke-WebRequest -Uri $url -OutFile "$tempDir\$filename" -UseBasicParsing
    } catch {
        Write-Error "Download failed. Using local build if available."
        if (Test-Path ".\anubis.exe") {
            Copy-Item ".\anubis.exe" "$InstallDir\anubis.exe"
        }
        return
    }
    
    Write-Info "Extracting..."
    Expand-Archive -Path "$tempDir\$filename" -DestinationPath $tempDir -Force
    
    # Create install directory
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    
    # Copy binary
    Copy-Item "$tempDir\anubis.exe" "$InstallDir\anubis.exe" -Force
    
    # Cleanup
    Remove-Item -Path $tempDir -Recurse -Force
    
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
    Write-Banner
    
    if (!(Test-Admin)) {
        Write-Info "Installing for current user only (run as admin for system-wide install)"
    }
    
    Install-Anubis
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
