# Download the latest KubeLoop desktop release for Windows.
# Usage:
#   irm https://raw.githubusercontent.com/fengqi-dev/kube-loop/main/scripts/install.ps1 | iex
#   .\scripts\install.ps1 -Version v1.1.0
param(
  [string]$Version = $env:VERSION,
  [string]$Repo = $(if ($env:REPO) { $env:REPO } else { "fengqi-dev/kube-loop" }),
  [string]$Dest = (Get-Location).Path,
  [ValidateSet("installer", "portable")]
  [string]$Package = "installer"
)

$ErrorActionPreference = "Stop"

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }

if ($Version) {
  $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/tags/$Version"
} else {
  $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
}

$tag = $rel.tag_name
$ver = $tag.TrimStart("v")
$names = if ($Package -eq "installer") {
  @(
    "kubeloop-$ver-windows-$arch-installer.exe",
    "kubeloop-windows-$arch-installer.exe"
  )
} else {
  @(
    "kubeloop-$ver-windows-$arch.zip",
    "kubeloop-windows-$arch.zip"
  )
}

$asset = $rel.assets | Where-Object { $names -contains $_.name } | Select-Object -First 1
if (-not $asset -and $Package -eq "installer") {
  # Fall back to portable zip when the NSIS installer is absent (e.g. v1.0.0).
  $names = @(
    "kubeloop-$ver-windows-$arch.zip",
    "kubeloop-windows-$arch.zip"
  )
  $asset = $rel.assets | Where-Object { $names -contains $_.name } | Select-Object -First 1
  $Package = "portable"
}
if (-not $asset) {
  throw "no matching Windows/$arch asset in $tag"
}

New-Item -ItemType Directory -Force -Path $Dest | Out-Null
$out = Join-Path $Dest $asset.name
Write-Host "Downloading $($asset.name) ($tag)..."
Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $out

if ($Package -eq "installer" -or $out -like "*.exe") {
  Write-Host "Starting installer: $out"
  Start-Process -FilePath $out
} else {
  $extract = Join-Path $Dest "kubeloop"
  New-Item -ItemType Directory -Force -Path $extract | Out-Null
  Expand-Archive -Path $out -DestinationPath $extract -Force
  Write-Host "Extracted portable build to $extract"
  Write-Host "Run KubeLoop.exe from that folder."
}
