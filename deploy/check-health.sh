#!/usr/bin/env bash
# ============================================================================
# 扫码点餐管理系统 - 后端服务健康检查脚本
#
# 用途: 判断后端服务是否在线。适合部署后自检、cron 定时探测、监控系统调用。
#
# 用法:
#   bash deploy/check-health.sh                     # 检查本机 8080
#   bash deploy/check-health.sh -p 9000             # 指定端口
#   bash deploy/check-health.sh -H 10.0.0.5         # 指定主机(默认 127.0.0.1)
#   bash deploy/check-health.sh --json              # JSON 输出(便于监控解析)
#   bash deploy/check-health.sh --quiet             # 静默, 仅以退出码表达结果
#   bash deploy/check-health.sh -u http://IP:8080/api/customer/config
#
# 退出码:
#   0  服务在线(健康接口返回 HTTP 200)
#   1  服务离线 / 健康接口异常
#   2  用法错误或缺少依赖(curl)
#
# 判定口径: 以「公开健康接口是否返回 200」为准(无需鉴权、不写数据);
#           systemd 状态与端口监听仅作辅助展示, 便于定位问题。
# ============================================================================
set -uo pipefail

SERVICE_NAME="${SERVICE_NAME:-dining-backend}"
HOST="127.0.0.1"
PORT=""
HEALTH_PATH="${HEALTH_PATH:-/api/customer/config}"
TIMEOUT="${TIMEOUT:-3}"
RETRIES="${RETRIES:-3}"
URL=""
MODE="text"   # text | json | quiet

die() { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 2; }

usage() {
  cat <<'EOF'
用法: bash check-health.sh [选项]

选项:
  -H, --host <host>   后端主机, 默认 127.0.0.1
  -p, --port <port>   后端端口, 默认 8080(或自动读取 systemd 服务单元)
  -u, --url <url>     直接指定完整健康检查 URL(优先级最高)
      --json          以 JSON 输出结果, 便于监控系统解析
  -q, --quiet         静默模式, 不输出内容, 仅用退出码表达结果
  -h, --help          显示本帮助

退出码: 0=在线  1=离线/异常  2=用法错误

环境变量: TIMEOUT(单次超时秒, 默认3) RETRIES(重试次数, 默认3) SERVICE_NAME(默认 dining-backend)
EOF
}

# ---------------------------- 解析参数 ----------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    -H|--host)  [ $# -ge 2 ] || die "缺少参数值: $1"; HOST="$2"; shift 2 ;;
    -p|--port)  [ $# -ge 2 ] || die "缺少参数值: $1"; PORT="$2"; shift 2 ;;
    -u|--url)   [ $# -ge 2 ] || die "缺少参数值: $1"; URL="$2";  shift 2 ;;
    --json)     MODE="json";  shift ;;
    -q|--quiet) MODE="quiet"; shift ;;
    -h|--help)  usage; exit 0 ;;
    *) die "未知参数: $1 (用 -h 查看用法)" ;;
  esac
done

case "$PORT" in
  ""|*[!0-9]*) [ -z "$PORT" ] || die "端口必须是数字: $PORT" ;;
esac

command -v curl >/dev/null 2>&1 || die "缺少 curl, 无法进行 HTTP 探测"

# ---------------------------- 组装探测地址 ----------------------------
if [ -z "$URL" ]; then
  if [ -z "$PORT" ]; then
    # 优先从 systemd 服务单元里读端口(与 deploy.sh 安装的配置保持一致);
    # 锚定行首, 避免命中单元里被注释掉的示例行(# Environment=PORT=8080)。
    PORT="$(grep -oE '^Environment=PORT=[0-9]+' "/etc/systemd/system/${SERVICE_NAME}.service" 2>/dev/null | head -1 | cut -d= -f3 || true)"
    PORT="${PORT:-8080}"
  fi
  URL="http://${HOST}:${PORT}${HEALTH_PATH}"
fi

# ---------------------------- HTTP 探测(带重试) ----------------------------
HTTP_CODE="000"
LATENCY="0"
ATTEMPT=0
while [ "$ATTEMPT" -lt "$RETRIES" ]; do
  ATTEMPT=$((ATTEMPT + 1))
  RESULT="$(curl -s -o /dev/null -w '%{http_code} %{time_total}' --max-time "$TIMEOUT" "$URL" 2>/dev/null || true)"
  HTTP_CODE="${RESULT%% *}"
  LATENCY="${RESULT##* }"
  [ -n "$HTTP_CODE" ] || HTTP_CODE="000"
  # curl 彻底失败时可能只输出 "000"(无第二列), 此时 LATENCY 会等于 HTTP_CODE
  [ -n "$LATENCY" ] && [ "$LATENCY" != "$HTTP_CODE" ] || LATENCY="0"
  [ "$HTTP_CODE" = "200" ] && break
  [ "$ATTEMPT" -lt "$RETRIES" ] && sleep 1
done

LATENCY_MS="$(awk -v t="$LATENCY" 'BEGIN{printf "%.0f", t*1000}' 2>/dev/null || echo 0)"
[ -n "$LATENCY_MS" ] || LATENCY_MS=0

# ---------------------------- 辅助信息: systemd 状态 ----------------------------
SVC_STATE="n/a"
if command -v systemctl >/dev/null 2>&1; then
  if [ -f "/etc/systemd/system/${SERVICE_NAME}.service" ] || systemctl list-unit-files "${SERVICE_NAME}.service" >/dev/null 2>&1; then
    SVC_STATE="$(systemctl is-active "$SERVICE_NAME" 2>/dev/null | head -1 || true)"
    SVC_STATE="${SVC_STATE:-unknown}"
  else
    SVC_STATE="not-installed"
  fi
fi

# ---------------------------- 辅助信息: 端口监听 ----------------------------
# 先把探测结果整段读进变量, 再在变量上匹配。
# 注意: 不可写成 `netstat | grep -q` —— grep -q 命中即退出会让上游收到 SIGPIPE,
#       在 set -o pipefail 下整条管道被判为失败, 导致明明在监听却报「未监听到」。
PORT_LISTEN="n/a"
if [ -n "$PORT" ]; then
  LISTEN_OUT=""
  if command -v ss >/dev/null 2>&1; then
    LISTEN_OUT="$(ss -ltn 2>/dev/null || true)"
  fi
  if [ -z "$LISTEN_OUT" ] && command -v netstat >/dev/null 2>&1; then
    LISTEN_OUT="$(netstat -an 2>/dev/null || true)"
  fi
  if [ -n "$LISTEN_OUT" ]; then
    PORT_LISTEN="no"
    if printf '%s\n' "$LISTEN_OUT" | grep -E "[.:]${PORT}[[:space:]]" >/dev/null 2>&1; then
      PORT_LISTEN="yes"
    fi
  fi
fi

# ---------------------------- 判定与输出 ----------------------------
if [ "$HTTP_CODE" = "200" ]; then
  STATUS="up"; EXIT_CODE=0
else
  STATUS="down"; EXIT_CODE=1
fi

if [ "$MODE" = "quiet" ]; then
  exit "$EXIT_CODE"
fi

if [ "$MODE" = "json" ]; then
  printf '{"status":"%s","url":"%s","http_code":%s,"latency_ms":%s,"service":"%s","service_state":"%s","port":"%s","port_listening":"%s","attempts":%s}\n' \
    "$STATUS" "$URL" "$HTTP_CODE" "$LATENCY_MS" "$SERVICE_NAME" "$SVC_STATE" "${PORT:-}" "$PORT_LISTEN" "$ATTEMPT"
  exit "$EXIT_CODE"
fi

if [ "$STATUS" = "up" ]; then
  printf '\033[1;32m[check]\033[0m 后端服务在线\n'
else
  printf '\033[1;31m[check]\033[0m 后端服务离线\n'
fi
printf '  健康接口  : %s\n' "$URL"
printf '  探测结果  : HTTP %s  延迟 %sms  尝试 %s/%s 次\n' "$HTTP_CODE" "$LATENCY_MS" "$ATTEMPT" "$RETRIES"
if [ -n "$PORT" ]; then
  if [ "$PORT_LISTEN" = "yes" ]; then
    printf '  监听端口  : %s (LISTEN)\n' "$PORT"
  else
    printf '  监听端口  : %s (未监听到)\n' "$PORT"
  fi
fi
printf '  systemd   : %s -> %s\n' "$SERVICE_NAME" "$SVC_STATE"

if [ "$STATUS" = "down" ]; then
  printf '\n排查建议:\n'
  printf '  journalctl -u %s -n 50 --no-pager     # 看启动日志/报错\n' "$SERVICE_NAME"
  printf '  systemctl status %s                   # 看服务状态\n' "$SERVICE_NAME"
fi

exit "$EXIT_CODE"
