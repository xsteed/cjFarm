#!/usr/bin/env bash
#
# check-online.sh — 云端侧查询 print-agent 在线状态
#
# 干什么用的:
#   门店的 print-agent 是出站轮询,「是否在线」只有云端知道。本脚本跑在
#   部署后端的机器上(或任何能访问管理端接口的机器),用管理端账号登录后
#   调 GET /api/admin/printer/agent/info,输出代理在线/离线、最近心跳与队列积压。
#   判定口径与后端 agent.go 一致:最近心跳距当前 < 90 秒即在线。
#
# 用法:
#   ./check-online.sh --server https://dining.example.com --user admin --pass xxx
#   ./check-online.sh --token <已登录的 Bearer token>              # 免账号密码
#   CJ_FARM_SERVER=https://dining.example.com \
#   CJ_FARM_ADMIN_USER=admin CJ_FARM_ADMIN_PASS=xxx ./check-online.sh
#
# 可选参数:
#   --server URL    云端地址(默认取 $CJ_FARM_SERVER,再默认 http://localhost:8080)
#   --user  USER    管理端账号;--token 优先,二者给其一即可
#   --pass  PASS    管理端密码(不给时交互输入,不回显)
#   --token TOKEN   直接使用已登录的 Bearer token(跳过登录)
#   --insecure      跳过 TLS 证书校验(自签证书环境)
#   --json          只输出接口原始 JSON(调试/二次处理)
#   --timeout N     HTTP 超时秒数(默认 10)
#
# 退出码(可接监控/告警):
#   0  至少一台代理在线
#   1  已配置代理但全部离线(或从未收到心跳)
#   2  服务端尚未配置代理令牌
#   3  请求失败(网络/登录/权限),需人工介入
#
# 依赖:curl(必有)与 python3(解析 JSON 用;缺失时退化为原始 JSON 输出)。
# 注意:管理端登录接口有失败次数锁定,密码错误连续重试会临时锁 IP。
set -u

SERVER="${CJ_FARM_SERVER:-}"
USERNAME="${CJ_FARM_ADMIN_USER:-}"
PASSWORD="${CJ_FARM_ADMIN_PASS:-}"
TOKEN="${CJ_FARM_ADMIN_TOKEN:-}"
INSECURE=0
AS_JSON=0
TIMEOUT=10

usage() {
  cat <<'EOF'
用法:
  ./check-online.sh --server https://dining.example.com --user admin --pass xxx
  ./check-online.sh --token <已登录的 Bearer token>              # 免账号密码
  CJ_FARM_SERVER=https://dining.example.com \
  CJ_FARM_ADMIN_USER=admin CJ_FARM_ADMIN_PASS=xxx ./check-online.sh

可选参数:
  --server URL    云端地址(默认取 $CJ_FARM_SERVER,再默认 http://localhost:8080)
  --user  USER    管理端账号;--token 优先,二者给其一即可
  --pass  PASS    管理端密码(不给时交互输入,不回显)
  --token TOKEN   直接使用已登录的 Bearer token(跳过登录)
  --insecure      跳过 TLS 证书校验(自签证书环境)
  --json          只输出接口原始 JSON(调试/二次处理)
  --timeout N     HTTP 超时秒数(默认 10)

退出码(可接监控/告警):
  0  至少一台代理在线
  1  已配置代理但全部离线(或从未收到心跳)
  2  服务端尚未配置代理令牌
  3  请求失败(网络/登录/权限),需人工介入
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2 ;;
    --user)   USERNAME="$2"; shift 2 ;;
    --pass)   PASSWORD="$2"; shift 2 ;;
    --token)  TOKEN="$2"; shift 2 ;;
    --insecure) INSECURE=1; shift ;;
    --json)   AS_JSON=1; shift ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1(见脚本头部注释或 --help)"; exit 3 ;;
  esac
done

if [ -z "$SERVER" ]; then
  SERVER="http://localhost:8080"
fi
SERVER="${SERVER%/}"
case "$SERVER" in
  http://*|https://*) ;;
  *) echo "错误: --server 必须是 http(s):// 开头的地址,当前: $SERVER"; exit 3 ;;
esac

CURL_OPTS=( -sS --connect-timeout "$TIMEOUT" --max-time "$TIMEOUT" )
[ "$INSECURE" -eq 1 ] && CURL_OPTS+=( -k )

# fetch <method> <path> [json_body]: 返回 body 与 http_code 写进全局变量。
fetch() {
  local method="$1" path="$2" body="${3:-}"
  local args=( "${CURL_OPTS[@]}" -X "$method" -w '\n%{http_code}' )
  [ -n "$TOKEN" ] && args+=( -H "Authorization: Bearer $TOKEN" )
  if [ -n "$body" ]; then
    args+=( -H 'Content-Type: application/json' -d "$body" )
  fi
  local out
  out=$(curl "${args[@]}" "$SERVER$path" 2>/dev/null) || { echo "错误: 无法连接 $SERVER(网络/TLS 问题)"; exit 3; }
  HTTP_CODE=$(printf '%s' "$out" | tail -n1)
  HTTP_BODY=$(printf '%s' "$out" | sed '$d')
}

if [ -z "$TOKEN" ]; then
  if [ -z "$USERNAME" ]; then
    echo "错误: 未提供凭据。用 --token 传登录令牌,或用 --user/--pass(或环境变量 CJ_FARM_ADMIN_USER / CJ_FARM_ADMIN_PASS)。"
    exit 3
  fi
  if [ -z "$PASSWORD" ]; then
    printf '管理端密码(%s): ' "$USERNAME"
    IFS= read -rs PASSWORD || true
    echo
  fi

  if command -v python3 >/dev/null 2>&1; then
    LOGIN_BODY=$(printf '%s\n%s' "$USERNAME" "$PASSWORD" | python3 -c 'import sys,json
u=sys.stdin.readline().rstrip("\n"); p=sys.stdin.readline().rstrip("\n")
print(json.dumps({"username":u,"password":p}))')
  else
    LOGIN_BODY="{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}"
  fi
  fetch POST /api/auth/login "$LOGIN_BODY"
  if [ "$HTTP_CODE" != "200" ]; then
    printf '登录失败(HTTP %s): %s\n' "$HTTP_CODE" "$HTTP_BODY"
    exit 3
  fi
  # 从统一响应体 {code,msg,data:{token}} 中取出 token。
  if command -v python3 >/dev/null 2>&1; then
    TOKEN=$(printf '%s' "$HTTP_BODY" | python3 -c 'import sys,json
d=json.load(sys.stdin)
data=d.get("data") or {}
print(data.get("token") or "")' 2>/dev/null)
  else
    TOKEN=$(printf '%s' "$HTTP_BODY" | grep -o '"token":"[^"]*"' | head -n1 | sed 's/^"token":"//; s/"$//')
  fi
  if [ -z "$TOKEN" ]; then
    printf '登录失败: %s\n' "$HTTP_BODY"
    exit 3
  fi
fi

fetch GET /api/admin/printer/agent/info
if [ "$HTTP_CODE" != "200" ]; then
  printf '查询失败(HTTP %s): %s\n' "$HTTP_CODE" "$HTTP_BODY"
  echo "提示: 接口需要 printer:view 权限;若返回 401,说明 --token 已过期,重新用账号密码登录。"
  exit 3
fi

if [ "$AS_JSON" -eq 1 ]; then
  printf '%s\n' "$HTTP_BODY"
  # 退出码同样遵循约定,便于 --json 模式接脚本处理。
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$HTTP_BODY" | python3 -c 'import sys,json
info=(json.load(sys.stdin).get("data") or {})
if not info.get("configured"): sys.exit(2)
sys.exit(0 if info.get("online") else 1)'
    exit $?
  fi
  printf '%s' "$HTTP_BODY" | grep -q '"online":true' && exit 0 || exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  # 解析程序落临时文件:heredoc 若直接喂给 python3 - 会抢占 stdin,
  # 导致管道里的 JSON 进不来。JSON 走 stdin,程序走文件,互不干扰。
  PARSE_TMP=$(mktemp)
  trap 'rm -f "$PARSE_TMP"' EXIT
  cat > "$PARSE_TMP" <<'PY'
import json, sys, time

SERVER = sys.argv[1]
WINDOW = 90  # 与后端 agent.go 的 agentOnlineWindow 一致

def ago(ts):
    if not ts:
        return None
    try:
        t = time.mktime(time.strptime(ts, "%Y-%m-%d %H:%M:%S"))
        return int(time.time() - t)
    except ValueError:
        return None

info = json.load(sys.stdin).get("data") or {}
if not info.get("configured"):
    print("本地打印代理未配置:请在管理后台「系统配置 → 小票打印 → 打印代理」填写代理令牌")
    sys.exit(2)

online = bool(info.get("online"))
print("本地打印代理通道状态")
print("  整体在线: %s" % ("在线" if online else "离线"))
print("  最近心跳: %s" % (info.get("lastSeen") or "从未收到"))
pending = int(info.get("pending") or 0)
dead = int(info.get("dead") or 0)
print("  队列积压: %d 单(重试耗尽放弃 %d 单)" % (pending, dead))
oldest = int(info.get("oldestPendingSec") or 0)
if oldest > 0:
    print("  最老待打印单已等: %d 秒" % oldest)

agents = info.get("agents") or []
if agents:
    latest = info.get("latestAgentVersion") or ""
    print("  已签发代理 %d 台:" % len(agents))
    for a in agents:
        name = a.get("agentName") or ("代理 #%s" % a.get("agentId"))
        if a.get("status") != 1:
            state = "停用"
        else:
            d = ago(a.get("lastSeen") or "")
            if d is None:
                state = "离线(从未心跳)"
            elif d < WINDOW:
                state = "在线"
            else:
                state = "离线(%d 秒前心跳)" % d
        seen = a.get("lastSeen") or ""
        ver = a.get("version") or ""
        line = "    - %s  %s" % (name, state)
        if seen:
            line += "  最近心跳 %s" % seen
        print(line)
        if ver:
            extra = "  版本 %s" % ver
            if latest and ver != latest:
                extra += "(云端最新 %s,可升级)" % latest
            print(extra)

print("")
print("排障提示: 离线时到门店机器上执行 <print-agent> --once 自检;"
      "队列一直积压先查门店代理与打印机。详见 docs/print-agent.md")
sys.exit(0 if online else 1)
PY
  printf '%s' "$HTTP_BODY" | python3 "$PARSE_TMP" "$SERVER"
else
  echo "未找到 python3,输出原始接口响应(退出码仅按顶层 online 字段粗判):"
  printf '%s\n' "$HTTP_BODY"
  printf '%s' "$HTTP_BODY" | grep -q '"online":true' && exit 0 || exit 1
fi
