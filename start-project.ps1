# 1. 自动定位工作目录
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrEmpty($ScriptDir)) { $ScriptDir = Get-Location }
Set-Location $ScriptDir

Write-Host "=============================================" -ForegroundColor Cyan
Write-Host "       Starting Backend & Frontend Services   " -ForegroundColor Cyan
Write-Host "=============================================" -ForegroundColor Cyan

# 2. 启动后端服务 (Go)
Write-Host "[1/2] Launching Go Backend (Port 8080)..." -ForegroundColor Green
Start-Process powershell -ArgumentList "-NoExit", "-Command", "Set-Location '$ScriptDir'; cd backend; go run ./cmd/server 2>&1 | tee server.log"

# 3. 检查并启动前端服务 (Vue/Vite)
Write-Host "[2/2] Checking frontend dependencies..." -ForegroundColor Green
if (-not (Test-Path "frontend\node_modules")) {
    Write-Host "--> node_modules not found. Installing and starting..." -ForegroundColor Yellow
    Start-Process powershell -ArgumentList "-NoExit", "-Command", "Set-Location '$ScriptDir'; cd frontend; pnpm install; npm run dev"
} else {
    Write-Host "--> node_modules found. Starting dev server..." -ForegroundColor Green
    Start-Process powershell -ArgumentList "-NoExit", "-Command", "Set-Location '$ScriptDir'; cd frontend; npm run dev"
}

Write-Host "---------------------------------------------" -ForegroundColor Cyan
Write-Host "All services started successfully!" -ForegroundColor Cyan
Write-Host "You can close this main window now." -ForegroundColor Cyan