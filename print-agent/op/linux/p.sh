#!/usr/bin/env bash
# 打印门店打印代理运行状态(Linux):
#   进程状态 + 最近 15 行日志 + 云端连通自检提示。
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG="${PRINT_AGENT_LOG:-$DIR/print-agent.log}"

echo "== 进程状态 =="
ACT="$(systemctl is-active print-agent 2>/dev/null || echo inactive)"
if [ "$ACT" = "active" ]; then
  echo "运行中(systemctl active)"
else
  echo "未运行"
fi

echo ""
echo "== 最近日志(末 15 行,文件: $LOG) =="
if [ -f "$LOG" ]; then
  tail -n 15 "$LOG"
else
  echo "<尚无日志>(代理未启动或未配置 PRINT_AGENT_LOG)"
fi

echo ""
echo "== 云端连通自检 =="
echo "  手动执行: sudo ./print-agent --once"
