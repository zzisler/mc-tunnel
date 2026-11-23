# Builds the release executable.
#
# -H=windowsgui links the binary as a GUI program, so Windows does not open an
# empty console window next to the application window. Without it Go defaults to
# the console subsystem and a stray black window follows the app everywhere.
#
# Everything the program has to say goes to the log panel, so losing the console
# costs nothing.
$ErrorActionPreference = 'Stop'

$env:CGO_ENABLED = '0'
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go test ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -trimpath -ldflags '-s -w -H=windowsgui' -o MC-Tunnel.exe ./cmd/mctunnel
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host 'built MC-Tunnel.exe (GUI subsystem, no console window)'
