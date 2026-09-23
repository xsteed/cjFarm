#!/usr/bin/env bash
# 停止门店打印代理(Linux):只停止运行实例,自启动保留,重启后自动恢复。
set -u
systemctl stop print-agent && echo "[已停止] 自启动保留(重启后自动恢复)" \
  || echo "[失败] 无权限?请改用: sudo $(dirname "$0")/stop.sh"
