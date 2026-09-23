@echo off
REM 自助升级门店打印代理(Windows):从云端下载最新版本并替换自身,随后自动触发任务计划重启。
REM 前提:服务端已部署新产物且管理后台已配置「代理最新版本号」。
setlocal
set BIN=
if exist "print-agent.exe" set BIN=print-agent.exe
if not defined BIN if exist "print-agent-windows-amd64.exe" set BIN=print-agent-windows-amd64.exe
if not defined BIN (
  echo [错误] 未找到代理产物(print-agent.exe / print-agent-windows-amd64.exe^),请先把它放到本脚本同目录
  exit /b 1
)
"%BIN%" --upgrade
