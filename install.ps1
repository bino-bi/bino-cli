<#
.SYNOPSIS
    Installs the latest bino CLI release for Windows.

.DESCRIPTION
    Downloads and installs the bino CLI from GitHub releases.
    Supports checksum verification and custom install directories.

.PARAMETER Repo
    Override release repository (owner/repo). Default: bino-bi/bino-cli-releases

.PARAMETER Tag
    Tag to install. Default: latest

.PARAMETER InstallDir
    Destination directory for the binary. Default: $env:LOCALAPPDATA\bino

.PARAMETER Yes
    Non-interactive mode (accept all prompts).

.PARAMETER DryRun
    Show actions but do not perform installation.

.EXAMPLE
    # One-liner installation
    irm https://github.com/bino-bi/bino-cli-releases/releases/latest/download/install.ps1 | iex

.EXAMPLE
    # With parameters
    .\install.ps1 -Tag v1.0.0 -InstallDir "C:\Tools\bino"

.NOTES
    Requires Windows 10 or later with PowerShell 5.1+
#>

[CmdletBinding()]
param(
    [string]$Repo = "bino-bi/bino-cli-releases",
    [string]$Tag = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\bino",
    [switch]$Yes,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"  # Speeds up Invoke-WebRequest

$AssetName = "bino-cli_Windows_x86_64.zip"
$ChecksumName = "checksums.txt"
$BinaryName = "bino.exe"

function Write-Step {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Green
}

function Write-Warning {
    param([string]$Message)
    Write-Host "Warning: $Message" -ForegroundColor Yellow
}

function Confirm-Action {
    param([string]$Prompt)
    if ($Yes) { return $true }
    $response = Read-Host "$Prompt [y/N]"
    return $response -match '^[Yy]'
}

function Get-ReleaseAssetUrl {
    param([string]$Repo, [string]$Tag, [string]$AssetName)

    if ($Tag -eq "latest") {
        $apiUrl = "https://api.github.com/repos/$Repo/releases/latest"
    } else {
        $apiUrl = "https://api.github.com/repos/$Repo/releases/tags/$Tag"
    }

    try {
        $release = Invoke-RestMethod -Uri $apiUrl -Headers @{ "User-Agent" = "bino-installer" }
        $asset = $release.assets | Where-Object { $_.name -eq $AssetName }
        if ($asset) {
            return $asset.browser_download_url
        }
    } catch {
        Write-Warning "Failed to query GitHub API: $_"
    }
    return $null
}

function Get-ChecksumForAsset {
    param([string]$ChecksumContent, [string]$AssetName)

    foreach ($line in $ChecksumContent -split "`n") {
        $line = $line.Trim()
        if ($line -match "^([a-fA-F0-9]{64})\s+.*$AssetName") {
            return $matches[1].ToLower()
        }
        if ($line -match "$AssetName\s+([a-fA-F0-9]{64})") {
            return $matches[1].ToLower()
        }
    }
    return $null
}

function Get-FileHashSHA256 {
    param([string]$FilePath)
    return (Get-FileHash -Path $FilePath -Algorithm SHA256).Hash.ToLower()
}

function Test-InPath {
    param([string]$Directory)

    $normalizedDir = [System.IO.Path]::GetFullPath($Directory).TrimEnd('\')
    $currentPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::User)
    $systemPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::Machine)
    $fullPath = "$currentPath;$systemPath"

    foreach ($entry in $fullPath -split ';') {
        if ([string]::IsNullOrWhiteSpace($entry)) { continue }
        try {
            $normalizedEntry = [System.IO.Path]::GetFullPath($entry).TrimEnd('\')
            if ($normalizedDir -eq $normalizedEntry) {
                return $true
            }
        } catch {
            # Skip invalid path entries
        }
    }
    return $false
}

function Get-PathDirectoriesInPath {
    # Return common directories that are already in PATH
    $commonDirs = @(
        "$env:LOCALAPPDATA\Microsoft\WindowsApps",
        "$env:USERPROFILE\bin",
        "$env:LOCALAPPDATA\Programs",
        "C:\Tools",
        "C:\bin"
    )

    $inPath = @()
    foreach ($dir in $commonDirs) {
        if (Test-InPath -Directory $dir) {
            $inPath += $dir
        }
    }
    return $inPath
}

function Add-ToPath {
    param([string]$Directory)

    $currentPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::User)
    if ($currentPath -notlike "*$Directory*") {
        $newPath = "$currentPath;$Directory"
        [Environment]::SetEnvironmentVariable("Path", $newPath, [EnvironmentVariableTarget]::User)
        $env:Path = "$env:Path;$Directory"
        return $true
    }
    return $false
}

# Main installation logic
Write-Step "Bino CLI Installer for Windows"
Write-Host "Repo: $Repo"
Write-Host "Tag: $Tag"
Write-Host "Install directory: $InstallDir"
Write-Host "Asset: $AssetName"
Write-Host ""

# Check if install directory is in PATH
if (-not (Test-InPath -Directory $InstallDir)) {
    Write-Host "============================================================" -ForegroundColor Red
    Write-Host "WARNING: Install directory is not in your PATH" -ForegroundColor Red
    Write-Host "============================================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "The install directory '$InstallDir' is not in your PATH."
    Write-Host "After installation, you won't be able to run 'bino' directly."
    Write-Host ""
    Write-Host "You have two options:" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  1. Let the installer add the directory to your PATH automatically"
    Write-Host "     (will be added to user PATH, requires terminal restart)"
    Write-Host ""
    Write-Host "  2. Choose a different install directory that's already in your PATH:" -ForegroundColor Yellow
    Write-Host ""

    $dirsInPath = Get-PathDirectoriesInPath
    if ($dirsInPath.Count -gt 0) {
        foreach ($dir in $dirsInPath) {
            Write-Host "     .\install.ps1 -InstallDir `"$dir`""
        }
    } else {
        Write-Host "     (No common directories found in PATH)"
    }

    Write-Host ""
    Write-Host "============================================================" -ForegroundColor Red
    Write-Host ""

    if (-not $Yes) {
        $response = Read-Host "Continue and add '$InstallDir' to PATH? [y/N]"
        if ($response -notmatch '^[Yy]') {
            Write-Host "Installation aborted."
            exit 0
        }
    }
    $addToPathAfterInstall = $true
} else {
    $addToPathAfterInstall = $false
}

# Get asset URL
Write-Step "Fetching release information..."
$assetUrl = Get-ReleaseAssetUrl -Repo $Repo -Tag $Tag -AssetName $AssetName
if (-not $assetUrl) {
    Write-Error "Could not find release asset $AssetName in $Repo (tag: $Tag)."
}

$checksumUrl = Get-ReleaseAssetUrl -Repo $Repo -Tag $Tag -AssetName $ChecksumName

# Create temp directory
$tempDir = Join-Path $env:TEMP "bino-install-$(Get-Random)"
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

try {
    $archivePath = Join-Path $tempDir $AssetName

    # Download archive
    if ($DryRun) {
        Write-Host "[dry-run] Would download: $assetUrl -> $archivePath"
    } else {
        Write-Step "Downloading $AssetName..."
        Invoke-WebRequest -Uri $assetUrl -OutFile $archivePath -UseBasicParsing
    }

    # Download and verify checksum
    if ($checksumUrl -and -not $DryRun) {
        Write-Step "Downloading checksums..."
        $checksumPath = Join-Path $tempDir $ChecksumName
        Invoke-WebRequest -Uri $checksumUrl -OutFile $checksumPath -UseBasicParsing

        $checksumContent = Get-Content -Path $checksumPath -Raw
        $expectedHash = Get-ChecksumForAsset -ChecksumContent $checksumContent -AssetName $AssetName

        if ($expectedHash) {
            Write-Step "Verifying checksum..."
            $actualHash = Get-FileHashSHA256 -FilePath $archivePath

            if ($expectedHash -ne $actualHash) {
                Write-Error "Checksum mismatch for $AssetName`nExpected: $expectedHash`nActual:   $actualHash"
            }
            Write-Success "Checksum OK."
        } else {
            Write-Warning "Checksum for $AssetName not found in $ChecksumName; skipping verification."
        }
    } elseif (-not $checksumUrl) {
        Write-Warning "$ChecksumName not found in release; skipping checksum verification."
    }

    # Extract archive
    $extractDir = Join-Path $tempDir "extracted"
    if ($DryRun) {
        Write-Host "[dry-run] Would extract: $archivePath -> $extractDir"
    } else {
        Write-Step "Extracting archive..."
        Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
    }

    # Find binary
    $binPath = Get-ChildItem -Path $extractDir -Filter $BinaryName -Recurse | Select-Object -First 1
    if (-not $binPath -and -not $DryRun) {
        Write-Error "Could not find $BinaryName inside the archive."
    }

    # Install
    if ($DryRun) {
        Write-Host "[dry-run] Would create directory: $InstallDir"
        Write-Host "[dry-run] Would copy binary to: $InstallDir\$BinaryName"
        if ($addToPathAfterInstall) {
            Write-Host "[dry-run] Would add to PATH: $InstallDir"
        }
    } else {
        Write-Step "Installing to $InstallDir..."

        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }

        $destPath = Join-Path $InstallDir $BinaryName
        Copy-Item -Path $binPath.FullName -Destination $destPath -Force

        # Copy DuckDB DLL if present (required for Windows)
        $dllPath = Get-ChildItem -Path $extractDir -Filter "duckdb.dll" -Recurse | Select-Object -First 1
        if ($dllPath) {
            Copy-Item -Path $dllPath.FullName -Destination (Join-Path $InstallDir "duckdb.dll") -Force
        }

        # Add to PATH if needed and user confirmed
        if ($addToPathAfterInstall) {
            if (Add-ToPath -Directory $InstallDir) {
                Write-Success "Added $InstallDir to user PATH."
                Write-Host "Note: You may need to restart your terminal for PATH changes to take effect."
            }
        }

        Write-Success "Installation complete."
        Write-Host "Installed: $destPath"
        Write-Host ""

        # Run post-install setup
        Write-Step "Running post-install setup..."
        try {
            & $destPath setup
        } catch {
            Write-Warning "Post-install setup failed. Please run 'bino setup' manually."
        }

        Write-Host ""
        Write-Success "Run: bino version"
    }
} finally {
    # Cleanup temp directory
    if (Test-Path $tempDir) {
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
