@echo off
chcp 65001 >nul
echo 正在停止扫码点餐系统后端服务...
taskkill /f /im dining-server.exe >nul 2>&1
if %errorlevel%==0 (
    echo 后端服务已停止。
) else (
    echo 未发现正在运行的后端服务（dining-server.exe）。
)
echo.
pause
