@echo off
chcp 65001 >nul
echo ============================================
echo   扫码点餐管理系统 - 一键启动
echo ============================================
echo.

rem 切到后端目录（%~dp0 为本脚本所在目录）
cd /d "%~dp0backend"

rem 若端口已被占用，先提示
netstat -ano | findstr ":8080" | findstr "LISTENING" >nul
if %errorlevel%==0 (
    echo [提示] 端口 8080 已被占用，可能服务已在运行。
    echo        将直接打开浏览器，如需重启请先运行 stop.bat。
    goto open
)

echo [1/2] 正在启动后端服务（最小化窗口，请勿关闭）...
start "dining-server" /min bin\dining-server.exe
timeout /t 2 /nobreak >nul

:open
echo [2/2] 正在打开浏览器...
start http://localhost:8080
echo.
echo 启动完成！管理端默认账号：admin / admin123
echo 关闭服务请运行同目录下的 stop.bat
echo.
pause
