#!/usr/bin/env bash
# ============================================================================
# 扫码点餐管理系统 - 一键发布（编译 + 打包 + 上传 + 远程部署更新）
#
# 用法:
#   ./release.sh                     # 只打包, 产物落在 release/
#   ./release.sh user@server         # 打包 + 上传 + 远程执行安装/更新
#   SERVER=user@server ./release.sh  # 同上(适合写进 .release.env)
#
# 推荐: 把服务器信息写进项目根目录的 .release.env(已被 .gitignore 忽略),
#       之后每次发版只需要跑一句 ./release.sh, 全程无参数。
#
#   # .release.env
#   SERVER=user@1.2.3.4
#   INSTALL_DIR=/opt/dining-system
#   ARCH=amd64                 # amd64 | arm64 | both
#   SSH_PORT=22
#
# 产物结构(与仓库相对路径一致, 包内 deploy/deploy.sh 可直接执行):
#   dining-<版本>-<时间戳>/
#   ├── backend/bin/dining-backend-linux-*   # 后端二进制
#   ├── backend/uploads/                     # 种子图
#   ├── frontend/dist/                       # 前端产物
#   ├── deploy/                              # 部署物料(含 deploy.sh)
#   └── VERSION
# ============================================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 本地配置(不入库), 先读以便被命令行/环境变量覆盖
ENV_FILE="$ROOT_DIR/.release.env"
# shellcheck disable=SC1090
[ -f "$ENV_FILE" ] && . "$ENV_FILE"

SERVER="${SERVER:-}"
INSTALL_DIR="${INSTALL_DIR:-/opt/dining-system}"
ARCH="${ARCH:-amd64}"
SSH_PORT="${SSH_PORT:-22}"
NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmmirror.com}"
OUT_DIR="$ROOT_DIR/release"
KEEP_PACKAGES="${KEEP_PACKAGES:-5}"

usage() {
  cat <<'EOF'
用法: ./release.sh [user@server]

  (无参数)        只编译打包, 产物输出到 release/
  user@server     打包后上传并远程执行 deploy/deploy.sh(安装或更新)

可写进 .release.env 的配置:
  SERVER=user@1.2.3.4     服务器地址(留空则只打包)
  INSTALL_DIR=/opt/dining-system
  ARCH=amd64|arm64|both   默认 amd64
  SSH_PORT=22

示例:
  ./release.sh                          # 出包
  ./release.sh root@10.0.0.5            # 一键发版到服务器
  ARCH=arm64 ./release.sh root@10.0.0.5
EOF
}

for arg in "$@"; do
  case "$arg" in
    -h|--help) usage; exit 0 ;;
    *) [ -n "${SERVER_OVERRIDE:-}" ] && { echo "参数过多: $arg" >&2; exit 2; }
       SERVER="$arg"; SERVER_OVERRIDE=1 ;;
  esac
done

log()  { printf '\033[1;36m[release]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

case "$ARCH" in
  amd64|arm64|both) ;;
  *) die "ARCH 只能是 amd64 / arm64 / both, 当前: $ARCH" ;;
esac

VERSION="${VERSION:-$(cd "$ROOT_DIR" && git rev-parse --short HEAD 2>/dev/null || echo dev)}"
VERSION="$(printf '%s' "$VERSION" | tr -c 'A-Za-z0-9._-' '-')"
BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
TS="$(date +%Y%m%d-%H%M%S)"
PKG_NAME="dining-${VERSION}-${TS}"
TARBALL="$PKG_NAME.tar.gz"

# ---------------------------- 定位 Go(>=1.23) ----------------------------
go_version_ok() {
  local bin="$1" v
  command -v "$bin" >/dev/null 2>&1 || return 1
  v="$("$bin" env GOVERSION 2>/dev/null | sed 's/^go//')" || return 1
  [ -n "$v" ] || return 1
  awk -v a="$v" -v b="1.23" 'BEGIN{split(a,x,".");split(b,y,".");exit !((x[1]+0>y[1]+0)||((x[1]+0==y[1]+0)&&(x[2]+0>=y[2]+0)))}'
}
GO_BIN=""
for cand in go "$HOME/sdk/go/bin/go" /usr/local/go/bin/go; do
  if go_version_ok "$cand"; then GO_BIN="$(command -v "$cand")"; break; fi
done
[ -n "$GO_BIN" ] || die "未找到 Go >= 1.23(go.mod 要求 1.23)。安装: brew install go, 或解压到 ~/sdk/go"
log "Go: $GO_BIN ($("$GO_BIN" env GOVERSION))  |  架构: $ARCH  |  版本: $VERSION"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
PKG_DIR="$STAGE/$PKG_NAME"
mkdir -p "$PKG_DIR/backend/bin" "$PKG_DIR/frontend" "$PKG_DIR/deploy"

# ---------------------------- 编译后端 ----------------------------
build_backend() {
  log "编译后端 (linux/$1) ..."
  ( cd "$ROOT_DIR/backend"
    GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH="$1" \
      "$GO_BIN" build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME -X main.gitCommit=$VERSION" \
      -o "$PKG_DIR/backend/bin/dining-backend-linux-$1" . )
}
if [ "$ARCH" = "both" ]; then
  build_backend amd64
  build_backend arm64
else
  build_backend "$ARCH"
fi

# ---------------------------- 构建前端 ----------------------------
log "构建前端 ..."
( cd "$ROOT_DIR/frontend"
  [ -d node_modules ] || npm install --registry="$NPM_REGISTRY" --no-audit --no-fund
  npm run build )
[ -f "$ROOT_DIR/frontend/dist/index.html" ] || die "前端构建失败: 缺少 frontend/dist/index.html"

# ---------------------------- 组装 ----------------------------
copy_tree() {
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --exclude='.DS_Store' "$1/" "$2/"
  else
    mkdir -p "$2"; cp -a "$1/." "$2/"; find "$2" -name '.DS_Store' -delete 2>/dev/null || true
  fi
}

copy_tree "$ROOT_DIR/frontend/dist" "$PKG_DIR/frontend/dist"
[ -d "$ROOT_DIR/backend/uploads" ] || die "缺少 backend/uploads(菜品图与收款码), 线上会 404"
copy_tree "$ROOT_DIR/backend/uploads" "$PKG_DIR/backend/uploads"

for f in deploy.sh setup-nginx.sh check-health.sh dining-backend.service nginx-production.conf; do
  [ -f "$ROOT_DIR/deploy/$f" ] && cp -f "$ROOT_DIR/deploy/$f" "$PKG_DIR/deploy/"
done
chmod +x "$PKG_DIR/deploy"/*.sh 2>/dev/null || true

cat > "$PKG_DIR/VERSION" <<EOF
version=$VERSION
build_time=$BUILD_TIME
arch=$ARCH
EOF

export COPYFILE_DISABLE=1   # macOS: 避免打包出 ._* 冗余文件
mkdir -p "$OUT_DIR"
tar -C "$STAGE" -czf "$OUT_DIR/$TARBALL" "$PKG_NAME"

# 只保留最近 N 个包, 避免 release/ 无限膨胀
ls -1t "$OUT_DIR"/dining-*.tar.gz 2>/dev/null | tail -n +$((KEEP_PACKAGES + 1)) | while read -r old; do
  rm -f "$old"
done

SIZE="$(du -h "$OUT_DIR/$TARBALL" | cut -f1)"
log "打包完成: release/$TARBALL ($SIZE, $(cd "$PKG_DIR" && find . -type f | wc -l | tr -d ' ') 个文件)"

# ---------------------------- 上传 + 远程部署 ----------------------------
if [ -z "$SERVER" ]; then
  cat <<EOF

未配置服务器地址, 仅完成打包。两种用法二选一:
  1) 手动上传:  scp -P $SSH_PORT release/$TARBALL 用户@服务器:/tmp/
                ssh 用户@服务器 "cd /tmp && tar -xzf $TARBALL && cd $PKG_NAME && sudo bash deploy/deploy.sh --skip-build"
  2) 配置 .release.env 后直接 ./release.sh 一键到底:
       SERVER=用户@服务器
EOF
  exit 0
fi

command -v scp >/dev/null 2>&1 || die "缺少 scp"
log "上传到 $SERVER:/tmp/ ..."
scp -P "$SSH_PORT" -q "$OUT_DIR/$TARBALL" "$SERVER:/tmp/$TARBALL"

log "远程执行 deploy.sh (安装或更新, 自动识别) ..."
# -t 分配 TTY, 便于远程 sudo 提示输入密码
ssh -p "$SSH_PORT" -t "$SERVER" "set -e
  rm -rf '/tmp/$PKG_NAME' '/tmp/$TARBALL'
  cd /tmp && tar -xzf '$TARBALL'
  cd '/tmp/$PKG_NAME' && sudo INSTALL_DIR='$INSTALL_DIR' bash deploy/deploy.sh --skip-build"

log "发布完成: $VERSION -> $SERVER:$INSTALL_DIR"
