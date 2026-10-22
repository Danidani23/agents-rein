# Installs the latest release of rein, or the one $env:REIN_VERSION names,
# into $env:LOCALAPPDATA\Programs\rein or $env:REIN_INSTALL_DIR: the archive
# for this machine, checked against the release's checksum file.
#
#   irm https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.ps1 | iex
$ErrorActionPreference = "Stop"

$repository = "Danidani23/agents-rein"
$version = if ($env:REIN_VERSION) { $env:REIN_VERSION } else { "latest" }
$targetDir = if ($env:REIN_INSTALL_DIR) { $env:REIN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\rein" }
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }

if ($version -eq "latest") {
  $version = (Invoke-RestMethod "https://api.github.com/repos/$repository/releases/latest").tag_name
}
$number = $version.TrimStart("v")
$archive = "rein_${number}_windows_${arch}.zip"
$checksums = "rein_${number}_checksums.txt"
$base = "https://github.com/$repository/releases/download/$version"

$scratch = Join-Path ([System.IO.Path]::GetTempPath()) ("rein-" + [System.Guid]::NewGuid())
New-Item -ItemType Directory -Path $scratch | Out-Null
try {
  Write-Host "downloading rein $version for windows/$arch"
  Invoke-WebRequest -Uri "$base/$archive" -OutFile (Join-Path $scratch $archive)
  Invoke-WebRequest -Uri "$base/$checksums" -OutFile (Join-Path $scratch $checksums)
  $line = Get-Content (Join-Path $scratch $checksums) | Where-Object { $_ -match " $([regex]::Escape($archive))$" } | Select-Object -First 1
  if (-not $line) { throw "the checksum file names no $archive; not installing" }
  $expected = ($line -split "\s+")[0]
  $actual = (Get-FileHash (Join-Path $scratch $archive) -Algorithm SHA256).Hash.ToLower()
  if ($expected -ne $actual) { throw "checksum mismatch for $archive; not installing" }
  Expand-Archive -Path (Join-Path $scratch $archive) -DestinationPath $scratch -Force
  New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
  Copy-Item (Join-Path $scratch "rein.exe") (Join-Path $targetDir "rein.exe") -Force
  Write-Host "installed rein $version to $targetDir\rein.exe"
  if (($env:Path -split ";") -notcontains $targetDir) {
    Write-Host "add $targetDir to your PATH: [Environment]::SetEnvironmentVariable('Path', `$env:Path + ';$targetDir', 'User')"
  }
} finally {
  Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
}
