param(
    [string]$Image = "ccr.ccs.tencentyun.com/sub2apifork/sub2apidepliy",
    [string]$Tag = "",
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

    if (-not $SkipLogin) {
        Write-Host "Logging in to $Registry ..."
        docker login $Registry
    }

    Write-Host "Building $Image:$Tag ..."
    docker build `
        --build-arg COMMIT=$Commit `
        --build-arg DATE=$Date `
        -t "$Image:$Tag" `
        .

    Write-Host "Pushing $Image:$Tag ..."
    docker push "$Image:$Tag"

    if (-not $NoLatest) {
        Write-Host "Tagging and pushing $Image:latest ..."
        docker tag "$Image:$Tag" "$Image:latest"
        docker push "$Image:latest"
    }

    Write-Host ""
    Write-Host "Done."
    Write-Host "Server deploy:"
    Write-Host "  cd /www/wwwroot/sub2api-deploy/deploy && ./update.sh $Tag"
}
finally {
    Pop-Location
}
