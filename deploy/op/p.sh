#!/usr/bin/env bash
# 后端运行状态一览: 服务 / 版本 / 端口 / 健康 / 数据库 / 磁盘 / 日志尾。
# 门店报「点不了菜」「打不出小票」时, 先跑这个。
# 用法: bash op/p.sh          # 只读, 不需要 root
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

echo '== 服务 =='
printf '  %s: %s\n' "$SERVICE_NAME" "$(service_state)"
if [ -f "$INSTALL_DIR/VERSION" ]; then
  printf '  版本: %s\n' "$(tr '\n' ' ' < "$INSTALL_DIR/VERSION" | sed 's/  */ /g')"
fi
if have systemctl && service_installed; then
  started="$(systemctl show -p ActiveEnterTimestamp --value "$SERVICE_NAME" 2>/dev/null || true)"
  [ -n "$started" ] && printf '  启动于: %s\n' "$started"
  mem="$(systemctl show -p MemoryCurrent --value "$SERVICE_NAME" 2>/dev/null || true)"
  case "$mem" in
    ''|*[!0-9]*) ;;
    *) printf '  内存占用: %s\n' "$(human_size "$mem")" ;;
  esac
fi

echo ''
echo '== 端口与健康 =='
PORT_CFG="$(cfg_get server port)"
PORT_CFG="${PORT_CFG:-8080}"
printf '  监听端口: %s' "$PORT_CFG"
LISTEN_OUT=""
have ss && LISTEN_OUT="$(ss -ltn 2>/dev/null || true)"
if [ -z "$LISTEN_OUT" ] && have netstat; then
  LISTEN_OUT="$(netstat -an 2>/dev/null || true)"
fi
if [ -n "$LISTEN_OUT" ]; then
  # 用 here-string 而不是管道: `cmd | grep -q` 命中即退出会让上游收到 SIGPIPE,
  # 在 pipefail 下整条管道被判失败 —— 明明在监听却报「未监听到」。
  if grep -qE "[.:]${PORT_CFG}[[:space:]]" <<< "$LISTEN_OUT"; then
    printf ' (LISTEN)\n'
  else
    printf ' (未监听到)\n'
  fi
else
  printf ' (无法探测: 无 ss/netstat)\n'
fi
if have curl; then
  res="$(curl -s -o /dev/null -w '%{http_code} %{time_total}' --max-time 5 "$(health_url)" 2>/dev/null || true)"
  code="${res%% *}"; lat="${res##* }"
  ms="$(awk -v t="$lat" 'BEGIN{printf "%.0f", t*1000}' 2>/dev/null || echo 0)"
  [ -n "$ms" ] || ms=0
  if [ "$code" = "200" ]; then
    ok "健康接口 200 (${ms}ms): $(health_url)"
    printf '      该接口会查库, 200 说明「进程活着 + 数据库可读」\n'
  else
    warn "健康接口 HTTP ${code:-000}: $(health_url)"
  fi
else
  warn '未安装 curl, 跳过健康探测'
fi

echo ''
echo '== 数据库 =='
DRV="$(db_driver)"
if [ "$DRV" = "mysql" ]; then
  db_load_conn
  printf '  driver: mysql  %s:%s/%s (user %s)\n' "$DB_HOST" "$DB_PORT" "$DB_NAME" "$DB_USER"
  if have mysql; then
    if mysql_client; then
      ok '连通正常(用 config.yaml 里的连接信息实测)'
      pkt="$(mysql_q 'SELECT @@global.max_allowed_packet' || true)"
      case "$pkt" in
        ''|*[!0-9]*) warn '未能读到 max_allowed_packet(权限不足?)' ;;
        *)
          if [ "$pkt" -lt 134217728 ]; then
            warn "max_allowed_packet=$(human_size "$pkt") < 128M: 上传接近 64MB 的图片会报 ER_NET_PACKET_TOO_LARGE"
            printf '      服务端执行: SET GLOBAL max_allowed_packet=134217728;(写入 my.cnf 持久化)\n'
          else
            ok "max_allowed_packet=$(human_size "$pkt")"
          fi ;;
      esac
    else
      warn '连通失败(账号/密码/网络/库名); 若后端本身正常, 以日志为准: journalctl -u '"$SERVICE_NAME"' -n 50'
    fi
  else
    warn '未安装 mysql 客户端, 跳过连通性探测(健康接口 200 已隐含数据库可读)'
  fi
else
  DBFILE="$INSTALL_DIR/data/dining.db"
  printf '  driver: sqlite  %s (%s)\n' "$DBFILE" "$(file_size "$DBFILE")"
  [ -f "$DBFILE" ] || warn '  SQLite 文件不存在(首次启动后才会创建)'
fi

echo ''
echo '== 磁盘与备份 =='
df -h "$INSTALL_DIR" 2>/dev/null | awk 'NR == 2 { printf "  磁盘可用: %s / %s(%s 已用)\n", $4, $2, $5 }'
for f in "$INSTALL_DIR/logs/app.log" "$INSTALL_DIR/logs/agent.log"; do
  [ -f "$f" ] && printf '  日志 %s: %s\n' "$(basename "$f")" "$(file_size "$f")"
done
latest_app="$(ls -1dt "$APP_BACKUP_DIR"/*/ 2>/dev/null | head -1 || true)"
[ -n "$latest_app" ] && printf '  最近程序备份(升级前): %s\n' "${latest_app%/}"
latest_data="$(ls -1dt "$DATA_BACKUP_DIR"/*/ 2>/dev/null | head -1 || true)"
[ -n "$latest_data" ] && printf '  最近业务备份(backup.sh): %s\n' "${latest_data%/}"

echo ''
echo '== 最近日志(末 15 行) =='
LOGFILE="$(cfg_get log path)"
LOGFILE="${LOGFILE:-./logs/app.log}"
case "$LOGFILE" in
  /*) ;;
  *) LOGFILE="$INSTALL_DIR/${LOGFILE#./}" ;;
esac
if [ -f "$LOGFILE" ]; then
  tail -n 15 "$LOGFILE"
else
  echo "  <无日志文件: $LOGFILE>"
fi

op_hint
