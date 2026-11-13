<#
.SYNOPSIS
  Builds an MSIX package from this folder's app_manifest.xml and Assets\ logos.

.DESCRIPTION
  Used instead of `wails3 tool msix`, which ignores the project's manifest and
  logos (it writes its own manifest from build/config.yml and blank placeholder
  images). This keeps the Store identity (Identity Name / Publisher from Partner
  Center) in app_manifest.xml, separate from productIdentifier in config.yml.

  Steps: stage exe + AppxManifest.xml + Assets\ in a layout folder, set the
  manifest's ProcessorArchitecture for -Arch, run MakeAppx pack, and optionally
  sign with SignTool. Leave the package unsigned for Microsoft Store upload
  (the Store signs it); sign only to sideload/test locally, using a certificate
  whose subject matches the manifest's Publisher.

.EXAMPLE
  powershell -File package.ps1 -Exe ..\..\..\bin\timers.exe -Out ..\..\..\bin\timers-amd64.msix -Arch amd64
#>
param(
    [Parameter(Mandatory)] [string] $Exe,
    [Parameter(Mandatory)] [string] $Out,
    [ValidateSet('amd64', 'arm64', 'x64')] [string] $Arch = 'amd64',
    [string] $Cert = '',          # .pfx to sign with (optional)
    [string] $LayoutDir = ''      # stage here and keep it (for inspection); default is a temp folder
)

$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot

function Find-SdkTool([string] $name) {
    $cmd = Get-Command $name -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $root = ${env:ProgramFiles(x86)}
    if (-not $root) { $root = 'C:\Program Files (x86)' }
    $hostArch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'x64' }
    $found = Get-ChildItem -Path (Join-Path $root 'Windows Kits\10\bin') -Filter $name -Recurse -ErrorAction SilentlyContinue |
        Where-Object { $_.Directory.Name -eq $hostArch } |
        Sort-Object { $_.Directory.Parent.Name } -Descending |
        Select-Object -First 1
    if (-not $found) {
        throw "$name not found. Install the Windows SDK (e.g. 'winget install Microsoft.WindowsSDK.10.0.26100') or run 'wails3 task windows:install:msix:tools'."
    }
    return $found.FullName
}

$Exe = (Resolve-Path $Exe).Path
$manifestSrc = Join-Path $here 'app_manifest.xml'
$assetsSrc = Join-Path $here 'Assets'
foreach ($p in @($manifestSrc, $assetsSrc)) {
    if (-not (Test-Path $p)) { throw "Missing $p" }
}

$keep = [bool]$LayoutDir
if (-not $keep) { $LayoutDir = Join-Path ([IO.Path]::GetTempPath()) ("msix-" + [guid]::NewGuid()) }
if (Test-Path $LayoutDir) { Remove-Item $LayoutDir -Recurse -Force }
New-Item -ItemType Directory -Path $LayoutDir | Out-Null

try {
    # Manifest: copy, set the architecture for this build, check the exe name matches.
    [xml] $xml = Get-Content -Raw -Encoding UTF8 $manifestSrc
    $msixArch = if ($Arch -eq 'arm64') { 'arm64' } else { 'x64' }
    $xml.Package.Identity.SetAttribute('ProcessorArchitecture', $msixArch)
    $exeName = Split-Path $Exe -Leaf
    foreach ($app in $xml.Package.Applications.Application) {
        if ($app.Executable -ne $exeName) {
            throw "app_manifest.xml Application Executable='$($app.Executable)' but the built exe is '$exeName'"
        }
    }
    $settings = New-Object System.Xml.XmlWriterSettings
    $settings.Indent = $true
    $settings.Encoding = New-Object System.Text.UTF8Encoding($false)
    $writer = [System.Xml.XmlWriter]::Create((Join-Path $LayoutDir 'AppxManifest.xml'), $settings)
    $xml.Save($writer)
    $writer.Close()

    Copy-Item $Exe (Join-Path $LayoutDir $exeName)
    Copy-Item $assetsSrc (Join-Path $LayoutDir 'Assets') -Recurse

    Write-Host "Staged MSIX layout in $LayoutDir"
    Write-Host ("  Identity: {0}  Publisher: {1}  Version: {2}  Arch: {3}" -f `
        $xml.Package.Identity.Name, $xml.Package.Identity.Publisher, $xml.Package.Identity.Version, $msixArch)

    $makeAppx = Find-SdkTool 'makeappx.exe'
    $outDir = Split-Path $Out -Parent
    if ($outDir -and -not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir | Out-Null }
    & $makeAppx pack /o /d $LayoutDir /p $Out
    if ($LASTEXITCODE -ne 0) { throw "MakeAppx failed ($LASTEXITCODE)" }

    if ($Cert) {
        $signTool = Find-SdkTool 'signtool.exe'
        $signArgs = @('sign', '/fd', 'SHA256', '/f', (Resolve-Path $Cert).Path)
        if ($env:MSIX_CERT_PASSWORD) { $signArgs += @('/p', $env:MSIX_CERT_PASSWORD) }
        & $signTool @signArgs $Out
        if ($LASTEXITCODE -ne 0) { throw "SignTool failed ($LASTEXITCODE)" }
    }

    Write-Host "MSIX package created: $Out"
}
finally {
    if (-not $keep -and (Test-Path $LayoutDir)) { Remove-Item $LayoutDir -Recurse -Force }
}
