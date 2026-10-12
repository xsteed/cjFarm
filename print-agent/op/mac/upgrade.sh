#!/usr/bin/env bash
# 自助升级门店打印代理(macOS):从云端下载最新版本并替换自身。
# 用法: ./upgrade.sh    (前提:服务端已部署新产物且管理后台已配置「代理最新版本号」)
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --upgrade 必须先读到 agent.env(云端地址 + 代理令牌),而「配置在哪」取决于程序在哪
# (见 config.go 的 dataDir):装入 App 后 agent.env 落在 Application Support,
# 解压目录里根本没有这个文件。因此若本目录没有 agent.env、而已安装实例有,
# 就把数据目录显式指过去 —— 否则直接跑本目录产物会报「缺少 --server(云端地址)」,
# 门店会以为令牌丢了,而实际上配置一直都在。
APP_DATA_DIR="$HOME/Library/Application Support/PrintAgent"
if [ ! -f "$DIR/agent.env" ] && [ -f "$APP_DATA_DIR/agent.env" ]; then
  export PRINT_AGENT_DATA_DIR="$APP_DATA_DIR"
  echo "[信息] 本目录无 agent.env,改用已安装实例的配置与状态目录:$APP_DATA_DIR"
fi

BIN=""
for f in "$DIR"/print-agent "$DIR"/print-agent-darwin-*; do
  case "$f" in
    *.state|*.log|*.sh|*.md|*.example|*.plist|*.service|*.xml) continue ;;
  esac
  if [ -f "$f" ] && [ -x "$f" ]; then BIN="$f"; break; fi
done
if [ -z "$BIN" ]; then
  echo "[错误] 未找到代理产物(print-agent 或 print-agent-darwin-*),请先把它放到本脚本同目录"
  exit 1
fi

"$BIN" --upgrade
echo ""
echo "[提示] 升级完成后执行 ./start.sh 让新版本生效"
