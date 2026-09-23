@echo off
REM 启动门店打印代理(Windows):触发任务计划;未安装时给出安装引导。
schtasks /query /tn "DiningPrintAgent" >nul 2>&1
if errorlevel 1 (
  echo [未安装] 找不到任务计划 DiningPrintAgent
  echo   请先安装(一条命令,自动配置 + 注册自启动^):
  echo     print-agent-windows-amd64.exe --install --server https://你的管理后台域名 --token 你的令牌
  exit /b 1
)
schtasks /run /tn "DiningPrintAgent" >nul 2>&1
echo [已启动] 任务计划 DiningPrintAgent 已触发
