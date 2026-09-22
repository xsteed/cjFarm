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
# 产物来源(相对项目/包根目录):
#   backend/bin/dining-backend-linux-<arch>   后端二进制
#   frontend/dist/                            前端构建产物
#   backend/uploads/                          种子图(仅补齐缺失, 不覆盖线上已有)
#
# 说明: 更新时只覆盖「程序产物」(bin / dist), 绝不触碰
#       dining.db、uploads 里的用户图片、data/master.key、config.yaml、/etc/dining-backend.env
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
UPLOAD_DIR="$INSTALL_DIR/uploads"
DB_PATH="$INSTALL_DIR/dining.db"
KEEP_BACKUPS="${KEEP_BACKUPS:-5}"                    # 保留最近几份备份
HEALTH_PATH="${HEALTH_PATH:-/prod-api/api/dining/config}"

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
UPLOADS_SRC="$PROJECT_DIR/backend/uploads"

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
  mkdir -p "$BIN_DIR" "$FRONTEND_DIST" "$UPLOAD_DIR" "$INSTALL_DIR/logs"

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

  if [ -d "$UPLOADS_SRC" ]; then
    log "补齐种子图片(菜品图与收款码, 不覆盖线上已有) ..."
    if command -v rsync >/dev/null 2>&1; then
      rsync -a --ignore-existing "$UPLOADS_SRC/" "$UPLOAD_DIR/"
    else
      cp -rn "$UPLOADS_SRC/." "$UPLOAD_DIR/" 2>/dev/null || true
    fi
  else
    warn "未找到种子图目录 $UPLOADS_SRC, 线上菜品图可能 404"
  fi

  # 运维脚本(check-health.sh)随包更新, 便于服务器上直接跑
  if [ -f "$PROJECT_DIR/deploy/check-health.sh" ]; then
    install -m 0755 "$PROJECT_DIR/deploy/check-health.sh" "$BIN_DIR/check-health.sh"
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
    chown "$SERVICE_USER:$SERVICE_USER" "$env_file" 2>/dev/null || true
    chmod 600 "$env_file"
  else
    log "保留已有凭据文件 $env_file"
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
fi

restart_and_verify
prune_backups

if [ "$MODE" = "install" ]; then
  log "部署完成!"
else
  log "更新完成!"
fi
cat <<EOF

  安装目录   : $INSTALL_DIR
  后端二进制 : $BIN_DIR/dining-backend
  前端产物   : $FRONTEND_DIST
  上传目录   : $UPLOAD_DIR
  数据库文件 : $DB_PATH
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
