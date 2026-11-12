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
# native App bridge directly against a real Windows daemon, using the engine
# binary staged for the Windows desktop layout. The Mac build uses this same
# bridge. The smoke launches that engine, then calls the shared management and
# folder routes and the engine sync journal before /v1/stop.
$env:FSE_WINDOWS_SMOKE_ENGINE = $engine
Push-Location $desktop
try {
    go test . -run TestWindowsBundledDaemonLifecycleSmoke -count=1
} finally {
    Pop-Location
}
