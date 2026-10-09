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
#   --skip-build    不编译, 只用现成产物(产物缺失时报错)
#   --rebuild       强制重新编译(仓库内开发时用)
#   --no-fix-nginx  不自动修复线上遗留的 /prod-api 旧前缀(默认会自动修复)
#   默认: 有产物就直接用, 没有才编译(更新包内天然走这条路)
#
# 部署配置(统一落在 config.yaml, 后端启动时读工作目录下这个文件):
#   <INSTALL_DIR>/config.yaml —— 端口/静态目录/日志路径/数据库/管理端账号/可信代理全在这里
#     · 首次安装: 从 backend/config.prod.yaml 生成(包里没有则回退 config.example.yaml), 并校正上述各项;
#     · 覆盖更新: database 段仅在本次传了 DB_* 时重写;
#       server.static_dir / log.path / log.agent_path 只在缺失时补写(模板给过或手改过则保留);
#     · 端口: 显式传 BACKEND_PORT 时写入该值, 否则沿用文件里已有的值;
#     · 历史版本写在 /etc/dining-backend.env 的 DB_* / ADMIN_* / TRUSTED_PROXIES
#       会自动迁移进 config.yaml 并从该文件移除(旧文件备份为 .bak.<时间戳>) —— 因为
#       真实环境变量优先级高于 config.yaml, 不迁移会导致「改了 config.yaml 不生效」。
#   systemd 服务单元因此不再声明任何配置项(只留 WorkingDirectory / ExecStart / EnvironmentFile)。
#
# 切换 MySQL(config.yaml 的 database 段由此重写, 详见 docs/mysql-migration.md):
#   sudo DB_DRIVER=mysql DB_HOST=10.0.0.9 DB_USER=dining \
#     DB_PASSWORD=xxx DB_NAME=dining bash deploy/deploy.sh
#   或一行完整连接串: sudo DB_DSN='dining:pw@tcp(10.0.0.9:3306)/dining?charset=utf8mb4&parseTime=true&loc=Local&maxAllowedPacket=67108864' bash deploy/deploy.sh
#
# 产物来源(相对项目/包根目录):
#   backend/bin/dining-backend-linux-<arch>   后端二进制
#   backend/config.example.yaml               部署配置模板(首次安装生成本机 config.yaml)
#   frontend/dist/                            前端构建产物
#
# 说明: 更新时只覆盖「程序产物」(bin / dist) 与 config.yaml 的 database 段, 绝不触碰
#       data/dining.db、uploads 目录(仅作升级时存量图片导入源, 不删除、不再写入)、
#       data/master.key。
#       更新模式下还会自动修复线上遗留的 /prod-api 旧 API 前缀(可用 --no-fix-nginx 关闭)。
# ============================================================================
set -euo pipefail

# ---------------------------- 可配置项 ----------------------------
INSTALL_DIR="${INSTALL_DIR:-/opt/dining-system}"   # 安装根目录
# BACKEND_PORT 是否由调用方显式指定: 决定端口以本次传参为准, 还是沿用 config.yaml 里已有的值。
BACKEND_PORT_GIVEN=0
if [ -n "${BACKEND_PORT:-}" ]; then BACKEND_PORT_GIVEN=1; fi
BACKEND_PORT="${BACKEND_PORT:-8080}"                # 后端监听端口
SERVICE_USER="${SERVICE_USER:-dining}"              # systemd 运行用户
SERVICE_NAME="dining-backend"
# 历史遗留的环境变量文件: 新版本不再往里写配置, 但仍会清理其中会压掉 config.yaml 的键。
SERVICE_ENV_FILE="/etc/${SERVICE_NAME}.env"
NGINX_CONF="/etc/nginx/conf.d/dining-system.conf"    # Nginx 站点配置落位
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"  # 项目/包 根目录

BIN_DIR="$INSTALL_DIR/bin"
FRONTEND_DIST="$INSTALL_DIR/frontend/dist"
DB_PATH="$INSTALL_DIR/data/dining.db"
# 唯一的配置落点: 后端启动时读「当前工作目录」(= systemd WorkingDirectory)下的 config.yaml。
DEPLOY_CONFIG="$INSTALL_DIR/config.yaml"
# 生成 <INSTALL_DIR>/config.yaml 的模板: 首选 config.prod.yaml(值已填好的生产配置),
# 包里没有它时回退 config.example.yaml(全量注释模板, 如从很老的发布包升级上来)。
CONFIG_TEMPLATE="$PROJECT_DIR/backend/config.prod.yaml"
CONFIG_TEMPLATE_FALLBACK="$PROJECT_DIR/backend/config.example.yaml"
KEEP_BACKUPS="${KEEP_BACKUPS:-5}"                    # 保留最近几份备份
HEALTH_PATH="${HEALTH_PATH:-/api/customer/config}"

# ---------------------------- 参数解析 ----------------------------
# 打印脚本头部连续注释块作为帮助: 不写死行号范围, 头注释改动不会让 --help 错位。
usage() { awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "${BASH_SOURCE[0]}"; }

FORCE_REBUILD=0
SKIP_BUILD=0
FIX_NGINX_LEGACY=1
for arg in "$@"; do
  case "$arg" in
    --rebuild)      FORCE_REBUILD=1 ;;
    --skip-build)   SKIP_BUILD=1 ;;
    --no-fix-nginx) FIX_NGINX_LEGACY=0 ;;
    -h|--help)      usage; exit 0 ;;
    *) echo "未知参数: $arg (用 -h 查看用法)" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1;32m[deploy]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

# sed_inplace: 跨平台「就地编辑」。GNU sed 的 -i 可省备份后缀, 但 BSD/macOS sed
# 的 -i 必须带参数(否则会把表达式当成后缀), 直接写 `sed -i` 在 mac 上本地演练会报错。
sed_inplace() {   # $1 = sed 表达式, $2 = 目标文件
  if sed --version >/dev/null 2>&1; then
    sed -i "$1" "$2"
  else
    sed -i '' "$1" "$2"
  fi
}

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

# ---------------------------- 数据库配置(写入 config.yaml) ----------------------------
# 默认 SQLite(与代码默认一致)。传 DB_DRIVER=mysql / DB_DSN 时改写 config.yaml 的
# database 段; 不传则保持 SQLite 开箱即用。不再使用 /etc/dining-backend.env。

# db_switch_requested: 本次部署是否明确要求切换数据库(传了 DB_DRIVER=mysql 或 DB_DSN)。
db_switch_requested() {
  [ "${DB_DRIVER:-}" = "mysql" ] || [ -n "${DB_DSN:-}" ]
}

# ---------------------------- YAML 小工具 ----------------------------
# yaml_quote: 把任意字符串转成安全的 YAML 双引号标量。数据库密码里出现
# # : @ 空格 引号 都会破坏裸标量, 统一加引号并转义反斜杠/双引号最省心。
yaml_quote() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  printf '"%s"' "$s"
}

# yaml_scalar: 纯数字裸写(port 之类), 其余一律加引号。
yaml_scalar() {
  case "$1" in
    ''|*[!0-9-]*) yaml_quote "$1" ;;
    *) printf '%s' "$1" ;;
  esac
}

# replace_yaml_section: 用文件 $3 的内容替换 $1 里的顶层段 $2(如 database)。
# 顶层段 = 第 0 列的 "<name>:" 起, 到下一个第 0 列的非空非注释行为止;
# 段不存在(被删掉了)则追加到文件末尾。其余段落一字不动 —— 用户手写的
# server / security 段不会被冲掉, 段内注释也只是被脚本接管的那一段会丢。
replace_yaml_section() {
  local file="$1" name="$2" block="$3" tmp
  tmp="$(mktemp)"
  awk -v name="$name" -v blockfile="$block" '
    BEGIN { while ((getline l < blockfile) > 0) blk = blk l "\n"; close(blockfile) }
    $0 ~ ("^" name ":") { printf "%s", blk; insec = 1; found = 1; next }
    insec && /^[^[:space:]#]/ { insec = 0 }
    !insec { print }
    END { if (!found) printf "%s", blk }
  ' "$file" > "$tmp"
  cat "$tmp" > "$file"   # 覆写内容而不是替换文件: 保留原 inode 的权限与属主
  rm -f "$tmp"
  return 0
}

# db_yaml_block: 生成 config.yaml 的 database 段(输出到 stdout)。
db_yaml_block() {
  printf '# ---------------------------------------------------------------------------\n'
  printf '# 数据库后端(本段由 deploy/deploy.sh 维护: 部署时传 DB_* 会被整体重写)\n'
  printf '# ---------------------------------------------------------------------------\n'
  printf 'database:\n'
  if db_switch_requested; then
    printf '  driver: mysql\n'
    printf '  sqlite:\n'
    printf '    path: ./data/dining.db\n'
    printf '  mysql:\n'
    [ -n "${DB_DSN:-}" ]      && printf '    dsn: %s\n' "$(yaml_quote "$DB_DSN")"
    [ -n "${DB_HOST:-}" ]     && printf '    host: %s\n' "$(yaml_quote "$DB_HOST")"
    [ -n "${DB_PORT:-}" ]     && printf '    port: %s\n' "$(yaml_scalar "$DB_PORT")"
    [ -n "${DB_USER:-}" ]     && printf '    user: %s\n' "$(yaml_quote "$DB_USER")"
    [ -n "${DB_PASSWORD:-}" ] && printf '    password: %s\n' "$(yaml_quote "$DB_PASSWORD")"
    [ -n "${DB_NAME:-}" ]     && printf '    name: %s\n' "$(yaml_quote "$DB_NAME")"
    [ -n "${DB_PARAMS:-}" ]   && printf '    params: %s\n' "$(yaml_quote "$DB_PARAMS")"
  else
    printf '  driver: sqlite\n'
    printf '  sqlite:\n'
    printf '    path: ./data/dining.db\n'
    printf '  # ---- MySQL(driver 设为 mysql 时生效; 部署时传 DB_* 会自动写在这里) ----\n'
    printf '  # mysql:\n'
    printf '  #   host: 127.0.0.1\n'
    printf '  #   port: 3306\n'
    printf '  #   user: root\n'
    printf '  #   password: ""\n'
    printf '  #   name: dining\n'
  fi
  printf '\n'   # 段间留一个空行(替换时会吃掉原段尾的空行, 这里补回来)
  return 0
}

# db_yaml_admin_block: 生成 config.yaml 的 admin 段(仅首次建库读取一次)。
admin_yaml_block() {
  local pass="${ADMIN_PASS:-}"
  printf '# ---------------------------------------------------------------------------\n'
  printf '# 管理端初始账号(仅首次建库时写入 tb_user; 之后请在后台「员工管理」维护)\n'
  printf '# ---------------------------------------------------------------------------\n'
  printf 'admin:\n'
  printf '  user: %s\n' "$(yaml_quote "$ADMIN_USER")"
  [ -n "$pass" ] && printf '  pass: %s\n' "$(yaml_quote "$pass")"
  printf '\n'   # 同上: 段间留一个空行
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

# yaml_section_value: 取 config.yaml 中某顶层段内的键值(去掉引号与行尾注释)。
# $1 = 段名(database) $2 = 键名(driver / dsn)。
yaml_section_value() {
  [ -f "$DEPLOY_CONFIG" ] || return 0
  awk -v sec="$1" -v key="$2" '
    $0 ~ ("^" sec ":")        { insec = 1; next }
    insec && /^[^[:space:]#]/ { insec = 0 }
    insec && $0 ~ ("^[[:space:]]+" key ":") {
      sub("^[[:space:]]+" key ":[[:space:]]*", "")
      sub(/[[:space:]]+#.*$/, "")
      print; exit
    }
  ' "$DEPLOY_CONFIG" | tr -d '"' | tr -d "'"
  return 0
}

# yaml_has_key: 判断 config.yaml 的某个顶层段内是否已有该键(未注释)。
yaml_has_key() {
  awk -v sec="$1" -v key="$2" '
    $0 ~ ("^" sec ":")        { insec = 1; next }
    insec && /^[^[:space:]#]/ { insec = 0 }
    insec && $0 ~ ("^[[:space:]]+" key ":") { found = 1; exit }
    END { exit !found }
  ' "$DEPLOY_CONFIG"
}

# set_yaml_scalar: 把 config.yaml 里 $2 段的键 $3 设为已转义的标量 $4。
#   段内已有该键 → 替换该行; 段存在但没这个键 → 插到段首; 整段不存在 → 追加到文件末尾。
# 只动这一个键, 同段其它键与注释一概保留(与 replace_yaml_section 的整体替换互补)。
set_yaml_scalar() {
  local file="$1" sec="$2" key="$3" val="$4" tmp
  tmp="$(mktemp)"
  awk -v sec="$sec" -v key="$key" -v val="$val" '
    { line[NR] = $0 }
    END {
      s = 0; e = 0
      for (i = 1; i <= NR; i++) {
        if (!s && line[i] ~ ("^" sec ":[[:space:]]*$")) { s = i; continue }
        if (s && !e && line[i] ~ /^[^[:space:]#]/)      { e = i; break }
      }
      if (s && !e) e = NR + 1
      exists = 0
      if (s) {
        for (i = s + 1; i < e; i++) {
          if (line[i] ~ ("^[[:space:]]+" key ":")) { exists = 1; break }
        }
      }
      for (i = 1; i <= NR; i++) {
        if (s && i == s) {
          print line[i]
          if (!exists) print "  " key ": " val
          continue
        }
        if (s && exists && i > s && i < e && line[i] ~ ("^[[:space:]]+" key ":")) {
          print "  " key ": " val
          continue
        }
        print line[i]
      }
      if (!s) { print ""; print sec ":"; print "  " key ": " val }
    }
  ' "$file" > "$tmp"
  cat "$tmp" > "$file"
  rm -f "$tmp"
  return 0
}

# db_summary 输出部署总结里的「数据库后端」一行:
#   1. 本次命令传了 DB_DRIVER/DB_DSN → 用它;
#   2. 否则读 config.yaml 的 database 段(更新模式下配置可能早已写在其中);
#   3. 都没有 → SQLite。
db_summary() {
  local drv="${DB_DRIVER:-}" dsn="${DB_DSN:-}"
  if ! db_switch_requested && [ -f "$DEPLOY_CONFIG" ]; then
    drv="$(yaml_section_value database driver)"
    [ "$drv" = "mysql" ] && dsn="$(yaml_section_value database dsn)"
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
  # 配置文件一并备份: 本次若切换数据库会重写 database 段, 还会把 /etc/*.env 里的旧配置
  # 迁移过来; 回滚时必须连它一起还原, 否则会出现「二进制回滚了、配置还指着新库」。
  [ -f "$DEPLOY_CONFIG" ] && cp -a "$DEPLOY_CONFIG" "$BACKUP_DIR/config.yaml"
  [ -f "$SERVICE_ENV_FILE" ] && cp -a "$SERVICE_ENV_FILE" "$BACKUP_DIR/dining-backend.env"
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
  if [ -f "$BACKUP_DIR/config.yaml" ]; then
    install -m 0600 "$BACKUP_DIR/config.yaml" "$DEPLOY_CONFIG"
    chown "$SERVICE_USER:$SERVICE_USER" "$DEPLOY_CONFIG" 2>/dev/null || true
    warn "已还原配置文件 $DEPLOY_CONFIG(数据库配置回到更新前)"
  fi
  if [ -f "$BACKUP_DIR/dining-backend.env" ]; then
    install -m 0600 "$BACKUP_DIR/dining-backend.env" "$SERVICE_ENV_FILE"
    chown "$SERVICE_USER:$SERVICE_USER" "$SERVICE_ENV_FILE" 2>/dev/null || true
    warn "已还原 $SERVICE_ENV_FILE"
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

  # 版本信息: 发布包根部的 VERSION(含 git_commit / build_time / arch) 装到安装目录,
  # 部署后 `cat /opt/dining-system/VERSION` 即可核对本次部署的提交号。
  if [ -f "$PROJECT_DIR/VERSION" ]; then
    cp -f "$PROJECT_DIR/VERSION" "$INSTALL_DIR/VERSION"
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

# ---------------------------- 配置文件(config.yaml) ----------------------------
# import_db_from_env_file: 把历史版本写在 /etc/dining-backend.env 里的数据库配置
# 搬到本次部署的 DB_* 变量上(仅当命令行没显式给)。不做这一步就清理该文件, 服务会
# 回落成 config.yaml 里的 sqlite —— 等于把线上库悄悄切回去了。
import_db_from_env_file() {
  [ -f "$SERVICE_ENV_FILE" ] || return 0
  db_switch_requested && return 0
  local driver dsn
  driver="$(sed -n 's/^[[:space:]]*DB_DRIVER=//p' "$SERVICE_ENV_FILE" | head -1)"
  dsn="$(sed -n 's/^[[:space:]]*DB_DSN=//p' "$SERVICE_ENV_FILE" | head -1)"
  [ "$driver" = "mysql" ] || [ -n "$dsn" ] || return 0

  warn "检测到 $SERVICE_ENV_FILE 里仍有数据库配置, 正在迁移到 $DEPLOY_CONFIG ..."
  DB_DRIVER="${DB_DRIVER:-$driver}"
  DB_DSN="${DB_DSN:-$dsn}"
  DB_HOST="${DB_HOST:-$(sed -n 's/^[[:space:]]*DB_HOST=//p' "$SERVICE_ENV_FILE" | head -1)}"
  DB_PORT="${DB_PORT:-$(sed -n 's/^[[:space:]]*DB_PORT=//p' "$SERVICE_ENV_FILE" | head -1)}"
  DB_USER="${DB_USER:-$(sed -n 's/^[[:space:]]*DB_USER=//p' "$SERVICE_ENV_FILE" | head -1)}"
  DB_PASSWORD="${DB_PASSWORD:-$(sed -n 's/^[[:space:]]*DB_PASSWORD=//p' "$SERVICE_ENV_FILE" | head -1)}"
  DB_NAME="${DB_NAME:-$(sed -n 's/^[[:space:]]*DB_NAME=//p' "$SERVICE_ENV_FILE" | head -1)}"
  DB_PARAMS="${DB_PARAMS:-$(sed -n 's/^[[:space:]]*DB_PARAMS=//p' "$SERVICE_ENV_FILE" | head -1)}"
  return 0
}

# ensure_trusted_proxies: config.yaml 里没有 trusted_proxies 时补一段。
# 已有(未注释)则不动 —— 尊重用户在文件里自行配置的多行列表。
ensure_trusted_proxies() {
  local file="$1" value="${2:-127.0.0.1}" tmp
  LC_ALL=C grep -qE '^[[:space:]]*trusted_proxies:' "$file" && return 0
  tmp="$(mktemp)"
  awk -v v="$value" '
    { print }
    /^server:[[:space:]]*$/ && !done {
      print "  # 经 Nginx 反代时必须声明可信代理, 否则登录限流取不到真实客户端 IP"
      print "  trusted_proxies:"
      print "    - " v
      done = 1
    }
    END {
      if (!done) {
        print ""
        print "server:"
        print "  trusted_proxies:"
        print "    - " v
      }
    }
  ' "$file" > "$tmp"
  cat "$tmp" > "$file"
  rm -f "$tmp"
  log "已补充 server.trusted_proxies=$value"
  return 0
}

# strip_env_file: 从历史遗留的 /etc/dining-backend.env 里移除会被 config.yaml 覆盖的键。
# 真实环境变量优先级高于 config.yaml, 留着这些键用户改 config.yaml 就会「不生效」。
strip_env_file() {
  [ -f "$SERVICE_ENV_FILE" ] || return 0
  local pattern='^[[:space:]]*(export[[:space:]]+)?(DB_(DRIVER|DSN|HOST|PORT|USER|PASSWORD|NAME|PARAMS)|ADMIN_USER|ADMIN_PASS|TRUSTED_PROXIES)='
  LC_ALL=C grep -qE "$pattern" "$SERVICE_ENV_FILE" || return 0

  cp -a "$SERVICE_ENV_FILE" "$SERVICE_ENV_FILE.bak.$(date +%Y%m%d-%H%M%S)" 2>/dev/null || true
  local tmp; tmp="$(mktemp)"
  {
    printf '# 配置已统一到 %s。\n' "$DEPLOY_CONFIG"
    printf '# 历史遗留的 DB_* / ADMIN_* / TRUSTED_PROXIES 已迁移到该文件并从此处移除。\n'
    printf '# 本文件只用于放 config.yaml 不支持的项(如日志路径), 内容为空可以直接删掉。\n'
    LC_ALL=C grep -vE "$pattern" "$SERVICE_ENV_FILE" || true
  } > "$tmp"
  cat "$tmp" > "$SERVICE_ENV_FILE"
  rm -f "$tmp"
  chown "$SERVICE_USER:$SERVICE_USER" "$SERVICE_ENV_FILE" 2>/dev/null || true
  chmod 600 "$SERVICE_ENV_FILE"
  log "已把 $SERVICE_ENV_FILE 的配置键迁移到 $DEPLOY_CONFIG(原文件已备份)"
  return 0
}

# ensure_deploy_config: 保证 <INSTALL_DIR>/config.yaml 存在, 并把本次的
# database / admin / trusted_proxies 写进去。已存在时只重写被明确要求改动的段,
# 用户手写的 server / security 等段落一字不动。
ensure_deploy_config() {
  local existed=0
  [ -f "$DEPLOY_CONFIG" ] && existed=1

  local tpl="$CONFIG_TEMPLATE"
  [ -f "$tpl" ] || tpl="$CONFIG_TEMPLATE_FALLBACK"

  if [ "$existed" = "0" ]; then
    if [ -f "$tpl" ]; then
      log "生成部署配置 $DEPLOY_CONFIG (来源: $tpl)"
      cp -f "$tpl" "$DEPLOY_CONFIG"
    else
      log "生成部署配置 $DEPLOY_CONFIG (内置精简模板; 未找到 $CONFIG_TEMPLATE)"
      cat > "$DEPLOY_CONFIG" <<'EOF'
# 由 deploy/deploy.sh 生成。全部可配项与注释见 backend/config.example.yaml。
server:
  port: 8080
  trusted_proxies:
    - 127.0.0.1

database:
  driver: sqlite
  sqlite:
    path: ./data/dining.db

admin:
  user: admin
  pass: admin123
EOF
    fi
  fi

  local block
  # database 段: 新建的文件一定写; 已存在时只在本次传了 DB_* 才重写。
  if [ "$existed" = "0" ] || db_switch_requested; then
    block="$(mktemp)"
    db_yaml_block > "$block"
    replace_yaml_section "$DEPLOY_CONFIG" "database" "$block"
    rm -f "$block"
    if db_switch_requested; then
      log "已写入 database 段(MySQL) -> $DEPLOY_CONFIG"
    fi
  else
    log "保留 $DEPLOY_CONFIG 的 database 段(本次未传 DB_*)"
  fi

  # admin 段: 只在新建文件时写(仅在首次建库时读取一次, 更新不该动已有账号)。
  if [ "$existed" = "0" ]; then
    block="$(mktemp)"
    admin_yaml_block > "$block"
    replace_yaml_section "$DEPLOY_CONFIG" "admin" "$block"
    rm -f "$block"
  fi

  # ---- 运维参数(静态目录 / 日志路径 / 端口) ----
  # 静态目录: config.prod.yaml 里写的是 ./frontend/dist(相对安装目录, 目录整体迁移仍有效),
  # 这里不再动它; 只有模板没提供时(如老的 example 模板该项是注释)才补绝对路径 ——
  # 后端默认值 ../frontend/dist 是按「从 backend/ 启动」设计的, 放着不管会解析到
  # /opt/frontend/dist, 前端直接 404。
  if ! yaml_has_key server static_dir; then
    set_yaml_scalar "$DEPLOY_CONFIG" "server" "static_dir" "$(yaml_quote "$FRONTEND_DIST")"
  fi

  # 日志路径: 只在不曾配置时写入(默认 ./logs/app.log 相对工作目录其实已经正确,
  # 写出来是为了让运维一眼看到去哪儿查日志); 手改过就尊重手改。
  if ! yaml_has_key log path; then
    set_yaml_scalar "$DEPLOY_CONFIG" "log" "path" "$(yaml_quote "$INSTALL_DIR/logs/app.log")"
  fi
  if ! yaml_has_key log agent_path; then
    set_yaml_scalar "$DEPLOY_CONFIG" "log" "agent_path" "$(yaml_quote "$INSTALL_DIR/logs/agent.log")"
  fi

  # 端口: 显式传了 BACKEND_PORT 就以它为准并写入; 否则沿用 config.yaml 里已有的值,
  # 这样 Nginx 反代与健康检查用的是同一个端口, 不会出现「反代 8080、服务听 9000」。
  local configured_port
  configured_port="$(yaml_section_value server port)"
  if [ "$BACKEND_PORT_GIVEN" = "1" ] || [ -z "$configured_port" ]; then
    set_yaml_scalar "$DEPLOY_CONFIG" "server" "port" "$(yaml_scalar "$BACKEND_PORT")"
  elif [ "$configured_port" != "$BACKEND_PORT" ]; then
    log "采用 $DEPLOY_CONFIG 里的 server.port=$configured_port"
    BACKEND_PORT="$configured_port"
  fi

  ensure_trusted_proxies "$DEPLOY_CONFIG" "127.0.0.1"

  chown "$SERVICE_USER:$SERVICE_USER" "$DEPLOY_CONFIG" 2>/dev/null || true
  chmod 600 "$DEPLOY_CONFIG"   # 内含数据库密码, 与 data/master.key 同级对待
  return 0
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

  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
}

# ---------------------------- Nginx ----------------------------
# 配置只有一份来源: deploy/nginx-production.conf, 由 setup-nginx.sh 生成。
# 未指定 SERVER_NAME 时用通配 "_"(任何 Host 都匹配), 等价于过去的通用配置。
setup_nginx() {
  local dst="$NGINX_CONF"
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

# ---------------------------- 旧 Nginx 前缀自动修复(仅更新) ----------------------------
# 后端路由已从 /prod-api 迁移到 /api。本脚本只在首次安装时生成 Nginx 配置,
# 更新模式不整体覆盖线上配置(避免冲掉自定义的域名/HTTPS), 但从旧版本升级上来的
# 服务器若残留 /prod-api/, 前端所有接口会 404 —— 所以这里做「最小改动」的自动修复:
# 只把生效的 location 里的前缀换成 /api/, 先备份 + nginx -t 校验, 失败自动回滚。
# 不需要该行为可加 --no-fix-nginx(退回只告警), 或改用 setup-nginx.sh 整体重生成。
fix_nginx_legacy_prefix() {
  [ "$MODE" = "update" ] || return 0
  local conf="$NGINX_CONF" backup
  [ -f "$conf" ] || return 0
  # 只匹配生效的 location 行(行首无注释), 避免命中模板注释里的加固示例。
  grep -Eq '^[[:space:]]*location[^#]*/prod-api/' "$conf" || return 0

  warn "检测到线上 Nginx 配置仍反代旧 API 前缀 /prod-api/(后端已切换到 /api/)"
  if [ "$FIX_NGINX_LEGACY" != "1" ]; then
    warn "已按 --no-fix-nginx 跳过自动修复; 不处理会导致前端所有接口 404。手动修复:"
    warn "  sudo sed -i 's|/prod-api/|/api/|g' $conf && sudo nginx -t && sudo nginx -s reload"
    return 0
  fi

  backup="$conf.bak.prefix.$(date +%Y%m%d-%H%M%S)"
  cp -a "$conf" "$backup" || { warn "备份 $conf 失败, 跳过自动修复"; return 0; }
  sed_inplace 's|/prod-api/|/api/|g' "$conf"

  if command -v nginx >/dev/null 2>&1; then
    if ! nginx -t >/dev/null 2>&1; then
      cp -a "$backup" "$conf"
      warn "改后 nginx -t 校验失败, 已回滚 Nginx 配置(线上仍是旧配置)"
      warn "请排查后重跑: sudo nginx -t; 或整体重生成: sudo bash deploy/setup-nginx.sh --server-name <线上 server_name>"
      return 0
    fi
    if systemctl reload nginx 2>/dev/null; then
      log "Nginx 已重载"
    else
      nginx -s reload >/dev/null 2>&1 || warn "Nginx 重载失败, 请手动执行: sudo nginx -s reload"
    fi
  else
    warn "未检测到 nginx 命令, 已改配置但未校验/重载; 请在 Nginx 主机上执行 nginx -t && nginx -s reload"
  fi
  log "已自动修复 API 前缀(/prod-api -> /api), 旧配置备份: $backup"
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
import_db_from_env_file   # 旧 env 文件里的 DB 配置先搬出来(必须在写 config.yaml 之前)
ensure_deploy_config      # 生成/维护 <INSTALL_DIR>/config.yaml(唯一配置落点)
strip_env_file            # 再清掉会压掉 config.yaml 的那几个键
setup_service

if [ "$MODE" = "install" ]; then
  setup_nginx
else
  fix_nginx_legacy_prefix
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
  版本信息   : $INSTALL_DIR/VERSION  (cat 可看 git_commit / build_time / arch)
  后端二进制 : $BIN_DIR/dining-backend
  前端产物   : $FRONTEND_DIST
  配置文件   : $DEPLOY_CONFIG   ← 端口/静态目录/日志/数据库/凭据/可信代理全在这里
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
if [ "$MODE" = "install" ]; then
  cat <<EOF

管理端账号: $ADMIN_USER / 密码(部署时设置的 ADMIN_PASS)
以上已写入 $DEPLOY_CONFIG 的 admin 段
EOF
fi
cat <<EOF

改配置: 编辑 $DEPLOY_CONFIG 的对应段后 systemctl restart $SERVICE_NAME
        (下次执行本脚本时, 只有 database 段会因传入 DB_* 被重写, 其余内容保留)
EOF
exit 0
