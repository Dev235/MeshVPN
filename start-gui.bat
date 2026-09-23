@echo off
cd /d "%~dp0"
if not exist "bin\meshvpn-gui.exe" (
    echo Compiling MeshVPN Desktop GUI...
    go build -o bin\meshvpn-gui.exe .\cmd\meshvpn-gui
)
start "" "%~dp0bin\meshvpn-gui.exe"
