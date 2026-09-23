@echo off
REM 停止门店打印代理(Windows):只结束运行实例,自启动保留,重启后自动恢复。
schtasks /end /tn "DiningPrintAgent" >nul 2>&1
echo [已停止] 自启动保留(重启后自动恢复)
