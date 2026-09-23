#!/usr/bin/env bash
# ============================================================================
# 扫码点餐管理系统 - 部署 / 更新脚本（服务器端，同一个命令两件事）
#
# 自动识别模式:
#   安装目录下没有后端二进制 → 首次安装(建用户 + 装 systemd + 配 Nginx + 启动)
#   已有后端二进制           → 覆盖更新(先备份, 失败自动回滚)
# 因此首次部署和日常更新都执行同一条命令, 不需要记两套脚本。
#
# 用法:
#   sudo bash deploy/deploy.sh
#
#   # 指定域名/IP 以生成生产 Nginx 配置(含 HTTPS); 不指定则用通配 "_"
#   SERVER_NAME=111.230.154.50 sudo bash deploy/deploy.sh
#   SERVER_NAME=dining.example.com SSL_CERT=/etc/nginx/ssl/d.crt \
#     SSL_KEY=/etc/nginx/ssl/d.key sudo bash deploy/deploy.sh
#
#   # 调整安装位置 / 端口 / 运行用户
#   INSTALL_DIR=/opt/dining-system BACKEND_PORT=9000 sudo bash deploy/deploy.sh
#
# 可选参数:
#   --skip-build   不编译, 只用现成产物(产物缺失时报错)
#   --rebuild      强制重新编译(仓库内开发时用)
#   默认: 有产物就直接用, 没有才编译(更新包内天然走这条路)
#
# 切换 MySQL(DB_* 变量仅首次安装时写入 /etc/dining-backend.env,
# 更新模式已有该文件则保留不动, 需切换时手动编辑; 详见 docs/mysql-migration.md):
#   sudo DB_DRIVER=mysql DB_HOST=10.0.0.9 DB_USER=dining \
#     DB_PASSWORD=xxx DB_NAME=dining bash deploy/deploy.sh
#   或一行完整连接串: sudo DB_DSN='dining:pw@tcp(10.0.0.9:3306)/dining?charset=utf8mb4&parseTime=true&loc=Local&maxAllowedPacket=67108864' bash deploy/deploy.sh
#
# 产物来源(相对项目/包根目录):
#   backend/bin/dining-backend-linux-<arch>   后端二进制
#   frontend/dist/                            前端构建产物
#
# 说明: 更新时只覆盖「程序产物」(bin / dist), 绝不触碰
#       data/dining.db、uploads 目录(仅作升级时存量图片导入源, 不删除、不再写入)、
#       data/master.key、config.yaml、/etc/dining-backend.env
# ============================================================================
set -euo pipefail

# ---------------------------- 可配置项 ----------------------------
INSTALL_DIR="${INSTALL_DIR:-/opt/dining-system}"   # 安装根目录
BACKEND_PORT="${BACKEND_PORT:-8080}"                # 后端监听端口
SERVICE_USER="${SERVICE_USER:-dining}"              # systemd 运行用户
SERVICE_NAME="dining-backend"
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"  # 项目/包 根目录

BIN_DIR="$INSTALL_DIR/bin"
FRONTEND_DIST="$INSTALL_DIR/frontend/dist"
DB_PATH="$INSTALL_DIR/data/dining.db"
KEEP_BACKUPS="${KEEP_BACKUPS:-5}"                    # 保留最近几份备份
HEALTH_PATH="${HEALTH_PATH:-/api/customer/config}"

# ---------------------------- 参数解析 ----------------------------
FORCE_REBUILD=0
SKIP_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --rebuild)    FORCE_REBUILD=1 ;;
    --skip-build) SKIP_BUILD=1 ;;
    -h|--help)    sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $arg (用 -h 查看用法)" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1;32m[deploy]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

# ---------------------------- 前置校验 ----------------------------
if [ "${ALLOW_NON_ROOT:-0}" != "1" ] && [ "$(id -u)" -ne 0 ]; then
  die "请用 root 运行: sudo bash deploy/deploy.sh"
fi
HAS_SYSTEMD=1
if ! command -v systemctl >/dev/null 2>&1; then
  HAS_SYSTEMD=0
  warn "未检测到 systemd: 跳过服务注册与重启(仅适合本地演练或无 systemd 容器)"
fi

case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "不支持的 CPU 架构: $(uname -m)" ;;
esac

BIN_SRC="$PROJECT_DIR/backend/bin/dining-backend-linux-$ARCH"
DIST_SRC="$PROJECT_DIR/frontend/dist"

# ---------------------------- 模式判定 ----------------------------
if [ -x "$BIN_DIR/dining-backend" ]; then
  MODE="update"
else
  MODE="install"
fi
if [ "$MODE" = "install" ]; then MODE_TEXT="首次安装"; else MODE_TEXT="覆盖更新"; fi
log "模式: $MODE_TEXT  (安装目录: $INSTALL_DIR)"

# ---------------------------- 管理端初始凭据 ----------------------------
# 仅首次建库时读取(写入 tb_user);未设置时回退到默认值。
ADMIN_USER="${ADMIN_USER:-admin}"
if [ -z "${ADMIN_PASS:-}" ]; then
  if [ "$MODE" = "install" ]; then
    ADMIN_PASS="admin123"
    warn "未设置 ADMIN_PASS, 将使用默认口令 admin123; 公网环境请务必自行修改。"
  else
    ADMIN_PASS=""
  fi
fi

# ---------------------------- 数据库后端(可选) ----------------------------
# 默认 SQLite(与代码默认一致)。部署命令传 DB_DRIVER=mysql 或 DB_DSN 时,
# 首次安装会把这些变量写进 /etc/dining-backend.env(权限 600),由 systemd 的
# EnvironmentFile 加载; 不传则完全不写 DB 配置, 保持单机 SQLite 开箱即用。

# db_env_block 生成要追加到 env 文件的数据库配置段。
db_env_block() {
  if [ "${DB_DRIVER:-}" != "mysql" ] && [ -z "${DB_DSN:-}" ]; then
    return 0
  fi
  printf '# ---- 数据库后端(deploy.sh 注入, 详见 docs/mysql-migration.md) ----\n'
  if [ -n "${DB_DSN:-}" ]; then
    printf 'DB_DSN=%s\n' "$DB_DSN"
    if [ "${DB_DRIVER:-}" = "mysql" ]; then
      printf 'DB_DRIVER=mysql\n'
    fi
    return 0
  fi
  printf 'DB_DRIVER=mysql\n'
  [ -n "${DB_HOST:-}" ]     && printf 'DB_HOST=%s\n' "$DB_HOST"
  [ -n "${DB_PORT:-}" ]     && printf 'DB_PORT=%s\n' "$DB_PORT"
  [ -n "${DB_USER:-}" ]     && printf 'DB_USER=%s\n' "$DB_USER"
  [ -n "${DB_PASSWORD:-}" ] && printf 'DB_PASSWORD=%s\n' "$DB_PASSWORD"
  [ -n "${DB_NAME:-}" ]     && printf 'DB_NAME=%s\n' "$DB_NAME"
  [ -n "${DB_PARAMS:-}" ]   && printf 'DB_PARAMS=%s\n' "$DB_PARAMS"
  return 0
}

# mask_dsn 隐藏连接串密码(root:secret@tcp(...) -> root:***@tcp(...)),
# 避免在部署总结里明文输出。与后端 store.maskDSN 逻辑一致。
mask_dsn() {
  local dsn="$1" cred="${dsn%@*}"
  [ "$cred" = "$dsn" ] && { printf '%s' "$dsn"; return 0; }   # 无 @,原样返回
  case "$cred" in
    *:*) printf '%s' "${cred%%:*}:***@${dsn#*@}" ;;
    *)   printf '%s' "$dsn" ;;
  esac
}

# db_summary 输出部署总结里的「数据库后端」一行:
#   1. 本次命令传了 DB_DRIVER/DB_DSN → 用它;
#   2. 否则看既有 env 文件(更新模式, 配置可能已写在其中);
#   3. 都没有 → SQLite。若服务器上另有 config.yaml 配了 MySQL, 以实际日志为准。
db_summary() {
  local env_file="/etc/${SERVICE_NAME}.env"
  local drv="${DB_DRIVER:-}" dsn="${DB_DSN:-}"
  if [ "$drv" != "mysql" ] && [ -z "$dsn" ] && [ -f "$env_file" ]; then
    drv="$(sed -n 's/^DB_DRIVER=//p' "$env_file" | head -1)"
    dsn="$(sed -n 's/^DB_DSN=//p' "$env_file" | head -1)"
  fi
  if [ "$drv" = "mysql" ] || [ -n "$dsn" ]; then
    if [ -n "$dsn" ]; then
      printf 'MySQL 连接串: %s' "$(mask_dsn "$dsn")"
    else
      printf 'MySQL: %s@%s:%s/%s' \
        "${DB_USER:-root}" "${DB_HOST:-127.0.0.1}" "${DB_PORT:-3306}" "${DB_NAME:-dining}"
    fi
    return 0
  fi
  printf 'SQLite 文件: %s' "$DB_PATH"
}

# ---------------------------- 产物准备 ----------------------------
prepare_artifacts() {
  local need=0
  [ -f "$BIN_SRC" ] || need=1
  [ -f "$DIST_SRC/index.html" ] || need=1
  [ "$FORCE_REBUILD" = "1" ] && need=1

  if [ "$SKIP_BUILD" = "1" ] || [ "$need" = "0" ]; then
    [ -f "$BIN_SRC" ] || die "缺少后端二进制: $BIN_SRC (包内未带本机架构 $ARCH 的产物?)"
    [ -f "$DIST_SRC/index.html" ] || die "缺少前端产物: $DIST_SRC/index.html"
    log "使用现成产物(不编译)"
    return
  fi

  command -v go >/dev/null 2>&1 || die "缺少 go, 无法编译; 或改用已含产物的更新包"
  log "编译后端 (linux/$ARCH) ..."
  ( cd "$PROJECT_DIR/backend"
    GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION:-dev} -X main.buildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
      -o "$BIN_SRC" . )

  log "构建前端 ..."
  ( cd "$PROJECT_DIR/frontend"
    [ -d node_modules ] || npm ci
    npm run build )
}

# ---------------------------- 备份(仅更新) ----------------------------
BACKUP_DIR=""
backup_current() {
  [ "$MODE" = "update" ] || return 0
  BACKUP_DIR="$INSTALL_DIR/backup/$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$BACKUP_DIR"
  [ -f "$BIN_DIR/dining-backend" ] && cp -a "$BIN_DIR/dining-backend" "$BACKUP_DIR/"
  [ -d "$FRONTEND_DIST" ] && cp -a "$FRONTEND_DIST" "$BACKUP_DIR/dist"
  log "已备份当前版本 -> $BACKUP_DIR"
}

rollback() {
  [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || { warn "无备份可回滚"; return 0; }
  warn "回滚到更新前版本 ..."
  if [ -f "$BACKUP_DIR/dining-backend" ]; then
    install -m 0755 "$BACKUP_DIR/dining-backend" "$BIN_DIR/dining-backend.new"
    mv -f "$BIN_DIR/dining-backend.new" "$BIN_DIR/dining-backend"
  fi
  if [ -d "$BACKUP_DIR/dist" ]; then
    rm -rf "$FRONTEND_DIST"
    cp -a "$BACKUP_DIR/dist" "$FRONTEND_DIST"
  fi
  [ "$HAS_SYSTEMD" = "1" ] && systemctl restart "$SERVICE_NAME" || true
}

# ---------------------------- 安装产物 ----------------------------
install_artifacts() {
  mkdir -p "$BIN_DIR" "$FRONTEND_DIST" "$INSTALL_DIR/logs"

  log "安装后端二进制 ..."
  # 先写临时文件再原子替换: 直接 cp 到运行中的二进制会报 Text file busy
  install -m 0755 "$BIN_SRC" "$BIN_DIR/dining-backend.new"
  mv -f "$BIN_DIR/dining-backend.new" "$BIN_DIR/dining-backend"

  log "安装前端产物 ..."
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete "$DIST_SRC/" "$FRONTEND_DIST/"
  else
    rm -rf "$FRONTEND_DIST" && mkdir -p "$FRONTEND_DIST"
    cp -r "$DIST_SRC/." "$FRONTEND_DIST/"
  fi

  # 运维脚本(check-health.sh)随包更新, 便于服务器上直接跑
  if [ -f "$PROJECT_DIR/deploy/check-health.sh" ]; then
    install -m 0755 "$PROJECT_DIR/deploy/check-health.sh" "$BIN_DIR/check-health.sh"
  fi

  # 打印代理产物: 供门店侧 print-agent --upgrade 自助升级下载。
  # 后端默认 AGENT_BIN_DIR=print-agent/bin(相对 WorkingDirectory=/opt/dining-system),
  # 部署到这里即与默认路径对齐, 无需额外配置。
  if [ -d "$PROJECT_DIR/print-agent/bin" ]; then
    mkdir -p "$INSTALL_DIR/print-agent/bin"
    cp -f "$PROJECT_DIR"/print-agent/bin/* "$INSTALL_DIR/print-agent/bin/"
    log "安装打印代理产物 -> $INSTALL_DIR/print-agent/bin"
  fi
}

# ---------------------------- 运行用户 ----------------------------
ensure_service_user() {
  if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    log "创建运行用户 $SERVICE_USER ..."
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER" >/dev/null 2>&1 || true
  fi
  chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR" 2>/dev/null \
    || warn "chown 到 $SERVICE_USER 失败(用户可能不存在), 请确认文件对服务进程可读"
}

# ---------------------------- systemd 服务 ----------------------------
setup_service() {
  if [ "$HAS_SYSTEMD" != "1" ]; then
    warn "跳过 systemd 服务安装(环境无 systemctl)"
    return 0
  fi
  local src="$PROJECT_DIR/deploy/$SERVICE_NAME.service"
  [ -f "$src" ] || die "缺少服务单元: $src"
  log "安装 systemd 服务 ..."
  sed -e "s|/opt/dining-system|$INSTALL_DIR|g" \
      -e "s|^User=dining|User=$SERVICE_USER|" \
      -e "s|^Group=dining|Group=$SERVICE_USER|" \
      "$src" > "/etc/systemd/system/$SERVICE_NAME.service"

  # 管理端凭据 + 可信代理(仅首次安装时写入口令, 更新时不覆盖已生效的凭据)
  local env_file="/etc/${SERVICE_NAME}.env"
  if [ "$MODE" = "install" ] || [ ! -f "$env_file" ]; then
    log "写入管理端凭据 -> $env_file"
    umask 077
    cat > "$env_file" <<EOF
ADMIN_USER=$ADMIN_USER
ADMIN_PASS=$ADMIN_PASS
# 经 Nginx 反代时必须声明可信代理, 否则后端 c.ClientIP() 恒为 127.0.0.1:
# 登录失败计数会在全店所有设备间共享 —— 任何一台连续输错 5 次,
# 其他设备(尤其手机端)会跟着被锁 15 分钟, 表现为「突然所有人都登不上」。
TRUSTED_PROXIES=127.0.0.1
EOF
    # 数据库后端: 部署命令传了 DB_* 时一并写入(未传则保持默认 SQLite, 不写 DB 键)
    db_env_block >> "$env_file"
    if grep -q '^DB_DRIVER=mysql$' "$env_file"; then
      log "已写入 MySQL 数据库配置(DB_* 变量)"
    fi
    chown "$SERVICE_USER:$SERVICE_USER" "$env_file" 2>/dev/null || true
    chmod 600 "$env_file"
  else
    log "保留已有凭据文件 $env_file"
    if [ "${DB_DRIVER:-}" = "mysql" ] || [ -n "${DB_DSN:-}" ]; then
      warn "本次传入了 DB_* 但 $env_file 已存在(更新模式不覆盖), 变量未生效; 如需切换请手动编辑该文件后重启服务"
    fi
  fi

  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
}

# ---------------------------- Nginx ----------------------------
# 配置只有一份来源: deploy/nginx-production.conf, 由 setup-nginx.sh 生成。
# 未指定 SERVER_NAME 时用通配 "_"(任何 Host 都匹配), 等价于过去的通用配置。
setup_nginx() {
  local dst="/etc/nginx/conf.d/dining-system.conf"
  local setup="$PROJECT_DIR/deploy/setup-nginx.sh"

  if ! command -v nginx >/dev/null 2>&1; then
    warn "未检测到 Nginx, 跳过 Nginx 配置(后端仍可经 http://<服务器IP>:$BACKEND_PORT 直接访问)"
    return 0
  fi
  if [ ! -f "$setup" ]; then
    warn "缺少 $setup, 跳过 Nginx 配置(可手动执行: sudo bash deploy/setup-nginx.sh --server-name _)"
    return 0
  fi
  mkdir -p /etc/nginx/conf.d

  local name="${SERVER_NAME:-_}"
  local args=(--no-install --conf-dst "$dst" --install-dir "$INSTALL_DIR" --port "$BACKEND_PORT")
  if [ -n "${SSL_CERT:-}" ] && [ -n "${SSL_KEY:-}" ]; then
    args+=(--domain "$name" --cert "$SSL_CERT" --key "$SSL_KEY")
  else
    { [ -n "${SSL_CERT:-}" ] || [ -n "${SSL_KEY:-}" ]; } && warn "SSL_CERT / SSL_KEY 需成对提供, 已按纯 HTTP(80) 处理"
    args+=(--server-name "$name")
  fi
  log "生成 Nginx 配置 (server_name=$name) ..."
  bash "$setup" "${args[@]}" || warn "Nginx 配置安装失败, 可手动重跑: sudo bash deploy/setup-nginx.sh --help"
}

# ---------------------------- 旧 Nginx 前缀检查(仅更新) ----------------------------
# 后端路由已从 /prod-api 迁移到 /api。本脚本只在首次安装时生成 Nginx 配置,
# 更新模式不覆盖线上配置(避免冲掉自定义的域名/HTTPS), 因此从旧版本升级上来的
# 服务器若不做这一步, Nginx 仍反代 /prod-api/, 前端所有接口会 404。
check_nginx_legacy_prefix() {
  [ "$MODE" = "update" ] || return 0
  local conf="/etc/nginx/conf.d/dining-system.conf"
  [ -f "$conf" ] || return 0
  # 只匹配生效的 location 行(行首无注释), 避免命中模板注释里的加固示例。
  grep -Eq '^[[:space:]]*location[^#]*/prod-api/' "$conf" || return 0

  warn "检测到线上 Nginx 配置仍反代旧 API 前缀 /prod-api/(后端已切换到 /api/)"
  warn "更新模式不会自动覆盖 Nginx 配置, 不处理会导致前端所有接口 404。二选一修复:"
  warn "  1) 最小改动(只换前缀, 保留线上自定义):"
  warn "     sudo sed -i 's|/prod-api/|/api/|g' $conf && sudo nginx -t && sudo nginx -s reload"
  warn "  2) 重新生成(会备份旧配置): 把发布包解压后以 root 执行"
  warn "     sudo bash deploy/setup-nginx.sh --server-name <线上 server_name> [--domain <域名> --cert <证书> --key <私钥>]"
}

# ---------------------------- 重启 + 健康检查 ----------------------------
restart_and_verify() {
  if [ "$HAS_SYSTEMD" = "1" ]; then
    log "重启服务 $SERVICE_NAME ..."
    systemctl restart "$SERVICE_NAME"
  else
    warn "跳过重启(无 systemd), 直接做健康检查"
  fi

  local url="http://127.0.0.1:${BACKEND_PORT}${HEALTH_PATH}"
  local ok=0 i
  sleep 2
  for i in 1 2 3 4 5 6 7 8; do
    if command -v curl >/dev/null 2>&1; then
      curl -sf -o /dev/null --max-time 3 "$url" && { ok=1; break; }
    else
      systemctl is-active --quiet "$SERVICE_NAME" && { ok=1; break; }
    fi
    sleep 1
  done

  if [ "$ok" != "1" ]; then
    warn "健康检查失败: $url"
    rollback
    die "更新未生效, 已回滚。排查: journalctl -u $SERVICE_NAME -n 100 --no-pager"
  fi
  log "健康检查通过: $url"
}

# ---------------------------- 清理旧备份 ----------------------------
prune_backups() {
  [ -d "$INSTALL_DIR/backup" ] || return 0
  local count=0 d
  for d in $(ls -1dt "$INSTALL_DIR"/backup/*/ 2>/dev/null); do
    count=$((count + 1))
    [ "$count" -gt "$KEEP_BACKUPS" ] && rm -rf "$d"
  done
}

# ============================================================================
# 主流程
# ============================================================================
prepare_artifacts
backup_current
install_artifacts
ensure_service_user
setup_service

if [ "$MODE" = "install" ]; then
  setup_nginx
else
  check_nginx_legacy_prefix
fi

restart_and_verify
prune_backups

if [ "$MODE" = "install" ]; then
  log "部署完成!"
else
  log "更新完成!"
fi
DB_SUMMARY="$(db_summary)"
cat <<EOF

  安装目录   : $INSTALL_DIR
  后端二进制 : $BIN_DIR/dining-backend
  前端产物   : $FRONTEND_DIST
  图片存储   : 数据库 tb_image（旧目录 $INSTALL_DIR/uploads 仅作升级导入源）
  数据库后端 : $DB_SUMMARY
  后端端口   : $BACKEND_PORT
  本次模式   : $MODE

常用命令:
  systemctl status $SERVICE_NAME            # 服务状态
  journalctl -u $SERVICE_NAME -f            # 实时日志
  bash $BIN_DIR/check-health.sh              # 健康检查(也可用于 cron 探测)
  nginx -t && nginx -s reload               # 校验并重载 Nginx
EOF
[ "$MODE" = "install" ] && cat <<EOF

管理端账号: $ADMIN_USER / 密码(部署时设置的 ADMIN_PASS)
如需修改: 更新 /etc/${SERVICE_NAME}.env 后 systemctl restart $SERVICE_NAME
EOF
exit 0
