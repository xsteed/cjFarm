@echo off
REM 打印门店打印代理运行状态(Windows):
REM   任务计划/进程状态 + 最近 15 行日志 + 云端连通自检提示。
echo == 任务计划 ==
schtasks /query /tn "DiningPrintAgent" 2>nul | findstr /i "DiningPrintAgent"
if errorlevel 1 echo   未安装(找不到任务计划 DiningPrintAgent)

echo.
echo == 进程状态 ==
tasklist 2>nul | findstr /i "print-agent" || echo   未运行

echo.
echo == 最近日志(末 15 行,文件: print-agent.log) ==
powershell -NoProfile -Command "if (Test-Path print-agent.log) { Get-Content print-agent.log -Tail 15 } else { Write-Host '<尚无日志>(代理未启动或未配置 PRINT_AGENT_LOG)' }"

echo.
echo == 云端连通自检 ==
echo   手动执行: print-agent-windows-amd64.exe --once
