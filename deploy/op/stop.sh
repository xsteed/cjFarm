#!/usr/bin/env bash
# 停止后端服务(Linux): 只停本次运行, 开机自启保留(重启后自动恢复)。
# 用法: sudo bash op/stop.sh        # 想彻底不再自启请用 uninstall.sh
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

if ! service_installed; then
  die "找不到 systemd 单元 $SERVICE_NAME(未安装?)"
fi

if systemctl stop "$SERVICE_NAME"; then
  ok "已停止 $SERVICE_NAME(自启动保留, 重启后自动恢复)"
else
  die "停止失败。无权限请用 sudo"
fi
