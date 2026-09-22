#!/usr/bin/env bash
# ============================================================================
# 扫码点餐管理系统 - Nginx 生产配置一键脚本（在目标 Linux 服务器上以 root 执行）
#
# 本脚本把「改 server_name、换安装路径、切 HTTPS、备份旧配置、nginx -t 校验、
# 失败回滚、reload」这些手工操作全部自动化, 可反复执行(幂等)。
#
# 用法:
#   sudo bash deploy/setup-nginx.sh
#       纯 IP 模式: server_name=111.230.154.50, 只监听 80 端口
#
#   sudo bash deploy/setup-nginx.sh --ip 1.2.3.4
#       指定公网 IP
#
#   sudo bash deploy/setup-nginx.sh --server-name _
#       通配模式: 任何 Host 都匹配(不知道域名/IP 时的兜底, deploy.sh 未设
#       SERVER_NAME 时就是走这条)
#
#   sudo bash deploy/setup-nginx.sh --domain dining.example.com \
#        --cert /etc/nginx/ssl/dining.crt --key /etc/nginx/ssl/dining.key
#       域名 + HTTPS: 自动取消 443 段注释、替换证书路径、开启 80→443 跳转
#
#   sudo bash deploy/setup-nginx.sh --dry-run
#       只生成并做语法校验, 不写系统配置、不 reload(可先用它彩排)
#
# 可选参数:
#   --server-name NAME   直接指定 server_name(不启用 HTTPS)
#   --install-dir DIR    安装目录, 默认 /opt/dining-system
#   --port PORT          后端端口, 默认 8080
#   --conf-dst PATH      目标配置文件, 默认 /etc/nginx/conf.d/dining-system.conf
#   --no-install         不自动安装 Nginx(yum/apt)
#   --disable-default    禁用发行版自带的默认站点(sites-enabled/default)
#   --open-firewall      放行 80/443(firewall-cmd / ufw)
#   --emit PATH          只把生成的配置输出到 PATH(不校验/不落盘/不重载, 便于评审)
#   --allow-non-root     允许非 root 运行(仅配合 --dry-run / --emit 有意义)
#   -h, --help           查看帮助
#
# 脚本每一步都会打印, 结束时给出访问地址、后续操作与回滚方法。
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="${TEMPLATE:-$SCRIPT_DIR/nginx-production.conf}"

INSTALL_DIR="${INSTALL_DIR:-/opt/dining-system}"
BACKEND_PORT="${BACKEND_PORT:-8080}"
CONF_DST="${CONF_DST:-/etc/nginx/conf.d/dining-system.conf}"
DEFAULT_IP="111.230.154.50"

SERVER_NAME="${SERVER_NAME:-}"
DOMAIN=""
IP=""
CERT=""
KEY=""
EMIT=""
ENABLE_HTTPS=0
DO_INSTALL=1
DRY_RUN=0
DISABLE_DEFAULT=0
OPEN_FIREWALL=0
ALLOW_NON_ROOT=0
KEEP_BACKUPS="${KEEP_BACKUPS:-5}"
TMP_CONF=""

log()  { printf '\033[1;32m[nginx]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
info() { printf '\033[1;36m[info]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

cleanup() { [ -n "$TMP_CONF" ] && rm -f "$TMP_CONF" || true; }

usage() { awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "${BASH_SOURCE[0]}"; }

# ---------------------------- 参数解析 ----------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --ip)               IP="${2:-}"; shift 2 ;;
    --domain)           DOMAIN="${2:-}"; shift 2 ;;
    --cert)             CERT="${2:-}"; shift 2 ;;
    --key)              KEY="${2:-}"; shift 2 ;;
    --server-name)      SERVER_NAME="${2:-}"; shift 2 ;;
    --install-dir)      INSTALL_DIR="${2:-}"; shift 2 ;;
    --port)             BACKEND_PORT="${2:-}"; shift 2 ;;
    --conf-dst)         CONF_DST="${2:-}"; shift 2 ;;
    --emit)             EMIT="${2:-}"; shift 2 ;;
    --no-install)       DO_INSTALL=0; shift ;;
    --dry-run)          DRY_RUN=1; shift ;;
    --disable-default)  DISABLE_DEFAULT=1; shift ;;
    --open-firewall)    OPEN_FIREWALL=1; shift ;;
    --allow-non-root)   ALLOW_NON_ROOT=1; shift ;;
    -h|--help)          usage; exit 0 ;;
    *)                  die "未知参数: $1 (用 --help 查看用法)" ;;
  esac
done

# ---------------------------- 参数校验 ----------------------------
[ -f "$TEMPLATE" ] || die "找不到模板: $TEMPLATE"
grep -q 'HTTPS_BLOCK_BEGIN' "$TEMPLATE" || die "模板缺少 HTTPS_BLOCK_BEGIN 标记: $TEMPLATE"

if [ -n "$DOMAIN" ]; then
  [ -z "$IP" ] || die "--domain 与 --ip 只能二选一"
  [ -n "$CERT" ] && [ -n "$KEY" ] || die "--domain 模式必须同时提供 --cert 与 --key"
  SERVER_NAME="${SERVER_NAME:-$DOMAIN}"
  ENABLE_HTTPS=1
  [ -f "$CERT" ] || die "证书文件不存在: $CERT"
  [ -f "$KEY" ]  || die "私钥文件不存在: $KEY"
elif [ -n "$IP" ]; then
  SERVER_NAME="${SERVER_NAME:-$IP}"
fi
SERVER_NAME="${SERVER_NAME:-$DEFAULT_IP}"

# server_name 直接写进配置文件, 只允许合法字符, 避免注入超范围配置
case "$SERVER_NAME" in
  *[!A-Za-z0-9._*-]*) die "server_name 含非法字符: $SERVER_NAME (只允许字母/数字/./-/_)" ;;
esac
case "$BACKEND_PORT" in
  ''|*[!0-9]*) die "端口必须是数字: $BACKEND_PORT" ;;
esac

# ---------------------------- 生成配置 ----------------------------
generate_config() {   # $1 = 输出文件路径
  awk -v server_name="$SERVER_NAME" \
      -v install_dir="$INSTALL_DIR" \
      -v backend_port="$BACKEND_PORT" \
      -v cert="$CERT" \
      -v key="$KEY" \
      -v https="$ENABLE_HTTPS" \
      -v gen_time="$(date '+%Y-%m-%d %H:%M:%S')" '
    # 去掉行首的 "# " 注释符, 同时保留原有缩进(跳转行是带缩进的注释)
    function uncomment(s,   indent, rest) {
      match(s, /^[ \t]*/)
      indent = substr(s, 1, RLENGTH)
      rest = substr(s, RLENGTH + 1)
      sub(/^#[ \t]?/, "", rest)
      return indent rest
    }

    { line[NR] = $0 }

    END {
      hb = he = rb = re = 0
      for (i = 1; i <= NR; i++) {
        if (line[i] ~ /HTTPS_BLOCK_BEGIN/) hb = i
        if (line[i] ~ /HTTPS_BLOCK_END/)   he = i
        if (line[i] ~ /REDIRECT_BEGIN/)    rb = i
        if (line[i] ~ /REDIRECT_END/)      re = i
      }
      if (!hb || !he || !rb || !re) { print "模板标记不完整" > "/dev/stderr"; exit 3 }

      print "# !!! 由 deploy/setup-nginx.sh 于 " gen_time " 自动生成, 请勿手工修改(重跑脚本会整体覆盖) !!!"
      print "# 生成参数: server_name=" server_name ", install_dir=" install_dir ", backend_port=" backend_port ", https=" (https ? "on" : "off")

      for (i = 1; i <= NR; i++) {
        if (i == hb || i == he || i == rb || i == re) continue   # 丢弃脚本用的标记行
        l = line[i]
        if (https && i > hb && i < he) l = uncomment(l)   # 打开 443 服务块
        if (https && i > rb && i < re) l = uncomment(l)   # 打开 80→443 跳转

        # 只替换 server_name 指令的取值, 不动注释里的示例
        if (l ~ /^[ \t]*server_name[ \t]+111\.230\.154\.50/) sub(/111\.230\.154\.50/, server_name, l)

        if (https) {
          gsub(/\/etc\/nginx\/ssl\/dining\.crt/, cert, l)
          gsub(/\/etc\/nginx\/ssl\/dining\.key/, key, l)
        }
        gsub(/\/opt\/dining-system/, install_dir, l)
        gsub(/127\.0\.0\.1:8080/, "127.0.0.1:" backend_port, l)
        print l
      }
    }
  ' "$TEMPLATE" > "$1"
}

info "生成参数: server_name=$SERVER_NAME install_dir=$INSTALL_DIR backend_port=$BACKEND_PORT https=$([ "$ENABLE_HTTPS" = "1" ] && echo on || echo off)"

if [ -n "$EMIT" ]; then
  generate_config "$EMIT" || die "生成失败(模板标记不完整?)"
  log "已生成: $EMIT"
  exit 0
fi

# ---------------------------- 权限 ----------------------------
if [ "$ALLOW_NON_ROOT" != "1" ] && [ "$(id -u)" -ne 0 ]; then
  die "请用 root 运行: sudo bash deploy/setup-nginx.sh (演练可加 --dry-run --allow-non-root)"
fi

trap cleanup EXIT

# ---------------------------- 安装 Nginx ----------------------------
if ! command -v nginx >/dev/null 2>&1; then
  if [ "$DO_INSTALL" != "1" ]; then
    die "未安装 nginx, 且指定了 --no-install"
  fi
  log "未检测到 nginx, 尝试安装 ..."
  if command -v dnf >/dev/null 2>&1; then
    dnf install -y nginx
  elif command -v yum >/dev/null 2>&1; then
    yum install -y nginx
  elif command -v apt-get >/dev/null 2>&1; then
    apt-get update -y && apt-get install -y nginx
  else
    die "无法识别包管理器, 请手动安装 nginx 后重试"
  fi
  command -v nginx >/dev/null 2>&1 || die "nginx 安装失败"
  log "nginx 安装完成: $(nginx -v 2>&1)"
else
  info "已安装: $(nginx -v 2>&1)"
fi

CONF_DIR="$(dirname "$CONF_DST")"

# conf.d 是否被主配置 include(部分发行版改用 sites-enabled)
if [ -f /etc/nginx/nginx.conf ] && ! grep -Eq '^[[:space:]]*include[[:space:]]+.*conf\.d/.*\.conf' /etc/nginx/nginx.conf; then
  warn "/etc/nginx/nginx.conf 中未见 include conf.d/*.conf, 请确认 $CONF_DST 会被加载"
fi

TMP_CONF="$(mktemp)"
generate_config "$TMP_CONF" || die "生成配置失败(模板标记不完整?)"

# ---------------------------- 语法校验 ----------------------------
# 用「最小主配置 include 生成结果」的方式校验: 不依赖系统现有 nginx.conf, 也能在 dry-run 阶段发现问题。
check_syntax() {   # $1 = 待校验的完整配置文件
  local target="$1" tmp_main
  tmp_main="$(mktemp)"
  {
    printf 'worker_processes 1;\n'
    printf 'error_log /dev/null;\n'
    printf 'pid /tmp/nginx-setup-check.pid;\n'
    printf 'events { worker_connections 64; }\n'
    printf 'http {\n    include %s;\n}\n' "$target"
  } > "$tmp_main"
  if nginx -t -c "$tmp_main" 2>&1 | sed 's/^/    /'; then
    rm -f "$tmp_main"
    return 0
  fi
  rm -f "$tmp_main"
  return 1
}

log "校验生成结果 ..."
if ! check_syntax "$TMP_CONF"; then
  if [ "$DRY_RUN" = "1" ]; then
    info "生成结果(供排查, 前 40 行):"
    sed -n '1,40p' "$TMP_CONF" | sed 's/^/    /'
  fi
  die "生成的配置未通过语法校验, 未做任何改动"
fi
log "语法校验通过"

if [ "$DRY_RUN" = "1" ]; then
  info "dry-run 模式: 未写入 $CONF_DST, 未重载 nginx"
  info "预览(前 20 行):"
  sed -n '1,20p' "$TMP_CONF" | sed 's/^/    /'
  info "完整预览: $TMP_CONF (脚本退出后自动清理, 需要留存请用 --emit <路径>)"
  exit 0
fi

# ---------------------------- 备份旧配置 ----------------------------
mkdir -p "$CONF_DIR"
BACKUP=""
if [ -f "$CONF_DST" ]; then
  BACKUP="$CONF_DST.bak.$(date +%Y%m%d-%H%M%S)"
  cp -a "$CONF_DST" "$BACKUP"
  log "已备份旧配置 -> $BACKUP"
  COUNT=0
  for f in $(ls -1dt "$CONF_DST".bak.* 2>/dev/null); do
    COUNT=$((COUNT + 1))
    [ "$COUNT" -gt "$KEEP_BACKUPS" ] && rm -f "$f"
  done
fi

# ---------------------------- 写入配置 ----------------------------
install -m 0644 "$TMP_CONF" "$CONF_DST"
log "已写入 $CONF_DST"

# ---------------------------- 整体校验(含与主配置的冲突) ----------------------------
if ! nginx -t 2>&1 | sed 's/^/    /'; then
  warn "nginx -t 失败, 开始回滚 ..."
  if [ -n "$BACKUP" ] && [ -f "$BACKUP" ]; then
    cp -a "$BACKUP" "$CONF_DST"
    warn "已回滚到 $BACKUP (未重载服务, 线上仍是旧配置)"
  else
    rm -f "$CONF_DST"
    warn "已删除本次写入的配置(此前无旧配置可回滚)"
  fi
  die "配置未生效, 请根据上方报错修改后重试"
fi
log "整体校验通过"

# ---------------------------- 发行版默认站点 ----------------------------
if [ -e /etc/nginx/sites-enabled/default ]; then
  if [ "$DISABLE_DEFAULT" = "1" ]; then
    rm -f /etc/nginx/sites-enabled/default
    log "已禁用发行版默认站点 /etc/nginx/sites-enabled/default"
  else
    warn "存在 /etc/nginx/sites-enabled/default(默认站点), 如需以本配置为准请加 --disable-default"
  fi
fi

# ---------------------------- SELinux(反代必需) ----------------------------
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null || echo Disabled)" = "Enforcing" ]; then
  if command -v setsebool >/dev/null 2>&1; then
    if setsebool -P httpd_can_network_connect 1; then
      log "SELinux: 已开启 httpd_can_network_connect(允许 nginx 反代到 127.0.0.1:$BACKEND_PORT)"
    else
      warn "SELinux: setsebool 失败, 反代可能报 502"
    fi
  else
    warn "SELinux 处于 Enforcing 但缺少 setsebool, 反代可能报 502"
  fi
fi

# ---------------------------- 防火墙 ----------------------------
open_firewall() {
  if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    firewall-cmd --permanent --add-service=http >/dev/null
    [ "$ENABLE_HTTPS" = "1" ] && firewall-cmd --permanent --add-service=https >/dev/null
    firewall-cmd --reload >/dev/null
    log "firewalld: 已放行 80$([ "$ENABLE_HTTPS" = "1" ] && echo '/443')"
  elif command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
    ufw allow 80/tcp >/dev/null
    [ "$ENABLE_HTTPS" = "1" ] && ufw allow 443/tcp >/dev/null
    log "ufw: 已放行 80$([ "$ENABLE_HTTPS" = "1" ] && echo '/443')"
  else
    warn "未检测到启用的 firewalld/ufw, 请确认云厂商安全组已放行 80$([ "$ENABLE_HTTPS" = "1" ] && echo '/443')"
  fi
}
if [ "$OPEN_FIREWALL" = "1" ]; then
  open_firewall
elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  warn "本机 firewalld 处于开启状态; 如需脚本放行端口请加 --open-firewall"
fi

# ---------------------------- 生效 ----------------------------
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable nginx >/dev/null 2>&1 || true
  if systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null; then
    log "nginx 已重载"
  else
    warn "systemctl 重载失败, 回退 nginx -s reload"
    nginx -s reload || die "nginx 重载失败, 请查看 journalctl -u nginx -n 50"
    log "nginx 已重载"
  fi
else
  nginx -s reload || die "nginx 重载失败"
  log "nginx 已重载"
fi

# ---------------------------- 线上自检 ----------------------------
SITE_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H "Host: $SERVER_NAME" http://127.0.0.1/ 2>/dev/null || echo 000)"
API_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H "Host: $SERVER_NAME" http://127.0.0.1/prod-api/api/dining/config 2>/dev/null || echo 000)"

case "$SITE_CODE" in
  200|301|302) log "前端自检通过 (HTTP $SITE_CODE)" ;;
  000)         warn "前端自检失败: 无法连接 nginx" ;;
  *)           warn "前端自检异常: HTTP $SITE_CODE (确认 $INSTALL_DIR/frontend/dist 已部署)" ;;
esac
case "$API_CODE" in
  200) log "后端反代自检通过 (HTTP 200)" ;;
  *)   warn "后端反代自检异常: HTTP $API_CODE (确认后端已启动: systemctl status dining-backend)" ;;
esac

# ---------------------------- 完成 ----------------------------
SCHEME="http"
[ "$ENABLE_HTTPS" = "1" ] && SCHEME="https"
cat <<EOF

配置完成:
  配置文件   : $CONF_DST
  server_name: $SERVER_NAME
  访问地址   : $SCHEME://$SERVER_NAME/
  管理后台   : $SCHEME://$SERVER_NAME/dining/dashboard
  后端反代   : /prod-api/ -> 127.0.0.1:$BACKEND_PORT

后续操作:
  切到 HTTPS : sudo bash deploy/setup-nginx.sh --domain <域名> --cert <证书> --key <私钥>
  改端口     : sudo bash deploy/setup-nginx.sh --port 9000
  彩排/自检  : sudo bash deploy/setup-nginx.sh --dry-run

回滚(如需):
EOF
if [ -n "$BACKUP" ]; then
  echo "  sudo cp -a $BACKUP $CONF_DST && sudo nginx -t && sudo nginx -s reload"
else
  echo "  本次为首次生成, 回滚即删除: sudo rm -f $CONF_DST && sudo nginx -s reload"
fi
