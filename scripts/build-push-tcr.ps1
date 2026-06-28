param(
    [string]$Image = "ccr.ccs.tencentyun.com/sub2apifork/sub2apidepliy",
    [string]$Tag = "",
    [string]$Username = $(if ($env:TCR_USERNAME) { $env:TCR_USERNAME } else { "100009512456" }),
    [string]$Password = $env:TCR_PASSWORD,
    [switch]$NoLatest,
    [switch]$SkipLogin
)

$ErrorActionPreference = "Stop"

function Require-Value {
    param([string]$Name, [string]$Value)
    if ([string]::IsNullOrWhiteSpace($Value)) {
        throw "$Name is required. Pass -$Name or set the matching environment variable."
    }
}

function Require-Command {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command not found: $Name"
    }
}

function Invoke-Native {
    param(
        [string]$Command,
        [string[]]$Arguments
    )

    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command failed with exit code $LASTEXITCODE"
    }
}

Require-Command "git"
Require-Command "docker"
Require-Value "Image" $Image

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Push-Location $RepoRoot
try {
    if ([string]::IsNullOrWhiteSpace($Tag)) {
        $Tag = (git rev-parse --short HEAD).Trim()
    }

    $Registry = ($Image -split "/")[0]
    $Commit = (git rev-parse --short HEAD).Trim()
    $Date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    Write-Host "Checking Docker daemon ..."
    Invoke-Native "docker" @("info")

    if (-not $SkipLogin) {
        Write-Host "Logging in to $Registry ..."
        if ([string]::IsNullOrWhiteSpace($Password)) {
            Invoke-Native "docker" @("login", $Registry, "--username", $Username)
        }
        else {
            $Password | docker login $Registry --username $Username --password-stdin
            if ($LASTEXITCODE -ne 0) {
                throw "docker failed with exit code $LASTEXITCODE"
            }
        }
    }

    Write-Host "Building ${Image}:${Tag} ..."
    Invoke-Native "docker" @(
        "build",
        "--build-arg", "COMMIT=$Commit",
        "--build-arg", "DATE=$Date",
        "-t", "${Image}:${Tag}",
        "."
    )

    Write-Host "Pushing ${Image}:${Tag} ..."
    Invoke-Native "docker" @("push", "${Image}:${Tag}")

    if (-not $NoLatest) {
        Write-Host "Tagging and pushing ${Image}:latest ..."
        Invoke-Native "docker" @("tag", "${Image}:${Tag}", "${Image}:latest")
        Invoke-Native "docker" @("push", "${Image}:latest")
    }

    Write-Host ""
    Write-Host "Done."
    Write-Host "Server deploy:"
    Write-Host "  cd /www/wwwroot/sub2api-deploy/deploy && ./update.sh $Tag"
}
finally {
    Pop-Location
}
