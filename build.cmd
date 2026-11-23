@echo off
rem Builds the release executable.
rem
rem This wrapper exists because a .cmd file is not affected by the PowerShell
rem execution policy, which blocks unsigned .ps1 scripts on a default Windows
rem install. Same steps as build.ps1.
rem
rem -H=windowsgui links the binary as a GUI program, so Windows does not open an
rem empty console window next to the application window.
setlocal
set CGO_ENABLED=0

go vet ./...
if errorlevel 1 goto :failed

go test ./...
if errorlevel 1 goto :failed

go build -trimpath -ldflags "-s -w -H=windowsgui" -o MC-Tunnel.exe ./cmd/mctunnel
if errorlevel 1 goto :failed

echo built MC-Tunnel.exe ^(GUI subsystem, no console window^)
exit /b 0

:failed
echo build failed
exit /b 1
