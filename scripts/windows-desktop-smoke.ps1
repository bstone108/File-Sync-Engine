param(
    [Parameter(Mandatory = $true)]
    [string]$BundleRoot
)

$ErrorActionPreference = "Stop"

$bundle = (Resolve-Path $BundleRoot).Path
$app = Join-Path $bundle "app\fse-desktop.exe"
$engine = Join-Path $bundle "engine\windows\amd64\fse.exe"
if (!(Test-Path $app)) { throw "missing desktop application: $app" }
if (!(Test-Path $engine)) { throw "missing bundled engine: $engine" }

$stateRoots = @($env:APPDATA, $env:LOCALAPPDATA) |
    Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
    ForEach-Object { Join-Path $_ "fse-desktop" } |
    Select-Object -Unique
foreach ($stateRoot in $stateRoots) {
    if (Test-Path $stateRoot) { Remove-Item -Recurse -Force $stateRoot }
}

$desktop = $null
try {
    $desktop = Start-Process -FilePath $app -WorkingDirectory (Split-Path $app) -PassThru
    $sessionPath = $null
    $deadline = (Get-Date).AddSeconds(45)
    while ((Get-Date) -lt $deadline -and $null -eq $sessionPath) {
        foreach ($stateRoot in $stateRoots) {
            $candidate = Join-Path $stateRoot "gui-owned-daemon-session.json"
            if (Test-Path $candidate) {
                $sessionPath = $candidate
                break
            }
        }
        if ($null -eq $sessionPath) { Start-Sleep -Milliseconds 500 }
    }
    if ($null -eq $sessionPath) {
        throw "desktop GUI did not create a GUI-owned daemon session; native bridge or automatic bundled-daemon launch did not complete"
    }

    $session = Get-Content -Raw $sessionPath | ConvertFrom-Json
    if ([string]::IsNullOrWhiteSpace($session.encryptedApiBaseURL) -or [string]::IsNullOrWhiteSpace($session.statePath)) {
        throw "desktop GUI session did not contain encrypted API connection state"
    }
    $apiKeyPath = Join-Path $session.statePath "api-key"
    if (!(Test-Path $apiKeyPath)) { throw "desktop GUI session did not retain native API credential state" }
    $headers = @{ "X-FSE-API-Key" = (Get-Content -Raw $apiKeyPath).Trim() }

    $status = Invoke-RestMethod -Uri ($session.encryptedApiBaseURL + "/v1/status") -Headers $headers -SkipCertificateCheck -TimeoutSec 15
    if ([string]::IsNullOrWhiteSpace($status.nodeName)) { throw "bundled daemon status did not include nodeName" }

    $null = Invoke-RestMethod -Method Post -Uri ($session.encryptedApiBaseURL + "/v1/stop") -Headers $headers -ContentType "application/json" -Body "{}" -SkipCertificateCheck -TimeoutSec 15
    Write-Host "Windows desktop smoke PASS: GUI native bridge launched the bundled daemon and authenticated /v1/status plus /v1/stop succeeded."
}
finally {
    if ($null -ne $desktop -and !$desktop.HasExited) {
        Stop-Process -Id $desktop.Id -Force -ErrorAction SilentlyContinue
    }
}
