#!/usr/bin/env bash
# 重启后端服务并做一次健康检查。**改完 config.yaml 必须执行这个** ——
# 配置只在进程启动时读一次, 不重启不生效(最常见的「改了配置没反应」)。
# 用法: sudo bash op/restart.sh
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

if ! service_installed; then
  die "找不到 systemd 单元 $SERVICE_NAME; 安装: sudo bash deploy/op/install.sh"
fi

log "重启 $SERVICE_NAME ..."
if restart_service; then
  ok "已重启, 健康检查通过: $(health_url)"
else
  warn "重启后健康检查未通过(当前状态: $(service_state))"
  printf '  排查: journalctl -u %s -n 100 --no-pager\n' "$SERVICE_NAME"
  printf '  配置: %s(改完必须重启才生效)\n' "$CONFIG"
  exit 1
fi
