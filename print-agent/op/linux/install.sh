#!/usr/bin/env bash
# 安装门店打印代理(Linux):写配置 + 注册 systemd 自启动(需要 root)。
# 用法: sudo ./install.sh [管理后台地址] [代理令牌]   (省略参数则交互式询问)
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 优先按本机芯片选产物(aarch64 → arm64,x86_64 → amd64),避免字母序误选。
PREF=""
case "$(uname -m)" in
  aarch64|arm64) PREF="$DIR/print-agent-linux-arm64" ;;
  x86_64|amd64)  PREF="$DIR/print-agent-linux-amd64" ;;
esac

BIN=""
if [ -n "$PREF" ] && [ -f "$PREF" ] && [ -x "$PREF" ]; then
  BIN="$PREF"
else
  for f in "$DIR"/print-agent "$DIR"/print-agent-linux-*; do
    case "$f" in
      *.state|*.log|*.sh|*.md|*.example|*.plist|*.service|*.xml) continue ;;
    esac
    if [ -f "$f" ] && [ -x "$f" ]; then BIN="$f"; break; fi
  done
fi
if [ -z "$BIN" ]; then
  echo "[错误] 未找到代理产物(print-agent 或 print-agent-linux-*),请先把它放到本脚本同目录"
  exit 1
fi

if [ $# -ge 2 ]; then
  sudo "$BIN" --install --server "$1" --token "$2"
else
  sudo "$BIN" --install   # 交互式询问地址与令牌
fi
