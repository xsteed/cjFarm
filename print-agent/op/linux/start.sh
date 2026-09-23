#!/usr/bin/env bash
# 启动门店打印代理(Linux):systemctl 启动;未安装时给出安装引导。
set -u
if ! systemctl list-unit-files 2>/dev/null | grep -q "^print-agent"; then
  echo "[未安装] 找不到 systemd 单元 print-agent"
  echo "  请先安装(一条命令,自动配置 + 注册自启动):"
  echo "    sudo ./print-agent --install --server https://你的管理后台域名 --token 你的令牌"
  exit 1
fi
systemctl start print-agent && echo "[已启动] systemctl 已启动 print-agent" \
  || echo "[失败] 无权限?请改用: sudo $(dirname "$0")/start.sh"
