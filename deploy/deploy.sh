#!/usr/bin/env bash
# ============================================================================
# 扫码点餐管理系统 - 一键部署脚本 (目标: Linux 服务器)
#
# 在目标服务器上执行: 构建前端 + 构建后端(Linux 交叉编译) + 安装 systemd 服务 + 配置 Nginx
#
# 前置依赖(目标机): bash, go(>=1.23), node(>=18), npm, nginx, systemd
#
# 用法:
#   sudo bash deploy/deploy.sh            # 完整部署(构建+安装+启动)
#   sudo bash deploy/deploy.sh --skip-build   # 跳过构建, 仅安装已存在的产物
#
# 可配置项通过脚本顶部变量或环境变量覆盖, 例如:
#   INSTALL_DIR=/opt/dining-system BACKEND_PORT=9000 sudo bash deploy/deploy.sh
# ============================================================================

set -euo pipefail

# ---------------------------- 可配置项 ----------------------------
INSTALL_DIR="${INSTALL_DIR:-/opt/dining-system}"   # 安装根目录
BACKEND_PORT="${BACKEND_PORT:-8080}"                # 后端监听端口
SERVICE_USER="${SERVICE_USER:-dining}"              # systemd 运行用户
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"  # 项目根目录

BIN_DIR="$INSTALL_DIR/bin"
FRONTEND_DIST="$INSTALL_DIR/frontend/dist"
UPLOAD_DIR="$INSTALL_DIR/uploads"
DB_PATH="$INSTALL_DIR/dining.db"

# ---------------------------- 参数解析 ----------------------------
SKIP_BUILD=0
if [[ "${1:-}" == "--skip-build" ]]; then
  SKIP_BUILD=1
fi

log()  { echo -e "\033[1;32m[deploy]\033[0m $*"; }
warn() { echo -e "\033[1;33m[warn]\033[0m $*"; }
die()  { echo -e "\033[1;31m[error]\033[0m $*" >&2; exit 1; }

# ---------------------------- 安全校验(部署前置,必须先于构建) ----------------------------
# 生产环境严禁使用默认弱口令 admin123:未设置或为弱口令时直接中止部署。
ADMIN_PASS="${ADMIN_PASS:-}"
ADMIN_USER="${ADMIN_USER:-admin}"
if [[ -z "$ADMIN_PASS" ]]; then
  die "必须设置 ADMIN_PASS 环境变量(至少 8 位强密码)。示例: ADMIN_PASS='你的强密码' sudo bash deploy/deploy.sh"
fi
if [[ "${#ADMIN_PASS}" -lt 8 ]]; then
  die "ADMIN_PASS 长度不足 8 位,请使用强密码。"
fi
case "$ADMIN_PASS" in
  admin123|admin|password|12345678|123456789|qwerty|iloveyou|88888888|00000000)
    die "ADMIN_PASS 为常见弱口令,禁止用于生产环境。"
    ;;
esac
log "已通过管理员密码强度校验"

# ---------------------------- 构建 ----------------------------
if [[ "$SKIP_BUILD" == "0" ]]; then
  log "构建后端 (Linux amd64)..."
  ( cd "$PROJECT_DIR/backend"
    if command -v make >/dev/null 2>&1; then
      make build-linux-amd64
    else
      mkdir -p bin
      CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/dining-server-linux-amd64 .
    fi
  )

  log "构建前端 (npm ci && npm run build)..."
  ( cd "$PROJECT_DIR/frontend" && npm ci && npm run build )
else
  log "跳过构建 (--skip-build), 使用已有产物"
fi

# ---------------------------- 安装目录 ----------------------------
log "创建安装目录..."
mkdir -p "$BIN_DIR" "$FRONTEND_DIST" "$UPLOAD_DIR"

log "安装后端二进制..."
cp -f "$PROJECT_DIR/backend/bin/dining-server-linux-amd64" "$BIN_DIR/dining-server"
chmod +x "$BIN_DIR/dining-server"

log "安装前端产物..."
# 用 rsync 若可用, 否则 cp -r 兜底
if command -v rsync >/dev/null 2>&1; then
  rsync -a --delete "$PROJECT_DIR/frontend/dist/" "$FRONTEND_DIST/"
else
  rm -rf "$FRONTEND_DIST" && mkdir -p "$FRONTEND_DIST"
  cp -r "$PROJECT_DIR/frontend/dist/." "$FRONTEND_DIST/"
fi

log "安装种子图片(菜品图与收款码)..."
# 后端把 /uploads 与 /picture 两个路由都指向同一个上传目录(见 main.go),
# 种子数据里的 dish_image 与 pay_qr_* 走的是 /picture/...,
# 因此这 23 个文件必须随部署一起带上,否则线上菜品图与收款码全部 404。
# 同名文件不覆盖,避免冲掉线上用户后来上传的图片。
if [ -d "$PROJECT_DIR/backend/uploads" ]; then
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --ignore-existing "$PROJECT_DIR/backend/uploads/" "$UPLOAD_DIR/"
  else
    cp -rn "$PROJECT_DIR/backend/uploads/." "$UPLOAD_DIR/" 2>/dev/null || cp -r "$PROJECT_DIR/backend/uploads/." "$UPLOAD_DIR/"
  fi
else
  log "警告: 未找到 $PROJECT_DIR/backend/uploads,线上将缺少菜品图与收款码"
fi

# ---------------------------- 运行用户 ----------------------------
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  log "创建运行用户 $SERVICE_USER ..."
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER" || true
fi
chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"

# ---------------------------- systemd 服务 ----------------------------
log "安装 systemd 服务..."
SERVICE_SRC="$PROJECT_DIR/deploy/dining-server.service"
SERVICE_DST="/etc/systemd/system/dining-server.service"
sed -e "s|/opt/dining-system|$INSTALL_DIR|g" \
    -e "s|^User=dining|User=$SERVICE_USER|" \
    -e "s|^Group=dining|Group=$SERVICE_USER|" \
    "$SERVICE_SRC" > "$SERVICE_DST"

# 写入管理端凭据到环境文件(权限 600,仅运行用户可读),由服务的 EnvironmentFile 加载。
log "写入管理端凭据环境文件..."
ENV_FILE="/etc/dining-server.env"
umask 077
cat > "$ENV_FILE" <<EOF
ADMIN_USER=$ADMIN_USER
ADMIN_PASS=$ADMIN_PASS
EOF
chown "$SERVICE_USER:$SERVICE_USER" "$ENV_FILE" 2>/dev/null || true
chmod 600 "$ENV_FILE"

systemctl daemon-reload
systemctl enable dining-server
systemctl restart dining-server

# ---------------------------- Nginx 配置 ----------------------------
log "安装 Nginx 配置..."
NGINX_SRC="$PROJECT_DIR/deploy/nginx.conf"
NGINX_DST="/etc/nginx/conf.d/dining-system.conf"
sed -e "s|/opt/dining-system|$INSTALL_DIR|g" \
    -e "s|127.0.0.1:8080|127.0.0.1:$BACKEND_PORT|g" \
    "$NGINX_SRC" > "$NGINX_DST"

if nginx -t 2>/dev/null; then
  nginx -s reload || systemctl reload nginx
else
  warn "nginx -t 校验失败, 请手动检查 $NGINX_DST"
fi

# ---------------------------- 完成 ----------------------------
log "部署完成!"
cat <<EOF

部署信息:
  安装目录   : $INSTALL_DIR
  后端二进制 : $BIN_DIR/dining-server
  前端产物   : $FRONTEND_DIST
  上传目录   : $UPLOAD_DIR
  数据库文件 : $DB_PATH
  后端端口   : $BACKEND_PORT

常用命令:
  systemctl status dining-server        # 查看后端状态
  journalctl -u dining-server -f        # 查看后端日志
  nginx -t && nginx -s reload           # 校验并重载 Nginx

管理端账号: 用户名 $ADMIN_USER / 密码(部署时设置的 ADMIN_PASS)。
如需修改,请更新 /etc/dining-server.env 后执行 systemctl restart dining-server。
EOF
