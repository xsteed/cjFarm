#!/usr/bin/env bash
# 停止门店打印代理(macOS):卸载本次 launchd 任务并终止进程;
# 自启动保留(plist 仍在 ~/Library/LaunchAgents 且未被禁用),重登录/重启后自动恢复。
#
# 为什么不用 launchctl stop:plist 里 KeepAlive=true,launchctl stop 只是发一次停止
# 信号,launchd 会立刻把进程重新拉起(几秒内新 PID),看起来就像「停不掉」。
# 真正停止必须卸载任务(bootout / unload);卸载不等于禁用,重启后依然自动启动。
#
# 除了作业进程,还要清「残留实例」(手工启动的、或被 launchd 收养的孤儿):
# 它们不受作业注销影响,而且会和自启动的那个抢同一批云端任务 —— 表现为**小票被打两遍**。
set -u

LABEL="com.cjfarm.print-agent"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UID_NUM="$(id -u)"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_BIN="$HOME/Applications/PrintAgent.app/Contents/MacOS/print-agent"

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

# 是否还有残留实例。
#
# 匹配规则刻意用「**行首**的绝对路径」,而不是任意位置子串:
#   - 行首锚定可避免误伤「命令行里恰好含该路径」的其它进程 —— 从终端敲一条含该路径的
#     长命令时,子串匹配的 pkill 可能把那个 shell 一起杀掉(实测风险);
#   - 被 --install 注册的自启动用的就是绝对路径,锚定后仍能覆盖。
# 代价:手工用相对路径(`./print-agent-darwin-arm64`)启动的实例不在此列 —— 那种情况
# 由操作者自己 Ctrl+C 即可。
leftover() {
  pgrep -f "^$APP_BIN" >/dev/null 2>&1 && return 0
  pgrep -f "^$DIR/print-agent" >/dev/null 2>&1 && return 0
  return 1
}

# 清残留实例:先 SIGTERM 让它优雅退出,等不到再升级 SIGKILL。
#
# 为什么要等:代理的取单是长轮询(最长 hold 30 秒),收到退出信号也要等当前这一轮
# 结束才会走到退出分支 —— 「发完信号立刻断言停了」是不成立的。而门店要的是
# 「敲了 stop 就是停了」,所以这里等一个有界时间,超时才动手硬杀。
# (优雅退出通常 3~4 秒内完成;只有正卡在长轮询上时才接近 30 秒。)
clean_leftovers() {
  pkill -f "^$APP_BIN" >/dev/null 2>&1
  pkill -f "^$DIR/print-agent" >/dev/null 2>&1
  local i
  for i in $(seq 1 30); do
    leftover || return 0
    sleep 1
  done
  echo "[提示] 代理未在 30 秒内退出(可能正卡在取单长轮询),强制结束"
  pkill -KILL -f "^$APP_BIN" >/dev/null 2>&1
  pkill -KILL -f "^$DIR/print-agent" >/dev/null 2>&1
  sleep 1
  return 0
}

PID="$(job_pid)"
if [ -z "$PID" ] && ! launchctl list "$LABEL" >/dev/null 2>&1; then
  # 任务没加载,但可能仍有残留实例在跑。这里必须一并清掉再报告,否则门店会得到
  # 「我明明跑过 stop.sh,怎么还有进程」的错误印象(实测踩到)。
  if leftover; then
    clean_leftovers
    echo "[已停止] launchd 未加载 $LABEL,但清掉了残留实例"
  else
    echo "[无需停止] launchd 未加载 $LABEL,也没有残留进程"
  fi
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

# 无条件清残留。**不能**用「注销前读到的 PID」来判断要不要清:恰恰注销成功后那个 PID
# 就失效了,kill -0 返回失败,于是最该清的那一类(孤儿实例)被跳过(实测就是这么漏的)。
clean_leftovers

# 确认未被标记禁用,保证「重启后自动恢复」:enable 只清禁用标记,不会拉起任务
launchctl enable "gui/$UID_NUM/$LABEL" >/dev/null 2>&1

# 进程被 KeepAlive 托管时退出需要一点时间(尤其 caffeinate 包装的情况),轮询等待。
# 判定要同时满足「作业没进程了」与「没有残留实例」—— 只看前者会把孤儿算成成功。
OK=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if [ -z "$(job_pid)" ] && ! leftover; then
    OK=1
    break
  fi
  sleep 1
done

if [ "$OK" -eq 1 ]; then
  echo "[已停止] 自启动保留(重启或重新登录后自动恢复;立即启动执行 ./start.sh)"
else
  echo "[停止失败] 仍有进程在运行,请重试或手工执行:"
  echo "  launchctl bootout gui/$UID_NUM/$LABEL"
  echo "  pkill -f \"$APP_BIN\""
  exit 1
fi
