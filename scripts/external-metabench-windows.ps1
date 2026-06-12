param(
  [Parameter(Mandatory=$true)] [string] $Target,
  [string] $Timeout = "30m"
)

$ErrorActionPreference = "Stop"
$ScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$BundleRoot = Split-Path -Parent $ScriptRoot
$ResultsRoot = Join-Path $BundleRoot "results"
$BenchBin = Join-Path $BundleRoot (Join-Path "bin" (Join-Path $Target "fse-metabench.exe"))

if (-not (Test-Path $BenchBin)) {
  throw "missing executable benchmark binary for $Target`: $BenchBin"
}

$RunID = "{0}-{1}" -f (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ"), $Target
$RunDir = Join-Path $ResultsRoot $RunID
$LogDir = Join-Path $RunDir "logs"
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

$HostFacts = [ordered]@{
  target = $Target
  capturedAt = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  operatingSystem = (Get-CimInstance Win32_OperatingSystem | Select-Object Caption, Version, BuildNumber, OSArchitecture)
  processor = (Get-CimInstance Win32_Processor | Select-Object Name, NumberOfCores, NumberOfLogicalProcessors, MaxClockSpeed)
  memory = (Get-CimInstance Win32_ComputerSystem | Select-Object TotalPhysicalMemory)
  disks = (Get-CimInstance Win32_LogicalDisk | Select-Object DeviceID, Size, FreeSpace, FileSystem)
}
$HostFacts | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 (Join-Path $RunDir "host.json")

$Stdout = Join-Path $LogDir "fse-metabench.stdout.log"
$Stderr = Join-Path $LogDir "fse-metabench.stderr.log"
$Report = Join-Path $RunDir "metadata-benchmark.md"
$Process = Start-Process -FilePath $BenchBin -ArgumentList @("-timeout", $Timeout, "-output", $Report) -Wait -PassThru -NoNewWindow -RedirectStandardOutput $Stdout -RedirectStandardError $Stderr
$ExitCode = $Process.ExitCode

@"
# FSE Metadata Benchmark External Run

- Target: $Target
- Timeout: $Timeout
- Exit code: $ExitCode
- Benchmark report: metadata-benchmark.md
- Host facts: host.json

These results are one host's evidence only. Compare against other hardware before locking the production metadata backend.
"@ | Set-Content -Encoding UTF8 (Join-Path $RunDir "summary.md")

$Archive = Join-Path $ResultsRoot "fse-external-metabench-$RunID.zip"
if (Test-Path $Archive) { Remove-Item $Archive -Force }
Compress-Archive -Path (Join-Path $RunDir "*") -DestinationPath $Archive
Write-Host "metadata benchmark results written to $RunDir"
exit $ExitCode
