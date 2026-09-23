#!/usr/bin/env bash
# 卸载门店打印代理(Linux):停进程 + 停用并删除 systemd 单元 + 清理运行/升级残留文件。
#
# 用法:
#   sudo ./uninstall.sh         常规卸载:移除开机自启,保留产物与 agent.env(重装不用重填令牌)
#   sudo ./uninstall.sh --all   彻底卸载:额外删除代理产物与 agent.env(本目录下运维脚本保留)
#
# 与 stop.sh 的区别:stop.sh 只停本次运行(自启动保留,重启后自动恢复);
# 本脚本移除 systemd 单元本身,不再开机拉起。
set -u

UNIT="print-agent"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ALL=0
case "${1:-}" in
  --all) ALL=1 ;;
  "")    ;;
  *)     echo "[错误] 未知参数: $1(只支持 --all)"; exit 1 ;;
esac

# root 下的命令前缀:已是 root 则不带 sudo
SUDO="sudo"
[ "$(id -u)" -eq 0 ] && SUDO=""

# 定位同目录产物:优先本机架构版本(aarch64 → arm64,x86_64 → amd64)。
PREF=""
case "$(uname -m)" in
  aarch64|arm64) PREF="$DIR/print-agent-linux-arm64" ;;
  x86_64|amd64)  PREF="$DIR/print-agent-linux-amd64" ;;
esac

BIN=""
if [ -n "$PREF" ] && [ -f "$PREF" ]; then
  BIN="$PREF"
else
  for f in "$DIR"/print-agent "$DIR"/print-agent-linux-*; do
    case "$f" in
      *.state|*.log|*.old|*.tmp|*.sh|*.md|*.example|*.plist|*.service|*.xml) continue ;;
    esac
    if [ -f "$f" ]; then BIN="$f"; break; fi
  done
fi
if [ -n "$BIN" ] && [ ! -x "$BIN" ]; then
  chmod +x "$BIN" 2>/dev/null
fi

# 1) 停进程(systemd 单元 Restart=always,要先停用单元本身)
$SUDO systemctl stop "$UNIT" >/dev/null 2>&1
$SUDO pkill -f "$DIR/print-agent" >/dev/null 2>&1

# 2) 移除自启动:内置命令停用单元、删除 unit 文件并 daemon-reload;重复执行不报错。
#    产物已不在时用等价 systemctl/rm 兜底,避免 unit 一直留在 /etc/systemd/system。
if [ -n "$BIN" ]; then
  $SUDO "$BIN" --uninstall
else
  echo "[提示] 未找到代理产物(print-agent 或 print-agent-linux-*),直接清理残留单元"
  $SUDO systemctl disable --now "$UNIT" >/dev/null 2>&1
  $SUDO rm -f "/etc/systemd/system/$UNIT.service"
  $SUDO systemctl daemon-reload >/dev/null 2>&1
fi

# 3) 清理运行时文件与升级残留(日志、幂等状态文件、升级遗留的 .old/.tmp)
rm -f "$DIR/print-agent.log" "$DIR/print-agent.state" "$DIR"/*.old "$DIR"/*.tmp 2>/dev/null \
  || $SUDO rm -f "$DIR/print-agent.log" "$DIR/print-agent.state" "$DIR"/*.old "$DIR"/*.tmp 2>/dev/null

# 4) --all 才动产物与配置
if [ "$ALL" -eq 1 ]; then
  rm -f "$DIR/print-agent" "$DIR"/print-agent-linux-* "$DIR/agent.env" 2>/dev/null \
    || $SUDO rm -f "$DIR/print-agent" "$DIR"/print-agent-linux-* "$DIR/agent.env" 2>/dev/null
  echo "[已彻底卸载] 自启动已移除,产物与 agent.env 已删除"
  echo "             重装需重新执行 sudo ./install.sh 并重新填写令牌"
else
  echo "[已卸载] 开机自启已移除,代理已停止;运行文件已清理"
  echo "         产物与 agent.env 已保留(重装执行 sudo ./install.sh 即可,不必重填令牌)"
  echo "         想彻底清除: sudo ./uninstall.sh --all"
fi
echo "         想连同本目录一起删除: sudo rm -rf \"$DIR\""
