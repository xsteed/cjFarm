#!/usr/bin/env bash
# ============================================================================
# _common.sh — 后端运维脚本的共用库(内部文件, 不直接执行)
# ----------------------------------------------------------------------------
# deploy/op/ 下各脚本第一件事就是 source 它:
#   DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   . "$DIR/_common.sh"
#
# 统一提供(避免每个脚本各抄一份, 抄歪了就是新的坑):
#   · 路径常量: INSTALL_DIR / SERVICE_NAME / SERVICE_USER / CONFIG / 备份目录
#   · 输出:     log / ok / warn / die(带颜色, 非终端自动降级)
#   · 校验:     require_root / have / confirm
#   · 配置读取: cfg_get(读 config.yaml) / db_driver / db_load_conn / mysql_q / mysql_client
#   · systemd:  service_installed / service_state / restart_service
#   · 健康检查: health_url / health_ok
#
# 约定: 只用系统命令(systemctl / curl / mysql / df ...), 不依赖发布包是否还在机器上 ——
#       服务器上只剩安装目录时, 这些脚本必须照样能用。
# ============================================================================

# 故意不加 set -e: 运维脚本要在报错后继续把剩下的检查跑完(如 p.sh), 由调用点显式判断。
set -uo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/dining-system}"
SERVICE_NAME="${SERVICE_NAME:-dining-backend}"
SERVICE_USER="${SERVICE_USER:-dining}"
CONFIG="${CONFIG:-$INSTALL_DIR/config.yaml}"
BIN_DIR="$INSTALL_DIR/bin"
# 两类备份分开放, 语义不同:
#   backups/  = backup.sh 做的**业务备份**(数据库 + 密钥 + 配置), 用于灾备/迁移
#   backup/   = deploy.sh 升级前自动做的**程序备份**, 用于回滚上一版本
DATA_BACKUP_DIR="${DATA_BACKUP_DIR:-$INSTALL_DIR/backups}"
APP_BACKUP_DIR="${APP_BACKUP_DIR:-$INSTALL_DIR/backup}"

if [ -t 1 ]; then
  C_RED=$'\033[1;31m'; C_GREEN=$'\033[1;32m'; C_YELLOW=$'\033[1;33m'
  C_CYAN=$'\033[1;36m'; C_OFF=$'\033[0m'
else
  C_RED=""; C_GREEN=""; C_YELLOW=""; C_CYAN=""; C_OFF=""
fi

log()  { printf '%s[op]%s %s\n' "$C_CYAN" "$C_OFF" "$*"; }
ok()   { printf '%s[ok]%s %s\n' "$C_GREEN" "$C_OFF" "$*"; }
warn() { printf '%s[warn]%s %s\n' "$C_YELLOW" "$C_OFF" "$*" >&2; }
die()  { printf '%s[error]%s %s\n' "$C_RED" "$C_OFF" "$*" >&2; exit 1; }

# require_root: 需要 root 的脚本(安装/卸载/还原)开头调用。
# 本地演练(非 root / 无 systemd)可设 ALLOW_NON_ROOT=1 放行 —— 与 deploy.sh 同一口径,
# 只用于开发机演练, 生产环境不要用。
require_root() {
  [ "${ALLOW_NON_ROOT:-0}" = "1" ] && return 0
  [ "$(id -u)" -eq 0 ] || die "需要 root 权限: sudo bash $0"
}

have() { command -v "$1" >/dev/null 2>&1; }

# confirm <提示语>: 交互确认。调用方可用 --yes 跳过(各脚本自行判断)。
confirm() {
  local ans
  printf '%s[?]%s %s [y/N] ' "$C_YELLOW" "$C_OFF" "$1"
  read -r ans || return 1
  case "$ans" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# ---------------------------- config.yaml 读取 ----------------------------
# cfg_get <段> <键>: 取顶层段内的标量值(去掉引号与行尾注释), 未配置时输出空串。
# 与 deploy.sh 的 yaml_value_in 同一套匹配规则(段边界 = 第 0 列的非空非注释行)。
cfg_get() {
  local sec="$1" key="$2"
  [ -f "$CONFIG" ] || return 0
  awk -v sec="$sec" -v key="$key" '
    $0 ~ ("^" sec ":")        { insec = 1; next }
    insec && /^[^[:space:]#]/ { insec = 0 }
    insec && $0 ~ ("^[[:space:]]+" key ":") {
      sub("^[[:space:]]+" key ":[[:space:]]*", "")
      sub(/[[:space:]]+#.*$/, "")
      print; exit
    }
  ' "$CONFIG" | tr -d '"' | tr -d "'"
  return 0
}

# db_driver: 当前后端(sqlite / mysql), 未配置按 sqlite(与后端默认值一致)。
db_driver() {
  local d
  d="$(cfg_get database driver)"
  printf '%s' "${d:-sqlite}"
}

# db_load_conn: 把 MySQL 连接信息读进全局 DB_HOST/DB_PORT/DB_USER/DB_PASS/DB_NAME。
#
# 为什么不 printf 一行回来再 read 拆: 密码里允许有空格, 用空格分隔必然拆错; 而
# 未加引号的 heredoc 又会把密码里的 `$` 当变量展开。全局变量最不容易出错。
# 分项配置优先; 只给了完整 dsn 时尽力解析(user:pass@tcp(host:port)/name?params)。
db_load_conn() {
  local dsn cred hostport
  DB_HOST="$(cfg_get database host)"
  DB_PORT="$(cfg_get database port)"
  DB_USER="$(cfg_get database user)"
  DB_PASS="$(cfg_get database password)"
  DB_NAME="$(cfg_get database name)"
  dsn="$(cfg_get database dsn)"
  if [ -n "$dsn" ] && [ -z "$DB_HOST" ]; then
    cred="${dsn%%@tcp(*}"                     # user:pass
    hostport="${dsn#*@tcp(}"; hostport="${hostport%%)*}"
    DB_HOST="${hostport%%:*}"; DB_PORT="${hostport##*:}"
    DB_USER="${cred%%:*}"; DB_PASS="${cred#*:}"
    DB_NAME="$(printf '%s' "$dsn" | sed -n 's|.*)/\([^?]*\).*|\1|p')"
  fi
  DB_HOST="${DB_HOST:-127.0.0.1}"
  DB_PORT="${DB_PORT:-3306}"
  DB_USER="${DB_USER:-root}"
  DB_NAME="${DB_NAME:-dining}"
  DB_PASS="${DB_PASS:-}"
}

# mysql_q <SQL>: 用 config.yaml 里的连接信息执行 SQL(不指定库, 便于查全局变量)。
# 密码走 MYSQL_PWD 而不是 -p 参数: 后者会出现在 ps 输出里, 等于把库口令贴给同机所有用户。
# 需要 mysql 客户端; 没装时返回 127(调用方据此降级为「跳过探测」而不是报错中断)。
mysql_q() {
  have mysql || return 127
  db_load_conn
  MYSQL_PWD="$DB_PASS" mysql --protocol=TCP -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" -N -B -e "$1" 2>/dev/null
}

# mysql_client: mysql 客户端可用且能连上库时返回 0。
mysql_client() {
  have mysql || return 1
  mysql_q 'SELECT 1' >/dev/null 2>&1
}

# ---------------------------- systemd ----------------------------
service_installed() {
  [ -f "/etc/systemd/system/$SERVICE_NAME.service" ] && return 0
  have systemctl || return 1
  systemctl list-unit-files "$SERVICE_NAME.service" >/dev/null 2>&1
}

service_state() {
  service_installed || { printf 'not-installed'; return 0; }
  have systemctl || { printf 'n/a'; return 0; }
  local s
  s="$(systemctl is-active "$SERVICE_NAME" 2>/dev/null | head -1 || true)"
  printf '%s' "${s:-unknown}"
}

# restart_service: 重启并做一次健康检查; 成功返回 0。
restart_service() {
  if service_installed && have systemctl; then
    systemctl restart "$SERVICE_NAME" || return 1
  else
    warn "未检测到 systemd 单元 $SERVICE_NAME, 无法重启(只能手工启动)"
    return 1
  fi
  local i
  for i in 1 2 3 4 5 6 7 8; do
    health_ok && return 0
    sleep 1
  done
  return 1
}

# ---------------------------- 健康检查 ----------------------------
# 端口取 config.yaml 的 server.port(权威来源), 兜底 8080。
health_url() {
  local port
  port="$(cfg_get server port)"
  printf 'http://127.0.0.1:%s/api/customer/config' "${port:-8080}"
}

# health_ok: 健康接口返回 200 时返回 0。
# 该接口会查库(tb_config), 因此 200 同时说明「进程活着 + 数据库可读」——
# 这是不装 mysql 客户端也能判断后端整体可用性的最省事口径。
health_ok() {
  have curl || return 1
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$(health_url)" 2>/dev/null || true)"
  [ "$code" = "200" ]
}

# ---------------------------- 杂项 ----------------------------
human_size() {
  awk -v b="${1:-0}" 'BEGIN{ split("B KB MB GB TB", u, " "); i = 1
    while (b >= 1024 && i < 5) { b /= 1024; i++ }
    printf (i == 1 ? "%.0f%s" : "%.1f%s"), b, u[i] }'
}

# file_size <路径>: 输出人类可读大小; 文件不存在输出 "-"。
file_size() {
  [ -f "$1" ] || { printf -- '-'; return 0; }
  if stat -c %s "$1" >/dev/null 2>&1; then
    human_size "$(stat -c %s "$1")"
  else
    human_size "$(stat -f %z "$1" 2>/dev/null || echo 0)"
  fi
}

# __locate_pkg_root <起点>: 在目录里找发布包根(含 deploy/deploy.sh 的目录)。
# 兼容两种形态: 解压后的 dining-<版本>-<时间戳>/ 与直接 scp -r 上来的包目录。
__locate_pkg_root() {
  local base="$1" hit
  [ -d "$base" ] || return 0
  if [ -f "$base/deploy/deploy.sh" ]; then
    printf '%s' "$(cd "$base" && pwd)"
    return 0
  fi
  hit="$(find "$base" -maxdepth 3 -path '*/deploy/deploy.sh' 2>/dev/null | head -1 || true)"
  [ -n "$hit" ] && printf '%s' "$(cd "$(dirname "$hit")/.." && pwd)"
  return 0
}

# chown_install_tree: 把安装目录交回服务用户(运维脚本以 root 写入的文件必须回手)。
chown_install_tree() {
  id -u "$SERVICE_USER" >/dev/null 2>&1 || return 0
  chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR" 2>/dev/null || true
}

op_hint() {
  printf '\n常用运维命令(在 %s 下执行):\n' "$INSTALL_DIR"
  printf '  bash op/p.sh                  # 状态一览(服务/端口/数据库/日志)\n'
  printf '  bash op/restart.sh            # 改完 config.yaml 后重启\n'
  printf '  bash op/check-health.sh       # 健康检查(可挂 cron)\n'
  printf '  bash op/backup.sh             # 备份数据库 + 密钥 + 配置\n'
  printf '  bash op/upgrade.sh <新包>     # 用新发布包升级\n'
  printf '  bash op/rollback.sh           # 回滚到上一版本\n'
}
