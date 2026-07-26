param(
    [Parameter(Mandatory = $true)]
    [string]$EnginePath,

    [Parameter(Mandatory = $true)]
    [string]$DesktopSource
)

$ErrorActionPreference = "Stop"

$engine = (Resolve-Path $EnginePath).Path
if (!(Test-Path $engine)) { throw "missing bundled engine: $engine" }
$desktop = (Resolve-Path $DesktopSource).Path

# GitHub-hosted Windows workers are non-interactive, so WebView2 cannot reliably
# execute the visible Wails window and frontend mount lifecycle. Exercise the same
# native App bridge directly against a real Windows daemon instead: it launches the
# staged sibling-engine layout, waits for HTTPS readiness, uses the native credential
# reference for /v1/status, and issues /v1/stop.
$env:FSE_WINDOWS_SMOKE_ENGINE = $engine
Push-Location $desktop
try {
    go test . -run TestWindowsBundledDaemonLifecycleSmoke -count=1
} finally {
    Pop-Location
}
