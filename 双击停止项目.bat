@echo off
:: 运行停止脚本，并以管理员权限执行（某些端口占用需要权限才能终止）
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0stop-project.ps1"
pause