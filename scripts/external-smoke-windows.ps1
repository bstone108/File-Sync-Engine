param(
    [string]$Target = "",
    [string]$BundleRoot = "",
    [string]$RunId = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($BundleRoot)) {
    $BundleRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
}
if ([string]::IsNullOrWhiteSpace($RunId)) {
    $RunId = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
}
if ([string]::IsNullOrWhiteSpace($Target)) {
    $arch = (Get-CimInstance Win32_OperatingSystem).OSArchitecture
    if ($arch -match "ARM") { $Target = "windows-arm64" } else { $Target = "windows-amd64" }
}

$Bin = Join-Path $BundleRoot "bin\$Target\fse-$Target.exe"
if (!(Test-Path $Bin)) {
    throw "missing executable: $Bin"
}

$ResultRoot = Join-Path $BundleRoot "results\$Target-$RunId"
$Work = Join-Path $ResultRoot "work"
$Share = Join-Path $Work "share"
$LogDir = Join-Path $ResultRoot "logs"
New-Item -ItemType Directory -Force -Path $Share, $LogDir | Out-Null

$Config = Join-Path $Work "config.jsonc"
$Summary = Join-Path $ResultRoot "summary.md"
$Results = Join-Path $ResultRoot "results.json"
$HostFile = Join-Path $ResultRoot "host.json"
$CommandsJson = Join-Path $ResultRoot "commands.jsonl"

function Write-HostFacts {
    $os = Get-CimInstance Win32_OperatingSystem
    $cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
    $drive = Get-PSDrive -Name ([System.IO.Path]::GetPathRoot($ResultRoot).Substring(0,1))
    $facts = [ordered]@{
        target = $Target
        os = $os.Caption
        osVersion = $os.Version
        arch = $os.OSArchitecture
        cpu = $cpu.Name
        cores = $cpu.NumberOfLogicalProcessors
        ramBytes = [int64]$os.TotalVisibleMemorySize * 1024
        storage = "$([int64]$drive.Free) bytes free on $($drive.Root)"
        filesystem = "windows"
        goVersion = (& go version 2>$null) -join ""
        startUtc = (Get-Date).ToUniversalTime().ToString("o")
    }
    $facts | ConvertTo-Json -Depth 4 | Set-Content -Encoding UTF8 $HostFile
}

function Invoke-Step {
    param(
        [string]$Name,
        [scriptblock]$Command,
        [switch]$AllowFailure
    )
    $log = Join-Path $LogDir "$Name.log"
    $start = (Get-Date).ToUniversalTime().ToString("o")
    $status = 0
    try {
        & $Command *> $log
    } catch {
        $status = 1
        $_ | Out-String | Add-Content -Encoding UTF8 $log
    }
    $end = (Get-Date).ToUniversalTime().ToString("o")
    $record = [ordered]@{ name = $Name; status = $status; startUtc = $start; endUtc = $end; log = "logs/$Name.log" }
    ($record | ConvertTo-Json -Compress) | Add-Content -Encoding UTF8 $CommandsJson
    if ($status -ne 0 -and -not $AllowFailure) { return $false }
    return $true
}

Write-HostFacts
Set-Content -Encoding UTF8 $CommandsJson ""
Set-Content -Encoding UTF8 (Join-Path $Share "hello.txt") "hello from external smoke"
Set-Content -Encoding UTF8 $Summary "external smoke run for $Target`nstarted: $((Get-Date).ToUniversalTime().ToString('o'))`n"

$Pass = $true
# Literal command names below document the intended CLI smoke: fse.exe config init, fse.exe folder add, fse.exe validate, fse.exe scan, fse.exe status.
if (!(Invoke-Step "config_init" { & $Bin config init $Config })) { $Pass = $false }
if (!(Invoke-Step "folder_add" { & $Bin folder add smoke $Share --mode sendrecv $Config })) { $Pass = $false }
if (!(Invoke-Step "validate" { & $Bin validate $Config })) { $Pass = $false }
if (!(Invoke-Step "scan" { & $Bin scan --folder smoke $Config })) { $Pass = $false }
Invoke-Step "status_expected_no_daemon" { & $Bin status $Config } -AllowFailure | Out-Null
if (!(Invoke-Step "service_render" { & $Bin service render --platform windows --binary $Bin $Config })) { $Pass = $false }

$resultObj = [ordered]@{
    target = $Target
    runId = $RunId
    passed = $Pass
    endUtc = (Get-Date).ToUniversalTime().ToString("o")
    host = "host.json"
    commands = "commands.jsonl"
    summary = "summary.md"
}
$resultObj | ConvertTo-Json -Depth 4 | Set-Content -Encoding UTF8 $Results

Add-Content -Encoding UTF8 $Summary "`n## Result`n"
if ($Pass) { Add-Content -Encoding UTF8 $Summary "PASS" } else { Add-Content -Encoding UTF8 $Summary "FAIL" }
Add-Content -Encoding UTF8 $Summary "`n## Files`n- host.json`n- results.json`n- commands.jsonl`n- logs/`n"

$ResultsDir = Join-Path $BundleRoot "results"
$Archive = Join-Path $ResultsDir "$Target-$RunId.zip"
if (Test-Path $Archive) { Remove-Item $Archive -Force }
Compress-Archive -Path $ResultRoot -DestinationPath $Archive

if ($Pass) {
    Write-Host "external smoke PASS; results in $ResultRoot"
} else {
    Write-Error "external smoke FAIL; results in $ResultRoot"
    exit 1
}
