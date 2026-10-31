# Installs the flobase CLI on Windows (x64 or ARM64), then starts FloBase,
# which installs WSL 2 and Docker Desktop first if they are missing.
#
#   irm https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install-cli.ps1 | iex
#
# Options, as environment variables:
#   FLOBASE_VERSION    release to install, e.g. 0.2.0 (default: latest)
#   FLOBASE_NO_START   set to 1 to install the CLI only

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$repo = if ($env:FLOBASE_UPDATE_REPO) { $env:FLOBASE_UPDATE_REPO } else { 'flomo-abel-231/flobase' }
$version = if ($env:FLOBASE_VERSION) { $env:FLOBASE_VERSION } else { 'latest' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$binDir = Join-Path $env:LOCALAPPDATA 'FloBase\bin'

$base = if ($version -eq 'latest') {
  "https://github.com/$repo/releases/latest/download"
} else {
  "https://github.com/$repo/releases/download/v$($version.TrimStart('v'))"
}
$file = "flobase_windows_$arch.zip"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("flobase-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
  Write-Host "==> Downloading flobase for windows/$arch"
  Invoke-WebRequest "$base/$file" -OutFile (Join-Path $tmp $file)
  Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

  $expected = (Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($file))$" }) -split ' ' | Select-Object -First 1
  $actual = (Get-FileHash (Join-Path $tmp $file) -Algorithm SHA256).Hash.ToLower()
  if (-not $expected -or $expected -ne $actual) { throw "Checksum mismatch for $file." }

  Expand-Archive (Join-Path $tmp $file) -DestinationPath $tmp -Force
  New-Item -ItemType Directory -Path $binDir -Force | Out-Null
  Copy-Item (Join-Path $tmp 'flobase.exe') (Join-Path $binDir 'flobase.exe') -Force
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $binDir) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
  Write-Host "==> Added $binDir to your PATH; open a new terminal to use flobase by name"
}
$env:Path = "$env:Path;$binDir"
Write-Host "==> Installed $(& (Join-Path $binDir 'flobase.exe') version)"

if ($env:FLOBASE_NO_START -eq '1') {
  Write-Host "Run 'flobase start' to set up and start FloBase."
} else {
  & (Join-Path $binDir 'flobase.exe') start
}
