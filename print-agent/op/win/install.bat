@echo off
REM 安装门店打印代理(Windows):写配置 + 注册开机自启。
REM 用法: install.bat [管理后台地址] [代理令牌]  (不带参数则交互式询问)
setlocal
set BIN=
if exist "print-agent.exe" set BIN=print-agent.exe
if not defined BIN if exist "print-agent-windows-amd64.exe" set BIN=print-agent-windows-amd64.exe
if not defined BIN (
  echo [错误] 未找到代理产物(print-agent.exe / print-agent-windows-amd64.exe^),请先把它放到本脚本同目录
  exit /b 1
)
if not "%2"=="" (
  "%BIN%" --install --server %1 --token %2
) else (
  "%BIN%" --install
)
