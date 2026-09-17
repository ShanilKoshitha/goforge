[CmdletBinding()]
param(
    [ValidatePattern('^(latest|v\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?)$')]
    [string] $Version = 'latest',

    # Add Forge to this PowerShell process without changing the persistent user PATH.
    [switch] $SessionOnly,

    # Intended for CI and managed environments that configure PATH separately.
    [switch] $SkipPathUpdate
)

$ErrorActionPreference = 'Stop'

function Invoke-Go {
    param([string[]] $GoArguments)

    & go @GoArguments
    if ($LASTEXITCODE -ne 0) {
        throw "go $($GoArguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Test-PathEntry {
    param(
        [AllowNull()][AllowEmptyString()][string] $PathValue,
        [string] $Directory
    )

    $wanted = $Directory.TrimEnd('\', '/')
    foreach ($entry in ($PathValue -split [IO.Path]::PathSeparator)) {
        if ($entry.Trim().TrimEnd('\', '/') -ieq $wanted) {
            return $true
        }
    }
    return $false
}

Write-Host '[1/4] Checking the Go toolchain...'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go was not found. Install Go from https://go.dev/dl/ and run this installer again.'
}
Invoke-Go -GoArguments @('version')

$module = 'github.com/ShanilKoshitha/goforge'
$resolvedVersion = (& go list -m -f '{{.Version}}' "$module@$Version").Trim()
if ($LASTEXITCODE -ne 0 -or $resolvedVersion -notmatch '^v\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$') {
    throw "Could not resolve GoForge version $Version."
}

$installDirectory = (& go env GOBIN).Trim()
if ($LASTEXITCODE -ne 0) {
    throw 'Could not determine GOBIN.'
}
if ([string]::IsNullOrWhiteSpace($installDirectory)) {
    $goPath = (& go env GOPATH).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($goPath)) {
        throw 'Could not determine GOPATH.'
    }
    $goPathRoot = ($goPath -split [IO.Path]::PathSeparator)[0]
    $installDirectory = Join-Path $goPathRoot 'bin'
}

$package = "$module/cmd/forge@$resolvedVersion"
Write-Host "[2/4] Installing $package..."
Invoke-Go -GoArguments @('install', '-v', $package)

$forge = Join-Path $installDirectory 'forge.exe'
if (-not (Test-Path -LiteralPath $forge -PathType Leaf)) {
    throw "Go reported success, but $forge was not created."
}

Write-Host '[3/4] Verifying the installation...'
$buildMetadata = (& go version -m $forge)
if ($LASTEXITCODE -ne 0) {
    throw "Could not read build metadata from $forge."
}
$modulePattern = '(?m)^\s*mod\s+' + [regex]::Escape($module) + '\s+' + [regex]::Escape($resolvedVersion) + '(\s|$)'
if (($buildMetadata -join "`n") -notmatch $modulePattern) {
    throw "Installed binary metadata does not identify $module $resolvedVersion."
}
$installedVersion = (& $forge version)
if ($LASTEXITCODE -ne 0) {
    throw "Installed Forge could not start (exit code $LASTEXITCODE)."
}
$expectedVersion = "forge $($resolvedVersion.TrimStart('v'))"
if ($installedVersion -ne $expectedVersion) {
    throw "Installed Forge reported '$installedVersion'; expected '$expectedVersion'."
}

Write-Host "[4/4] Making $installDirectory available as forge..."
if ($SkipPathUpdate) {
    Write-Host 'Skipping the PATH update as requested.'
} else {
    if (-not (Test-PathEntry $env:Path $installDirectory)) {
        $env:Path = "$installDirectory$([IO.Path]::PathSeparator)$env:Path"
    }

    if ($SessionOnly) {
        Write-Host 'Skipping the persistent user PATH update as requested.'
    } else {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not (Test-PathEntry $userPath $installDirectory)) {
            $updatedUserPath = if ([string]::IsNullOrWhiteSpace($userPath)) {
                $installDirectory
            } else {
                "$installDirectory$([IO.Path]::PathSeparator)$userPath"
            }
            try {
                [Environment]::SetEnvironmentVariable('Path', $updatedUserPath, 'User')
                Write-Host 'Added the Go binary directory to your user PATH.'
            } catch {
                Write-Warning "Forge is available in this PowerShell session, but the user PATH could not be updated: $($_.Exception.Message)"
                Write-Warning "Add $installDirectory to PATH manually for future terminals."
            }
        } else {
            Write-Host 'The Go binary directory is already in your user PATH.'
        }
    }
}

Write-Host "Success: $installedVersion is installed at $forge"
if (-not $SkipPathUpdate) {
    Write-Host 'You can now run: forge new myapp --module example.com/myapp'
}
