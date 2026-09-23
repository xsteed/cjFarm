#!/usr/bin/env bash
# 自助升级门店打印代理(Linux):从云端下载最新版本并替换自身。
# 用法: sudo ./upgrade.sh    (前提:服务端已部署新产物且管理后台已配置「代理最新版本号」)
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN=""
for f in "$DIR"/print-agent "$DIR"/print-agent-linux-*; do
  case "$f" in
    *.state|*.log|*.sh|*.md|*.example|*.plist|*.service|*.xml) continue ;;
  esac
  if [ -f "$f" ] && [ -x "$f" ]; then BIN="$f"; break; fi
done
if [ -z "$BIN" ]; then
  echo "[错误] 未找到代理产物(print-agent 或 print-agent-linux-*),请先把它放到本脚本同目录"
  exit 1
fi

sudo "$BIN" --upgrade
echo ""
echo "[提示] 升级完成后执行 sudo systemctl restart print-agent 让新版本生效"
