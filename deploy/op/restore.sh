#!/usr/bin/env bash
# 从 backup.sh 的备份里还原 —— **会覆盖现网数据**: 数据库 + 密钥 + 配置。
#
# 用法:
#   sudo bash op/restore.sh <备份目录> [--yes] [--data-only | --config-only]
#
#   <备份目录>   backup.sh 生成的 <INSTALL_DIR>/backups/<时间戳>/(含 MANIFEST)
#   --yes        跳过交互确认(配合自动化; 二次确认仍会做一次现网备份)
#   --data-only  只还原数据库(不动密钥/配置)
#   --config-only只还原 config.yaml 与两把密钥(不动数据库)
#
# 安全设计:
#   1) 动手前**自动再备份一次现网**(调 backup.sh) —— 还原错了还有退路;
#   2) 默认交互确认, 并把将覆盖的内容列出来;
#   3) 先停服务再改文件, 最后按 config.yaml 重启并做健康检查;
#   4) 与 rollback.sh 的分工: 本脚本动**数据**, rollback.sh 只回滚**程序**。
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

usage() {
  cat <<'EOF'
用法: sudo bash op/restore.sh <备份目录> [--yes] [--data-only | --config-only]

  备份目录: op/backup.sh 生成的 <INSTALL_DIR>/backups/<时间戳>(必须含 MANIFEST)
EOF
}

require_root

[ $# -ge 1 ] || { usage; exit 2; }
src="${1%/}"; shift
YES=0
MODE="all"
while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=1; shift ;;
    --data-only) MODE="data"; shift ;;
    --config-only) MODE="config"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数: $1 (用 -h 查看用法)" ;;
  esac
done

[ -d "$src" ] || die "备份目录不存在: $src"
[ -f "$src/MANIFEST" ] || warn "$src 里没有 MANIFEST(不是 op/backup.sh 生成的?)"
if [ "$MODE" != "config" ] && [ ! -f "$src/db.sql" ] && [ ! -f "$src/dining.db" ]; then
  die "$src 里没有 db.sql / dining.db, 无法还原数据库(可加 --config-only 只还原配置与密钥)"
fi

echo "将用以下备份覆盖现网:"
printf '  备份   : %s\n' "$src"
[ -f "$src/MANIFEST" ] && sed 's/^/  /' "$src/MANIFEST"
case "$MODE" in
  all)    printf '  覆盖   : 数据库 + data/master.key + data/auth.key + config.yaml + VERSION\n' ;;
  data)   printf '  覆盖   : 数据库\n' ;;
  config) printf '  覆盖   : config.yaml + data/master.key + data/auth.key + VERSION\n' ;;
esac
if [ "$YES" != "1" ]; then
  confirm '确认覆盖现网?(此操作不可撤销)' || { log '已取消'; exit 0; }
fi

# ---- 0. 先自动备份现网(还原错了还有退路) ----
log '还原前先自动备份现网 ...'
if bash "$DIR/backup.sh" >/dev/null 2>&1; then
  log "现网已备份到 $(ls -1dt "$DATA_BACKUP_DIR"/*/ 2>/dev/null | head -1 | tr -d '\n')"
else
  warn '现网自动备份不完整; 继续还原后请立刻手工核对'
fi

# ---- 1. 停服务 ----
log '停止服务 ...'
service_installed && systemctl stop "$SERVICE_NAME" \
  || warn '停止服务失败/未安装(继续还原, 稍后需手工启动)'

# ---- 2. 还原 ----
restore_file() {   # restore_file <备份内文件名> <目标路径> <权限>
  local s="$src/$1" t="$2"
  [ -f "$s" ] || { warn "备份里没有 $1, 跳过"; return 0; }
  install -m "$3" "$s" "$t" || die "写入失败: $t"
  ok "已还原 $1 -> $t"
}

case "$MODE" in
  all|config)
    # data/ 可能还不存在(极端场景: 备份还原到全新目录), install 不会自建父目录
    mkdir -p "$INSTALL_DIR/data"
    restore_file config.yaml "$CONFIG" 0600
    restore_file master.key "$INSTALL_DIR/data/master.key" 0600
    restore_file auth.key "$INSTALL_DIR/data/auth.key" 0600
    restore_file VERSION "$INSTALL_DIR/VERSION" 0644
    ;;
esac

case "$MODE" in
  all|data)
    DRV="$(db_driver)"
    if [ "$DRV" = "mysql" ]; then
      have mysql || die '当前后端是 MySQL, 但本机没有 mysql 客户端, 无法导入(可装 mysql-client 后重试)'
      db_load_conn
      log "导入 MySQL $DB_NAME@$DB_HOST:$DB_PORT ..."
      # 若报 ER_NET_PACKET_TOO_LARGE: 先在服务端 SET GLOBAL max_allowed_packet=134217728;
      # (tb_image 单行可达数 MB, 导入包过大会在这里失败)
      if MYSQL_PWD="$DB_PASS" mysql --protocol=TCP -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" "$DB_NAME" < "$src/db.sql"; then
        ok '数据库已导入'
      else
        die "导入失败。服务仍处于停止状态; 排查后重新执行本脚本(数据未被部分改动的部分可用 mysql 客户端逐表核对)"
      fi
    elif [ -f "$src/dining.db" ]; then
      mkdir -p "$INSTALL_DIR/data"
      install -m 0644 "$src/dining.db" "$INSTALL_DIR/data/dining.db"
      rm -f "$INSTALL_DIR/data/dining.db-wal" "$INSTALL_DIR/data/dining.db-shm"
      ok 'SQLite 文件已还原(-wal/-shm 已清理, 与备份时点一致)'
    else
      warn '备份里没有数据库文件, 跳过数据还原'
    fi
    ;;
esac

# ---- 3. 属主 + 启动 ----
chown_install_tree
log '启动服务 ...'
if restart_service; then
  ok "还原完成, 健康检查通过: $(health_url)"
  op_hint
else
  warn '还原后健康检查未通过(当前状态: '"$(service_state)"')'
  printf '  排查: journalctl -u %s -n 100 --no-pager\n' "$SERVICE_NAME"
  exit 1
fi
