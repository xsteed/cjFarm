#!/usr/bin/env bash
# 启动后端服务(Linux): systemctl 启动; 未安装时给出安装引导。
# 用法: sudo bash op/start.sh
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

if ! service_installed; then
  printf '[未安装] 找不到 systemd 单元 %s\n' "$SERVICE_NAME"
  echo '  请先安装(把发布包解压到服务器后, 在包根目录执行):'
  echo '    sudo bash deploy/op/install.sh'
  exit 1
fi

if systemctl start "$SERVICE_NAME"; then
  ok "已启动 $SERVICE_NAME"
  if health_ok; then
    ok "健康检查通过: $(health_url)"
  else
    warn "已启动但健康检查未通过; 排查: journalctl -u $SERVICE_NAME -n 50 --no-pager"
    exit 1
  fi
else
  die "启动失败。无权限请用 sudo; 详见 journalctl -u $SERVICE_NAME -n 50 --no-pager"
fi
