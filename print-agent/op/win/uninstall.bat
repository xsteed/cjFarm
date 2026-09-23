@echo off
REM 卸载门店打印代理(Windows):停进程 + 删除任务计划自启动 + 清理日志/状态/升级残留文件。
REM
REM 用法:
REM   uninstall.bat          常规卸载:移除开机自启,保留 exe 与 agent.env(重装不用重填令牌)
REM   uninstall.bat --all    彻底卸载:额外删除代理 exe 与 agent.env(本目录下运维脚本保留)
REM
REM 与 stop.bat 的区别:stop.bat 只停本次运行(自启动保留,重启后自动恢复);
REM 本脚本删除任务计划本身,不再开机拉起。
setlocal
set ALL=0
if /i "%1"=="--all" set ALL=1

REM 统一以本脚本所在目录为工作目录:双击运行时本来就是,命令行跨目录调用也能定位到文件
cd /d "%~dp0"

set BIN=
if exist "print-agent.exe" set BIN=print-agent.exe
if not defined BIN if exist "print-agent-windows-amd64.exe" set BIN=print-agent-windows-amd64.exe

REM 1) 结束正在运行的实例:先停任务计划里的实例,再清可能手动双击启动的进程
schtasks /end /tn "DiningPrintAgent" >nul 2>&1
taskkill /f /im "print-agent.exe" /t >nul 2>&1
taskkill /f /im "print-agent-windows-amd64.exe" /t >nul 2>&1

REM 2) 删除任务计划自启动;产物已不在时退化为直接删任务
if defined BIN goto :uninstall_bin
echo [提示] 未找到代理产物(print-agent.exe / print-agent-windows-amd64.exe^),直接删除任务计划
schtasks /delete /tn "DiningPrintAgent" /f >nul 2>&1
goto :cleanup

:uninstall_bin
"%BIN%" --uninstall

:cleanup
REM 3) 清理运行时文件与升级残留(日志、幂等状态文件、升级遗留的 .old/.tmp^)
del /f /q print-agent.log print-agent.state *.old *.tmp >nul 2>&1

if not "%ALL%"=="1" goto :done
REM 4) --all 才动产物与配置
if defined BIN del /f /q "%BIN%" >nul 2>&1
del /f /q print-agent.exe print-agent-windows-amd64.exe agent.env >nul 2>&1

:done
if "%ALL%"=="1" goto :done_all
echo [已卸载] 开机自启已移除,代理已停止;运行文件已清理
echo           exe 与 agent.env 已保留(重装双击 install.bat 即可,不必重填令牌^)
echo           想彻底清除: uninstall.bat --all
goto :tip

:done_all
echo [已彻底卸载] 自启动已移除,exe 与 agent.env 已删除
echo              重装需重新双击 install.bat 并重新填写令牌

:tip
echo          想把这个文件夹一起删掉(运维脚本也会删掉^): rmdir /s /q "%CD%"
