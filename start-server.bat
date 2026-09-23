@echo off
title MeshVPN All-in-One Server
echo ========================================================
echo        Starting MeshVPN Native Server (All-in-One)      
echo ========================================================
echo Control Plane:  http://localhost:8080 (REST API)
echo STUN Reflector: UDP port 3478
echo Zero-Knowledge: UDP port 41641 (Relay)
echo.
cd /d "%~dp0"
if exist "bin\meshvpn-server.exe" (
    bin\meshvpn-server.exe
) else (
    echo Compiling meshvpn-server...
    go build -o bin\meshvpn-server.exe .\cmd\meshvpn-server
    bin\meshvpn-server.exe
)
pause
