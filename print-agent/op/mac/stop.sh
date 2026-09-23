#!/usr/bin/env bash
# 停止门店打印代理(macOS):卸载本次 launchd 任务并终止进程;
# 自启动保留(plist 仍在 ~/Library/LaunchAgents 且未被禁用),重登录/重启后自动恢复。
#
# 为什么不用 launchctl stop:plist 里 KeepAlive=true,launchctl stop 只是发一次停止
# 信号,launchd 会立刻把进程重新拉起(几秒内新 PID),看起来就像「停不掉」。
# 真正停止必须卸载任务(bootout / unload);卸载不等于禁用,重启后依然自动启动。
set -u

LABEL="com.cjfarm.print-agent"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UID_NUM="$(id -u)"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 进程 PID(无进程时返回空)。
# launchd 输出格式跨版本不一致:不带 Label 的 list 是「PID 退出码 Label」三列,
# 带 Label/String 的查询在新款 macOS 上返回 plist 风格键值块,这里两手准备。
job_pid() {
  local pid
  pid="$(launchctl list 2>/dev/null | awk -v l="$LABEL" '$3==l && $1 != "-" {print $1; exit}')"
  if [ -z "$pid" ]; then
    pid="$(launchctl print "gui/$UID_NUM/$LABEL" 2>/dev/null | sed -n 's/.*"PID" = \([0-9]*\).*/\1/p' | head -n 1)"
  fi
  printf '%s' "$pid"
}

PID="$(job_pid)"
if [ -z "$PID" ] && ! launchctl list "$LABEL" >/dev/null 2>&1; then
  echo "[无需停止] launchd 未加载 $LABEL"
  exit 0
fi

STOPPED=0
# 新版 API:bootout 把任务从本次会话注销(不影响 plist 文件)
if launchctl bootout "gui/$UID_NUM/$LABEL" >/dev/null 2>&1; then
  STOPPED=1
fi
# 旧版/兜底:unload(不带 -w,不会写禁用标记)
if [ "$STOPPED" -eq 0 ] && [ -f "$PLIST" ]; then
  launchctl unload "$PLIST" >/dev/null 2>&1 && STOPPED=1
fi
# 卸载过程中 launchd 会终止代理进程;这里再清一次可能的残留(如手动绕过 launchd 启动的实例)
# 两个位置都要清:解压目录里的产物,以及 --install 部署出来的 App 内的那份拷贝。
if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
  pkill -f "$DIR/print-agent" >/dev/null 2>&1
  pkill -f "$HOME/Applications/PrintAgent.app/Contents/MacOS/print-agent" >/dev/null 2>&1
fi

# 确认未被标记禁用,保证「重启后自动恢复」:enable 只清禁用标记,不会拉起任务
launchctl enable "gui/$UID_NUM/$LABEL" >/dev/null 2>&1

# 进程被 KeepAlive 托管时退出需要一点时间(尤其 caffeinate 包装的情况),轮询等待。
# 判定成功只要求「没有进程」:任务记录可能短暂残留在 launchctl list 里。
OK=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ -z "$(job_pid)" ] && OK=1 && break
  sleep 1
done

if [ "$OK" -eq 1 ]; then
  echo "[已停止] 自启动保留(重启或重新登录后自动恢复;立即启动执行 ./start.sh)"
else
  echo "[停止失败] PID $(job_pid) 仍在运行,请重试或执行:"
  echo "  launchctl bootout gui/$UID_NUM/$LABEL"
  exit 1
fi
