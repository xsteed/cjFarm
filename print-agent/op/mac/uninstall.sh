#!/usr/bin/env bash
# 卸载门店打印代理(macOS):停进程 + 移除 launchd 自启动 + 清理运行/升级残留文件。
#
# 用法:
#   ./uninstall.sh         常规卸载:移除开机自启,保留产物与 agent.env(重装不用重填令牌)
#   ./uninstall.sh --all   彻底卸载:额外删除代理产物与 agent.env(本目录下运维脚本保留)
#
# 与 stop.sh 的区别:stop.sh 只停本次运行(自启动保留,重登录/重启后自动恢复);
# 本脚本移除自启动本身,不再开机拉起。
set -u

LABEL="com.cjfarm.print-agent"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UID_NUM="$(id -u)"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ALL=0
case "${1:-}" in
  --all) ALL=1 ;;
  "")    ;;
  *)     echo "[错误] 未知参数: $1(只支持 --all)"; exit 1 ;;
esac

# 定位同目录产物:优先本机芯片版本(Apple Silicon → arm64,Intel → amd64)。
# 产物已被删掉时 BIN 为空,仍要尽力清理自启动残留。
PREF=""
case "$(uname -m)" in
  arm64)  PREF="$DIR/print-agent-darwin-arm64" ;;
  x86_64) PREF="$DIR/print-agent-darwin-amd64" ;;
esac

BIN=""
if [ -n "$PREF" ] && [ -f "$PREF" ]; then
  BIN="$PREF"
else
  for f in "$DIR"/print-agent "$DIR"/print-agent-darwin-*; do
    case "$f" in
      *.state|*.log|*.old|*.tmp|*.sh|*.md|*.example|*.plist|*.service|*.xml) continue ;;
    esac
    if [ -f "$f" ]; then BIN="$f"; break; fi
  done
fi
if [ -n "$BIN" ] && [ ! -x "$BIN" ]; then
  chmod +x "$BIN" 2>/dev/null
fi

# 1) 停进程:plist 里 KeepAlive=true,直接 kill 会被 launchd 立刻拉起,必须先把任务注销。
#    新版用 bootout(不带 disabled 覆盖),旧版/兜底用 unload;两者都不会写禁用标记。
launchctl bootout "gui/$UID_NUM/$LABEL" >/dev/null 2>&1 \
  || launchctl unload "$PLIST" >/dev/null 2>&1
pkill -f "$DIR/print-agent" >/dev/null 2>&1
pkill -f "$HOME/Applications/PrintAgent.app/Contents/MacOS/print-agent" >/dev/null 2>&1

# 2) 移除自启动:内置命令负责注销任务并删除 plist;重复执行不报错。
#    产物已不在时用 rm 兜底,否则 plist 会一直留在 ~/Library/LaunchAgents。
APP="$HOME/Applications/PrintAgent.app"
DATA_DIR="${PRINT_AGENT_DATA_DIR:-$HOME/Library/Application Support/PrintAgent}"

if [ -n "$BIN" ]; then
  "$BIN" --uninstall   # 内部会一并移除 $APP,agent.env 保留在数据目录
else
  echo "[提示] 未找到代理产物(print-agent 或 print-agent-darwin-*),直接清理残留 plist 与 App"
  rm -f "$PLIST"
  rm -rf "$APP"
fi
# 清掉可能的禁用标记,避免将来同 Label 重新安装被 launchd 判定为 disabled
launchctl enable "gui/$UID_NUM/$LABEL" >/dev/null 2>&1

# 3) 等待进程退出(KeepAlive 托管时退出需要一点时间)
for _ in 1 2 3 4 5 6 7 8 9 10; do
  pgrep -f "$DIR/print-agent" >/dev/null 2>&1 || break
  sleep 1
done

# 4) 清理运行时文件与升级残留(日志、幂等状态文件、升级遗留的 .old/.tmp)。
#    两个位置都清:解压目录,以及 --install 装成 App 后的数据目录
#    (~/Library/Application Support/PrintAgent —— 数据刻意不放进 App,以免破坏代码签名)。
rm -f "$DIR/print-agent.log" "$DIR/print-agent.state" "$DIR"/*.old "$DIR"/*.tmp 2>/dev/null
rm -f "$DATA_DIR/print-agent.log" "$DATA_DIR/print-agent.state" "$DATA_DIR"/*.old "$DATA_DIR"/*.tmp 2>/dev/null
rm -rf "$APP"

# 5) --all 才动产物与配置
if [ "$ALL" -eq 1 ]; then
  rm -f "$DIR/print-agent" "$DIR"/print-agent-darwin-* "$DIR/agent.env" 2>/dev/null
  rm -rf "$DATA_DIR"
  echo "[已彻底卸载] 自启动已移除,产物、App 与 agent.env 已删除"
  echo "             重装需重新执行 ./install.sh 并重新填写令牌"
else
  echo "[已卸载] 开机自启已移除,代理已停止;App 与运行文件已清理"
  echo "         agent.env 已保留在 $DATA_DIR(重装执行 ./install.sh 即可,不必重填令牌)"
  echo "         想彻底清除: ./uninstall.sh --all"
fi
echo "         想连同本目录一起删除: rm -rf \"$DIR\""

# 合盖睡眠开关是系统级设置,不是本程序的注册项:卸载时**不该**擅自改回去(这台机器
# 可能另有用途),但必须说清楚它还开着 —— 否则这台 Mac 会一直不睡、白白耗电发热。
if ioreg -r -c IOPMrootDomain -d 1 2>/dev/null | grep -q '"SleepDisabled" = Yes'; then
  echo "         [提示] 合盖睡眠开关仍是关闭状态;要恢复「合盖即睡」: sudo pmset -a disablesleep 0"
fi
