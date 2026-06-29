param(
    [switch]$Frontend,
    [switch]$Integration,
    [switch]$RequireDocker
)

$ErrorActionPreference = "Stop"

go test -count=1 ./...

if ($Frontend) {
    Push-Location web
    try {
        npm run build
    } finally {
        Pop-Location
    }
}

if ($Integration) {
    if ($RequireDocker) {
        $env:JIANMEN_REQUIRE_DOCKER = "1"
    }
    go test -count=1 -tags=integration ./internal/integration
}
