#!/usr/bin/env bash
# 备份后端数据: 数据库 + 加密主密钥 + 登录密钥 + 配置 + VERSION。
# 落到 <INSTALL_DIR>/backups/<时间戳>/, 默认保留最近 14 份。
#
# 用法:
#   sudo bash op/backup.sh [--out DIR] [--keep N]
#
# 为什么必须「成套」备份(任何一样缺失, 这份备份就不可用):
#   · db.sql / dining.db   —— 业务数据本体
#   · data/master.key      —— 敏感配置(微信支付密钥/小程序配置)的解密主密钥,
#                             缺了它, 备份出来的库就是一堆解不开的密文
#   · data/auth.key        —— 登录令牌签名密钥(丢了所有登录态失效, 全员重登)
#   · config.yaml          —— 决定连哪个库、怎么连、监听哪个端口
# 与升级备份(<INSTALL_DIR>/backup/)的分工: 那个只保护「程序」, 这个保护「数据」。
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

usage() {
  cat <<'EOF'
用法: sudo bash op/backup.sh [--out DIR] [--keep N]

  --out DIR   备份输出目录(默认 <INSTALL_DIR>/backups)
  --keep N    保留最近 N 份(默认 14, 超出的自动删除)
EOF
}

require_root

OUT="$DATA_BACKUP_DIR"
KEEP=14
while [ $# -gt 0 ]; do
  case "$1" in
    --out)  [ $# -ge 2 ] || die "缺少参数值: $1"; OUT="$2"; shift 2 ;;
    --keep) [ $# -ge 2 ] || die "缺少参数值: $1"; KEEP="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数: $1 (用 -h 查看用法)" ;;
  esac
done
case "$KEEP" in
  ''|*[!0-9]*) die "--keep 必须是数字: $KEEP" ;;
esac

stamp="$(date +%Y%m%d-%H%M%S)"
dst="$OUT/$stamp"
# 先清同名残留再建目录 —— 顺序不能反: 反过来就成了「建好目录又把它删掉」,
# 后面所有 cp 都会 No such file or directory(本地演练实测踩过)。
rm -rf "$dst"
mkdir -p "$dst" || die "无法创建备份目录: $dst"

DRV="$(db_driver)"
data_ok=1

# ---- 1. 数据库 ----
if [ "$DRV" = "mysql" ]; then
  if ! have mysqldump; then
    warn '未安装 mysqldump(mysql-client 包), 无法备份数据库'
    data_ok=0
  elif ! mysql_client; then
    warn '用 config.yaml 里的连接信息连不上 MySQL(账号/网络/库?), 无法备份数据库'
    data_ok=0
  else
    db_load_conn
    log "导出 MySQL 数据库 $DB_NAME@$DB_HOST:$DB_PORT ..."
    # --single-transaction: InnoDB 一致性快照, 不锁表(门店营业中也能备)
    if MYSQL_PWD="$DB_PASS" mysqldump --protocol=TCP -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" \
        --single-transaction --routines --triggers \
        --default-character-set=utf8mb4 "$DB_NAME" > "$dst/db.sql" 2> "$dst/db.err"; then
      rm -f "$dst/db.err"
      ok "数据库已导出: db.sql ($(file_size "$dst/db.sql"))"
    else
      msg="$(tail -n 2 "$dst/db.err" 2>/dev/null | tr '\n' ' ')"
      rm -f "$dst/db.sql"
      warn "mysqldump 失败: $msg"
      data_ok=0
    fi
  fi
else
  dbfile="$INSTALL_DIR/data/dining.db"
  if [ -f "$dbfile" ]; then
    # sqlite 用整文件复制; 有 WAL 时短暂停止写入更稳, 但门店营业中不宜停服 ——
    # 复制 dining.db + dining.db-wal + dining.db-shm 三个文件即可完整还原。
    cp -a "$dbfile" "$dst/dining.db"
    for ext in -wal -shm; do
      [ -f "$dbfile$ext" ] && cp -a "$dbfile$ext" "$dst/dining.db$ext"
    done
    ok "SQLite 已复制: dining.db ($(file_size "$dst/dining.db"))"
  else
    warn "找不到 SQLite 文件: $dbfile"
    data_ok=0
  fi
fi

# ---- 2. 密钥与配置(成套备份的关键) ----
for f in data/master.key data/auth.key config.yaml VERSION; do
  s="$INSTALL_DIR/$f"
  if [ -f "$s" ]; then
    cp -a "$s" "$dst/$(basename "$f")"
    ok "已备份 $f"
  else
    warn "缺少 $f"
    [ "$f" = "data/master.key" ] && data_ok=0
  fi
done

# ---- 3. 清单 ----
{
  printf 'time=%s\n' "$(date '+%Y-%m-%d %H:%M:%S')"
  printf 'host=%s\n' "$(hostname 2>/dev/null || echo unknown)"
  printf 'install_dir=%s\n' "$INSTALL_DIR"
  printf 'db_driver=%s\n' "$DRV"
  if [ "$DRV" = "mysql" ]; then
    db_load_conn
    printf 'mysql=%s@%s:%s/%s\n' "$DB_USER" "$DB_HOST" "$DB_PORT" "$DB_NAME"
  fi
  [ -f "$INSTALL_DIR/VERSION" ] && sed 's/^/build_/' "$INSTALL_DIR/VERSION"
} > "$dst/MANIFEST" 2>/dev/null

# ---- 4. 保留最近 N 份 ----
count=0
while IFS= read -r d; do
  [ -n "$d" ] || continue
  count=$((count + 1))
  [ "$count" -gt "$KEEP" ] && rm -rf "$d"
done < <(ls -1dt "$OUT"/*/ 2>/dev/null || true)
left="$(ls -1d "$OUT"/*/ 2>/dev/null | wc -l | tr -d ' ')"
printf '  当前共保留 %s 份(目录: %s)\n' "${left:-0}" "$OUT"

ok "备份完成: $dst"
printf '  还原: sudo bash %s/restore.sh %s\n' "$DIR" "$dst"

if [ "$data_ok" != "1" ]; then
  warn '本次备份**不含完整数据库/密钥**, 不要当作灾备使用; 修好上面的告警后重新备份'
  exit 1
fi
