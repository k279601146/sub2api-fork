Write-Host "=============================================" -ForegroundColor Red
Write-Host "       Stopping Backend & Frontend Services  " -ForegroundColor Red
Write-Host "=============================================" -ForegroundColor Red

# 定义需要关闭的端口 (已更新：前端设为 3000)
$ports = @(8080, 3000)

foreach ($port in $ports) {
    Write-Host "Checking port $port..." -ForegroundColor Yellow
    # 查找占用该端口的 PID
    $processId = Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique
    
    if ($processId) {
        foreach ($id in $processId) {
            $processName = (Get-Process -Id $id -ErrorAction SilentlyContinue).ProcessName
            Write-Host "--> Killing process $processName (PID: $id) on port $port" -ForegroundColor Red
            Stop-Process -Id $id -Force
        }
    } else {
        Write-Host "--> No process found on port $port." -ForegroundColor Green
    }
}

Write-Host "---------------------------------------------" -ForegroundColor Red
Write-Host "All specified services have been stopped." -ForegroundColor Red
Start-Sleep -Seconds 2