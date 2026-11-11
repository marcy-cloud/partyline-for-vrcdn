# Builds the Windows release: a single exe with libmpv compiled in, zipped
# together with the licence files it has to be distributed with.
#
#   powershell -ExecutionPolicy Bypass -File scripts\package.ps1
#
# Output: dist\partyline-<version>-windows-amd64.zip
# Needs Go, Node and the Wails v3 CLI (see README). libmpv is downloaded first
# if tools/fetch-libmpv has not been run yet.

$ErrorActionPreference = "Stop"

$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

function Invoke-Checked([string]$what, [scriptblock]$command) {
    & $command
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

$bundled = Join-Path $root "internal\libmpv\bundled"
if (-not (Test-Path (Join-Path $bundled "manifest.json"))) {
    Invoke-Checked "fetching libmpv" { go run -C tools/fetch-libmpv . }
}
$libmpv = Get-Content (Join-Path $bundled "manifest.json") -Raw | ConvertFrom-Json

Invoke-Checked "wails3 build" { wails3 build EXTRA_TAGS=bundled_libmpv }

$version = (Get-Content (Join-Path $root "build\windows\info.json") -Raw | ConvertFrom-Json).fixed.product_version
$name = "partyline-$version-windows-amd64"
$stage = Join-Path $root "dist\$name"
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Path $stage | Out-Null

Copy-Item (Join-Path $root "bin\partyline.exe") $stage
Copy-Item (Join-Path $root "LICENSE") (Join-Path $stage "LICENSE.txt")

# The notices, plus exactly which libmpv build is inside this exe.
$notices = Get-Content (Join-Path $root "THIRD-PARTY-NOTICES.md") -Raw
$notices += @"

## libmpv build in this release

- Archive: $($libmpv.asset) (release $($libmpv.release))
- Downloaded from: $($libmpv.source)
- SHA-256 of libmpv-2.dll: $($libmpv.sha256)
"@
[IO.File]::WriteAllText((Join-Path $stage "THIRD-PARTY-NOTICES.md"), $notices)

# Licence texts of every Go module compiled into the exe.
$licenses = Join-Path $stage "licenses"
$modules = go list -deps -tags "production,bundled_libmpv" -f "{{with .Module}}{{.Path}}|{{.Dir}}{{end}}" . |
    Sort-Object -Unique
if ($LASTEXITCODE -ne 0) { throw "go list failed" }
foreach ($line in $modules) {
    $path, $dir = $line -split '\|', 2
    if (-not $dir -or $dir -eq $root) { continue }
    $texts = Get-ChildItem $dir -File | Where-Object { $_.Name -match '^(LICEN[CS]E|COPYING|NOTICE)' }
    if (-not $texts) { throw "no licence file found for $path in $dir" }
    $target = Join-Path $licenses ($path -replace '[\\/]', '_')
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    $texts | Copy-Item -Destination $target
}
$goRoot = go env GOROOT
New-Item -ItemType Directory -Force -Path (Join-Path $licenses "go") | Out-Null
Copy-Item (Join-Path $goRoot "LICENSE") (Join-Path $licenses "go")
# ...and of what is compiled into the UI.
$uiPackages = "svelte", "@fontsource-variable/fredoka", "@fontsource-variable/nunito", "@fontsource/vt323"
foreach ($package in $uiPackages) {
    $target = Join-Path $licenses ($package -replace '[\\/]', '_')
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    Get-ChildItem (Join-Path $root "frontend\node_modules\$package") -File -Filter "LICENSE*" |
        Copy-Item -Destination $target
}

$zip = Join-Path $root "dist\$name.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
# Written entry by entry with "/" separators: Compress-Archive and
# ZipFile.CreateFromDirectory in Windows PowerShell 5.1 both write
# backslashes, which only Windows unpacks properly.
Add-Type -AssemblyName System.IO.Compression, System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::Open($zip, [IO.Compression.ZipArchiveMode]::Create)
try {
    foreach ($file in Get-ChildItem $stage -Recurse -File) {
        $entry = "$name/" + $file.FullName.Substring($stage.Length + 1).Replace('\', '/')
        [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
            $archive, $file.FullName, $entry, [IO.Compression.CompressionLevel]::Optimal)
    }
} finally {
    $archive.Dispose()
}

$exe = Get-Item (Join-Path $stage "partyline.exe")
Write-Host ("Done: {0} ({1:N0} MB; the exe alone is {2:N0} MB, libmpv {3})" -f `
    $zip, ((Get-Item $zip).Length / 1MB), ($exe.Length / 1MB), $libmpv.release)
