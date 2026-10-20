#!/usr/bin/env bash
# ============================================================================
# 点餐管理系统 - 一键发布（编译 + 打包 + 上传 + 远程部署更新）
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
#   KEEP_PACKAGES=5            # release/ 保留最近几个包(压缩包 + 未压缩目录)
#
# 切换数据库(可选): 本地设了下面任一 DB_* 就会原样透传给远程 deploy.sh,
# 由它重写 <INSTALL_DIR>/config.yaml 的 database 段(其余段落保留), 因此发一次版即可切库。
#   DB_DRIVER=mysql
#   DB_HOST=10.0.0.9
#   DB_USER=dining
#   DB_PASSWORD=xxx
#   DB_NAME=dining
# (也可只给一行连接串: DB_DSN='dining:pw@tcp(10.0.0.9:3306)/dining?charset=utf8mb4&parseTime=true&loc=Local&maxAllowedPacket=134217728')
#
# ⚠️ 切库需要额外的显式许可: sqlite -> mysql 时 deploy.sh 默认会**拒绝执行**,
#    必须再加 ALLOW_DB_SWITCH=1(它会转成 --allow-db-switch)。因为切库**不会自动搬数据**,
#    切到空库后系统会建表并灌入出厂种子, 现场表现与「数据全丢」无异。
#    正确顺序: 先按 docs/mysql-migration.md 把数据搬过去, 再 ALLOW_DB_SWITCH=1 发版切库。
#   ALLOW_DB_SWITCH=1
#
# 部署配置(config.prod.yaml)与远程 config.yaml 的关系:
#   · backend/config.prod.yaml 是权威来源, **出包时必须有**(缺失直接拒绝, 除非显式
#     ALLOW_NO_PROD_CONFIG=1 —— 那种包在服务器上只能回退 config.example.yaml=sqlite);
#   · 远程默认只把 database 段与本文件同步(其余段落保留线上值, 支持「手改 + restart」);
#   · 想让远程整份按本文件重建, 用 REFRESH_CONFIG=1(远程会先备份原 config.yaml)。
#
# 未提交改动: 工作树脏时版本号(git 短哈希)无法区分同版本号的不同代码, 出包会告警并把
# git_dirty=1 写进包内 VERSION(仍照常出包)。长期脏树发版可设 ALLOW_DIRTY_TREE=1。
#
# 产物(release/ 下同时保留两份, 结构完全一致):
#   dining-<版本>-<时间戳>.tar.gz   压缩包(默认上传给 deploy.sh 的那个)
#   dining-<版本>-<时间戳>/         未压缩的打包文件夹(可直接进目录核对 / scp -r / 手工部署)
# 内容结构(与仓库相对路径一致, 包内 deploy/deploy.sh 可直接执行):
#   ├── backend/bin/dining-backend-linux-*   # 后端二进制
#   ├── backend/bin/config.yaml              # 生产配置(由 config.prod.yaml 复制改名, 便于直接在 bin 目录启动后端)
#   ├── backend/config.prod.yaml             # 生产配置模板(本机有则带上; deploy.sh 优先据此生成 <INSTALL_DIR>/config.yaml)
#   ├── backend/config.example.yaml          # 全量注释模板(供查阅 / 老包回退)
#   ├── print-agent/bin/print-agent-*        # 门店打印代理五平台产物
#   ├── frontend/dist/                       # 前端产物
#   ├── deploy/                              # 部署物料(deploy.sh + op/ 后端运维脚本一整套, 装到 <INSTALL_DIR>/op/)
#   ├── backend/scripts/                     # 数据迁移脚本(sqlite2mysql.py, sqlite->mysql 搬数据用)
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
  KEEP_PACKAGES=5         release/ 保留最近几个包(压缩包与未压缩目录成对保留)
  DB_*                    切数据库(DB_DRIVER/DB_HOST/DB_USER/DB_PASSWORD/DB_NAME, 或一行 DB_DSN)
  ALLOW_DB_SWITCH=1       放行 sqlite -> mysql 的切库(默认拒绝: 数据不会自动搬迁)
  REFRESH_CONFIG=1        远程按包内 config.prod.yaml 整份重建 config.yaml(先备份)
  ALLOW_NO_PROD_CONFIG=1  允许在没有 backend/config.prod.yaml 的情况下出包(默认拒绝)
  ALLOW_DIRTY_TREE=1      工作树有未提交改动时只记标记、不告警(默认告警)

示例:
  ./release.sh                          # 出包
  ./release.sh root@10.0.0.5            # 一键发版到服务器
  ARCH=arm64 ./release.sh root@10.0.0.5
  REFRESH_CONFIG=1 ./release.sh root@10.0.0.5
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

# ---------------------------- 前端版本号自动递增 ----------------------------
# 发版时把后台管理系统(admin 前端)的 package.json version 做 patch+1,
# 左下角展示的版本号据此自动递增; 与后端运行期版本(git hash)解耦, 互不影响。
# 默认只改动文件不提交; 设 COMMIT_VERSION=1 则顺手提交这个 bump。
bump_admin_version() {
  local pkg="$ROOT_DIR/frontend/apps/admin/package.json"
  ADMIN_VERSION=""
  [ -f "$pkg" ] || { warn "未找到 $pkg, 跳过前端版本号递增"; return 0; }
  command -v node >/dev/null 2>&1 || { warn "未找到 node, 跳过前端版本号递增"; return 0; }
  ADMIN_VERSION="$(node -e '
    const fs=require("fs");
    const p=process.argv[1];
    const s=fs.readFileSync(p,"utf8");
    const m=s.match(/"version"\s*:\s*"(\d+)\.(\d+)\.(\d+)"/);
    if(!m){ process.stderr.write("package.json 缺少合法 version 字段\n"); process.exit(2); }
    const nv=`${m[1]}.${m[2]}.${Number(m[3])+1}`;
    fs.writeFileSync(p, s.replace(/"version"\s*:\s*"\d+\.\d+\.\d+"/, `"version": "${nv}"`));
    process.stdout.write(nv);
  ' "$pkg")" || die "前端版本号递增失败"
  log "前端(admin)版本号已递增 -> v$ADMIN_VERSION"
  if [ "${COMMIT_VERSION:-}" = "1" ]; then
    git -C "$ROOT_DIR" add "$pkg"
    git -C "$ROOT_DIR" commit -m "chore(admin): bump version to v$ADMIN_VERSION" \
      && log "已提交版本号 bump" \
      || warn "版本号 bump 提交失败(可能无改动或无提交权限)"
  fi
}

case "$ARCH" in
  amd64|arm64|both) ;;
  *) die "ARCH 只能是 amd64 / arm64 / both, 当前: $ARCH" ;;
esac

# ---------------------------- 生产配置必须带上 ----------------------------
# backend/config.prod.yaml 是部署配置的**权威来源**: 服务器上 deploy.sh 以它为模板生成
# <INSTALL_DIR>/config.yaml(首装照抄、之后每次同步 database 段)。缺了它, deploy.sh 只能
# 回退 config.example.yaml(sqlite), 现场表现就是「明明配了 mysql, 部署完还是 sqlite」。
# 这种包不该被生产出来, 所以默认直接拒绝; 确实要出「不含生产配置」的包(如给人试跑)
# 才显式放行。
# 放在编译之前校验: 免得 Go/前端编译几分钟后才发现包里没这个文件, 白忙一场。
if [ ! -f "$ROOT_DIR/backend/config.prod.yaml" ] && [ "${ALLOW_NO_PROD_CONFIG:-}" != "1" ]; then
  die "未找到 backend/config.prod.yaml —— 它是部署配置的权威来源, 缺了它服务器上会
     回退 config.example.yaml(sqlite), 把线上库配成 sqlite。
     请先创建: cp backend/config.example.yaml backend/config.prod.yaml 后按需填写;
     确实要出不含生产配置的包, 请显式: ALLOW_NO_PROD_CONFIG=1 ./release.sh"
fi

VERSION="${VERSION:-$(cd "$ROOT_DIR" && git rev-parse --short HEAD 2>/dev/null || echo dev)}"
VERSION="$(printf '%s' "$VERSION" | tr -c 'A-Za-z0-9._-' '-')"
BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
TS="$(date +%Y%m%d-%H%M%S)"
PKG_NAME="dining-${VERSION}-${TS}"
TARBALL="$PKG_NAME.tar.gz"

# ---------------------------- 工作树是否干净 ----------------------------
# VERSION 只是 HEAD 的短哈希: 工作树有未提交改动时, 同一个版本号可能对应两批不同代码,
# 事后排查(「线上跑的是哪份代码」)就失去了依据 —— 把线上 tar 包解开也比不出差异。
# 这里告警, 并把脏标记写进包内 VERSION 文件(git_dirty=1)供事后核对。
# 团队里长期带未提交改动发版时, 可用 ALLOW_DIRTY_TREE=1 关掉这条告警(标记照写)。
GIT_DIRTY=0
if command -v git >/dev/null 2>&1 && [ -d "$ROOT_DIR/.git" ]; then
  DIRTY_FILES="$(git -C "$ROOT_DIR" status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
  if [ "${DIRTY_FILES:-0}" != "0" ]; then
    GIT_DIRTY=1
    if [ "${ALLOW_DIRTY_TREE:-}" = "1" ]; then
      log "工作树有 $DIRTY_FILES 处未提交改动(已按 ALLOW_DIRTY_TREE=1 静默, VERSION 仍记 git_dirty=1)"
    else
      warn "工作树有 $DIRTY_FILES 处未提交改动: 版本号 $VERSION 只代表 HEAD, 无法区分同版本号的"
      warn "不同代码。包内 VERSION 已记 git_dirty=1; 想让包可追溯请先提交(或 ALLOW_DIRTY_TREE=1 静默)。"
    fi
  fi
fi

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

# 打包暂存目录直接建在 release/ 下, 并且**完成后保留** —— 它就是「未压缩的打包文件夹」,
# 可直接进目录核对内容、scp -r 整个目录、或在本机手工跑 deploy/deploy.sh 演练。
# 早先用 mktemp + trap 删除, 只能拿到 tar.gz, 想看一眼包内结构还得先解压一份出来。
# 失败中断时由 KEEP_PKG_DIR 决定: 未置 1 说明包没打成, 清掉半成品避免被误当成有效包。
PKG_DIR="$OUT_DIR/$PKG_NAME"
KEEP_PKG_DIR=0
trap 'if [ "$KEEP_PKG_DIR" != "1" ]; then rm -rf "$PKG_DIR"; fi' EXIT
rm -rf "$PKG_DIR"   # 同一秒内重跑会命中同名目录, 先清干净再建
mkdir -p "$PKG_DIR/backend/bin" "$PKG_DIR/frontend" "$PKG_DIR/deploy" \
  "$PKG_DIR/print-agent/bin" "$PKG_DIR/print-agent/deploy" "$PKG_DIR/print-agent/op"

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

# ---------------------------- 编译门店打印代理 ----------------------------
build_print_agent() {
  local goos="$1" goarch="$2" suffix="$3"
  log "编译门店打印代理 ($goos/$goarch) ..."
  ( cd "$ROOT_DIR/print-agent"
    GOTOOLCHAIN=local CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      "$GO_BIN" build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME -X main.gitCommit=$VERSION" \
      -o "$PKG_DIR/print-agent/bin/print-agent-$suffix" . )
}
build_print_agent linux amd64 linux-amd64
build_print_agent linux arm64 linux-arm64
build_print_agent windows amd64 windows-amd64.exe
build_print_agent darwin amd64 darwin-amd64
build_print_agent darwin arm64 darwin-arm64

# ---------------------------- 构建前端 ----------------------------
# 发版时先把 admin 前端版本号 patch+1, 再构建(左下角读 package.json 的 version)
bump_admin_version
log "构建前端 ..."
( cd "$ROOT_DIR/frontend"
  [ -d node_modules ] || npm install --registry="$NPM_REGISTRY" --no-audit --no-fund
  npm run build )
[ -f "$ROOT_DIR/frontend/dist/index.html" ] || die "前端构建失败: 缺少 frontend/dist/index.html"

# ---------------------------- 组装 ----------------------------
copy_tree() {
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --exclude='.DS_Store' --exclude='__pycache__' --exclude='*.pyc' "$1/" "$2/"
  else
    mkdir -p "$2"; cp -a "$1/." "$2/"
    find "$2" -name '.DS_Store' -delete 2>/dev/null || true
    # Python 侧的缓存目录(__pycache__/*.pyc)不该进交付包(backend/scripts 里有 .py 脚本)
    find "$2" -name '__pycache__' -type d -prune -exec rm -rf {} + 2>/dev/null || true
    find "$2" -name '*.pyc' -delete 2>/dev/null || true
  fi
}

copy_tree "$ROOT_DIR/frontend/dist" "$PKG_DIR/frontend/dist"
# 部署配置: config.prod.yaml 是本机私有文件(不入库), 有则带上 —— deploy.sh 首次安装
# 优先据此生成 <INSTALL_DIR>/config.yaml; 没有它时会回退 config.example.yaml(一并带上)。
if [ -f "$ROOT_DIR/backend/config.prod.yaml" ]; then
  cp -f "$ROOT_DIR/backend/config.prod.yaml" "$PKG_DIR/backend/config.prod.yaml"
  # 一并复制进 bin/ 并改名为 config.yaml, 便于直接进入 bin 目录启动后端
  # (后端读工作目录下的 config.yaml)。deploy.sh 更新时不会动 bin/ 里的这个文件。
  cp -f "$ROOT_DIR/backend/config.prod.yaml" "$PKG_DIR/backend/bin/config.yaml"
else
  # 走到这里说明调用方显式设了 ALLOW_NO_PROD_CONFIG=1(否则上面校验处已 die)。
  warn "未找到 backend/config.prod.yaml: 已按 ALLOW_NO_PROD_CONFIG=1 继续出包,"
  warn "该包不含生产配置, 服务器上 deploy.sh 会回退 config.example.yaml(sqlite)。"
fi
cp -f "$ROOT_DIR/backend/config.example.yaml" "$PKG_DIR/backend/config.example.yaml"

for f in deploy.sh setup-nginx.sh dining-backend.service nginx-production.conf; do
  [ -f "$ROOT_DIR/deploy/$f" ] && cp -f "$ROOT_DIR/deploy/$f" "$PKG_DIR/deploy/"
done
# 后端运维脚本一整套(deploy/op/ -> 包内 deploy/op/ -> deploy.sh 装到 <INSTALL_DIR>/op/)
copy_tree "$ROOT_DIR/deploy/op" "$PKG_DIR/deploy/op"
# 数据迁移脚本: deploy.sh 的切库拦截提示与 docs/mysql-migration.md 都指向
# <发布包>/backend/scripts/sqlite2mysql.py —— 不打包的话, 服务器上按提示做会找不到文件。
[ -d "$ROOT_DIR/backend/scripts" ] && copy_tree "$ROOT_DIR/backend/scripts" "$PKG_DIR/backend/scripts"
# 打印代理物料与源码同目录,发布包里也归到 print-agent/ 下(自启动模板 + 运维脚本 + 配置模板)。
for f in print-agent.service print-agent.plist print-agent-windows-task.xml; do
  [ -f "$ROOT_DIR/print-agent/deploy/$f" ] && cp -f "$ROOT_DIR/print-agent/deploy/$f" "$PKG_DIR/print-agent/deploy/"
done
copy_tree "$ROOT_DIR/print-agent/op" "$PKG_DIR/print-agent/op"
cp -f "$ROOT_DIR/print-agent/agent.env.example" "$PKG_DIR/print-agent/"
cp -f "$ROOT_DIR/print-agent/门店安装说明.md" "$PKG_DIR/print-agent/"
cp -f "$ROOT_DIR/print-agent/README.md" "$PKG_DIR/print-agent/"
chmod +x "$PKG_DIR/deploy"/*.sh "$PKG_DIR/deploy/op"/*.sh 2>/dev/null || true

cat > "$PKG_DIR/VERSION" <<EOF
version=$VERSION
build_time=$(TZ='Asia/Shanghai' date '+%Y-%m-%d %H:%M:%S')
arch=$ARCH
git_dirty=$GIT_DIRTY
dirty_files=${DIRTY_FILES:-0}
EOF

# macOS 打包: COPYFILE_DISABLE=1 只挡 ._* (AppleDouble) 冗余文件, 挡不住扩展属性。
# bsdtar 默认把 xattr 写进 pax 扩展头(LIBARCHIVE.xattr.com.apple.provenance 等),
# Linux 端 GNU tar 解压时就会逐文件告警 "Ignoring unknown extended header keyword"。
# 因此显式 --no-xattrs 让包内不带 xattr。个别 tar 不支持该选项时先探测, 不支持则回退并告警。
export COPYFILE_DISABLE=1
TAR_XATTR_OPT="--no-xattrs"
_probe_dir="$(mktemp -d)"
: > "$_probe_dir/x"
if ! tar --no-xattrs -cf /dev/null -C "$_probe_dir" x 2>/dev/null; then
  TAR_XATTR_OPT=""
  warn "当前 tar 不支持 --no-xattrs, 包内可能残留 macOS xattr 扩展头(解压时的告警可忽略)"
fi
rm -rf "$_probe_dir"
mkdir -p "$OUT_DIR"
tar -C "$OUT_DIR" -czf "$OUT_DIR/$TARBALL" $TAR_XATTR_OPT "$PKG_NAME"
# 包已打成: 允许保留未压缩目录(只有失败路径上的 trap 才会清它)。
KEEP_PKG_DIR=1

# 只保留最近 N 个包, 避免 release/ 无限膨胀 —— 压缩包与未压缩目录**成对清理**:
# 只删 tar.gz 会让未压缩目录一天天堆下来, 二进制+前端每次 100MB 级, 很快吃满磁盘。
ls -1t "$OUT_DIR"/dining-*.tar.gz 2>/dev/null | tail -n +$((KEEP_PACKAGES + 1)) | while read -r old; do
  rm -f "$old"
  rm -rf "${old%.tar.gz}"
done

SIZE="$(du -h "$OUT_DIR/$TARBALL" | cut -f1)"
SIZE_DIR="$(du -sh "$PKG_DIR" | cut -f1)"
log "打包完成: release/$TARBALL ($SIZE, $(cd "$PKG_DIR" && find . -type f | wc -l | tr -d ' ') 个文件)"
log "未压缩目录: release/$PKG_NAME/ ($SIZE_DIR, 可直接进目录核对或手工部署)"

# ---------------------------- 上传 + 远程部署 ----------------------------
if [ -z "$SERVER" ]; then
  cat <<EOF

未配置服务器地址, 仅完成打包。release/ 下同时留两份, 按需取用:
  · 压缩包    : release/$TARBALL  ($SIZE)
  · 未压缩目录: release/$PKG_NAME/  ($SIZE_DIR)

用法二选一:
  1) 手动上传 —— 传压缩包(省带宽)或整份目录:
     压缩包(先清同名残留, 否则解压会与旧目录混合):
       scp -P $SSH_PORT release/$TARBALL 用户@服务器:/tmp/
       ssh 用户@服务器 "rm -rf /tmp/$PKG_NAME && cd /tmp && tar -xzf $TARBALL && cd $PKG_NAME && sudo bash deploy/deploy.sh --skip-build"
     已解压目录(scp -r 会与 /tmp 下的同名旧目录合并, 同样先清):
       ssh 用户@服务器 "rm -rf /tmp/$PKG_NAME"
       scp -P $SSH_PORT -r release/$PKG_NAME 用户@服务器:/tmp/
       ssh 用户@服务器 "cd /tmp/$PKG_NAME && sudo bash deploy/deploy.sh --skip-build"
  2) 配置 .release.env 后直接 ./release.sh 一键到底:
       SERVER=用户@服务器
EOF
  exit 0
fi

command -v scp >/dev/null 2>&1 || die "缺少 scp"
# 上传前先清掉服务器 /tmp 上的同名解压目录: 不清的话 tar 会把新包覆盖进旧目录,
# 旧版本多出来的文件会残留在里面, 解出来的包 ≠ 本地打包结果。
# 顺带先连一次服务器, 地址/密钥不对立刻失败, 不必先白传 30MB 再说连不上。
# ⚠️ 清理必须在上传**之前**: 早先把它写在远程解压前(`rm -rf /tmp/$TARBALL`),
#    等于删掉了刚 scp 上去的包, 紧接着的 tar -xzf 必然报
#    "Cannot open: No such file or directory", 一键发版 (`./release.sh user@server`)
#    100% 失败, 只有手动上传那条路能用。
log "清理 $SERVER:/tmp 上的旧残留 ..."
ssh -p "$SSH_PORT" "$SERVER" "rm -rf '/tmp/$PKG_NAME'"
log "上传到 $SERVER:/tmp/ ..."
scp -P "$SSH_PORT" -q "$OUT_DIR/$TARBALL" "$SERVER:/tmp/$TARBALL"

# 数据库配置透传(可选): 本地给了 DB_* 就原样交给远程 deploy.sh, 由它重写
# <INSTALL_DIR>/config.yaml 的 database 段(其余内容保留), 实现「发一次版即切库」。
# 用 printf %q 转义, 避免密码里的 @ : / 空格 等把远程命令拆坏。
REMOTE_VARS=""
for _k in DB_DRIVER DB_DSN DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_PARAMS; do
  _v="${!_k:-}"
  [ -n "$_v" ] && REMOTE_VARS="$REMOTE_VARS $_k=$(printf '%q' "$_v")"
done
[ -n "$REMOTE_VARS" ] && log "将透传数据库配置(DB_*)到远程 deploy.sh"

# 切库需要额外的显式许可: sqlite -> mysql 时 deploy.sh 默认拒绝(数据不会自动搬迁,
# 切到空库会建表并灌入出厂种子, 现场表现与「数据全丢」无异)。
# 想真的切就带上 ALLOW_DB_SWITCH=1。
DEPLOY_EXTRA_ARGS=""
if [ "${ALLOW_DB_SWITCH:-}" = "1" ]; then
  DEPLOY_EXTRA_ARGS=" --allow-db-switch"
elif [ -n "$REMOTE_VARS" ]; then
  warn "本次透传了 DB_*: 若这是 sqlite -> mysql 的切换, 远程 deploy.sh 会拒绝执行(默认防误切空库)。"
  warn "确认新库已搬完数据后, 用 ALLOW_DB_SWITCH=1 ./release.sh 重跑。"
fi

# REFRESH_CONFIG=1: 让远程按包内 backend/config.prod.yaml **整份重建** <INSTALL_DIR>/config.yaml
# (远程 deploy.sh 会先把原文件备份成 config.yaml.bak.<时间戳>)。
# 不加时远程只同步 database 段(见 deploy.sh 头部): 手改过的 trusted_proxies / log.level /
# server.port 会保留 —— 这通常正是想要的, 所以整体重建必须显式要求。
if [ "${REFRESH_CONFIG:-}" = "1" ]; then
  if [ -f "$ROOT_DIR/backend/config.prod.yaml" ]; then
    DEPLOY_EXTRA_ARGS="$DEPLOY_EXTRA_ARGS --refresh-config"
    log "远程将按包内 config.prod.yaml 整份重建 config.yaml(原文件会被备份)"
  else
    warn "REFRESH_CONFIG=1 但包内没有 backend/config.prod.yaml, 已忽略(远程没有可重建的来源)"
  fi
fi

log "远程执行 deploy.sh (安装或更新, 自动识别) ..."
# -t 分配 TTY, 便于远程 sudo 提示输入密码
ssh -p "$SSH_PORT" -t "$SERVER" "set -e
  rm -rf '/tmp/$PKG_NAME'
  cd /tmp && tar -xzf '$TARBALL'
  cd '/tmp/$PKG_NAME' && sudo INSTALL_DIR='$INSTALL_DIR'$REMOTE_VARS bash deploy/deploy.sh --skip-build$DEPLOY_EXTRA_ARGS"

log "发布完成: $VERSION -> $SERVER:$INSTALL_DIR"
