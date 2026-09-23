# MeshVPN Automated Installer for Windows
# Usage:
#   irm https://raw.githubusercontent.com/Dev235/MeshVPN/master/install.ps1 | iex
#   powershell -ExecutionPolicy Bypass -File install.ps1

param(
    [string]$TargetDir = "$env:LOCALAPPDATA\MeshVPN",
    [string]$JoinNetwork = "",
    [string]$Password = ""
)

$ErrorActionPreference = "Stop"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "           MeshVPN Windows Automated Installer           " -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Create Target Directory
if (!(Test-Path $TargetDir)) {
    New-Item -ItemType Directory -Path $TargetDir -Force | Out-Null
}

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# 2. Copy or Compile Binaries
if ((Test-Path "$ScriptDir\bin\meshvpn.exe") -and (Test-Path "$ScriptDir\bin\meshvpn-gui.exe")) {
    Write-Host "Installing precompiled binaries from local repo..." -ForegroundColor Gray
    Copy-Item "$ScriptDir\bin\*.exe" -Destination $TargetDir -Force
} elseif (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "Compiling native binaries with Go..." -ForegroundColor Yellow
    Push-Location $ScriptDir
    go build -ldflags="-w -s" -o "$TargetDir\meshvpn.exe" ./cmd/meshvpn
    go build -ldflags="-w -s" -o "$TargetDir\meshvpnd.exe" ./cmd/meshvpnd
    go build -ldflags="-w -s" -o "$TargetDir\meshvpn-gui.exe" ./cmd/meshvpn-gui
    go build -ldflags="-w -s" -o "$TargetDir\meshvpn-server.exe" ./cmd/meshvpn-server
    Pop-Location
} else {
    Write-Host "Downloading latest MeshVPN release..." -ForegroundColor Yellow
    $zipPath = "$env:TEMP\meshvpn.zip"
    # Fallback to cloning or copying
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $tmpClone = "$env:TEMP\meshvpn-clone"
        if (Test-Path $tmpClone) { Remove-Item -Recurse -Force $tmpClone }
        git clone --depth 1 https://github.com/Dev235/MeshVPN.git $tmpClone
        if (Test-Path "$tmpClone\bin") {
            Copy-Item "$tmpClone\bin\*.exe" -Destination $TargetDir -Force
        }
        Remove-Item -Recurse -Force $tmpClone
    }
}

Write-Host "✓ Installed MeshVPN to $TargetDir" -ForegroundColor Green

# 3. Add to User PATH if not present
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$TargetDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$TargetDir", "User")
    $env:Path = "$env:Path;$TargetDir"
    Write-Host "✓ Added $TargetDir to User PATH" -ForegroundColor Green
}

# 4. Create Desktop Shortcut for Non-Technical Users
try {
    $WshShell = New-Object -ComObject WScript.Shell
    $DesktopPath = [Environment]::GetFolderPath("Desktop")
    $Shortcut = $WshShell.CreateShortcut("$DesktopPath\MeshVPN.lnk")
    $Shortcut.TargetPath = "$TargetDir\meshvpn-gui.exe"
    $Shortcut.WorkingDirectory = $TargetDir
    $Shortcut.Description = "MeshVPN Desktop Interface"
    if (Test-Path "$TargetDir\icon.ico") {
        $Shortcut.IconLocation = "$TargetDir\icon.ico"
    }
    $Shortcut.Save()
    Write-Host "✓ Created MeshVPN Desktop shortcut on your Desktop!" -ForegroundColor Green
} catch {
    Write-Host "Notice: Could not create desktop shortcut automatically." -ForegroundColor Gray
}

Write-Host ""
Write-Host "Installation Complete!" -ForegroundColor Green
Write-Host "• Desktop GUI: Double-click 'MeshVPN' on your Desktop" -ForegroundColor White
Write-Host "• CLI Usage:   meshvpn status, meshvpn join <network> [password]" -ForegroundColor White
Write-Host ""

# 5. Handle optional inline join
if ($JoinNetwork -ne "") {
    Write-Host "Joining network '$JoinNetwork'..." -ForegroundColor Cyan
    & "$TargetDir\meshvpn.exe" join $JoinNetwork $Password
}
