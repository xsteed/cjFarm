#!/usr/bin/env bash
# 回滚后端**程序**到升级前的版本(deploy.sh 每次升级前自动留在 <INSTALL_DIR>/backup/<时间戳>/)。
#
# 用法:
#   sudo bash op/rollback.sh                # 回滚到最近一次升级前
#   sudo bash op/rollback.sh --list         # 列出可回滚的版本
#   sudo bash op/rollback.sh <时间戳|目录>   # 回滚到指定一份(如 20261013-030803)
#   sudo bash op/rollback.sh --no-restart   # 只还原文件, 不重启(自己改完再 restart.sh)
#
# 与 restore.sh 的分工: 本脚本只动「程序」(二进制/前端/op 脚本/VERSION/config.yaml),
# 不碰数据库; 灾备恢复(连数据一起)用 restore.sh。
#
# 回滚的是 deploy.sh 的升级备份, 里面是: dining-backend / dist/ / config.yaml /
# VERSION / op/(新版才有) / print-agent-bin/(旧版才有)。
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

usage() {
  cat <<'EOF'
用法: sudo bash op/rollback.sh [--list] [--no-restart] [时间戳|目录]

  --list        列出 backup/ 下可回滚的版本
  --no-restart  只还原文件, 不重启服务
EOF
}

require_root

DO_LIST=0
DO_RESTART=1
target=""
while [ $# -gt 0 ]; do
  case "$1" in
    --list) DO_LIST=1; shift ;;
    --no-restart) DO_RESTART=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) target="$1"; shift ;;
  esac
done

[ -d "$APP_BACKUP_DIR" ] || die "没有升级备份目录: $APP_BACKUP_DIR(升级失败自动回滚已发生过? 手工检查 backup/)"

if [ "$DO_LIST" = "1" ]; then
  printf '可回滚的版本(新 -> 旧):\n'
  ls -1dt "$APP_BACKUP_DIR"/*/ 2>/dev/null || true
  printf '\n用法: sudo bash %s <时间戳或目录>\n' "$0"
  exit 0
fi

if [ -z "$target" ]; then
  bak="$(ls -1dt "$APP_BACKUP_DIR"/*/ 2>/dev/null | head -1 || true)"
  bak="${bak%/}"
  [ -n "$bak" ] || die "backup/ 下没有可回滚的备份"
elif [ -d "$target" ]; then
  bak="$(cd "$target" && pwd)"
else
  bak="$APP_BACKUP_DIR/$target"
  [ -d "$bak" ] || die "备份目录不存在: $bak (用 --list 查看)"
fi
[ -f "$bak/dining-backend" ] || die "$bak 里没有 dining-backend, 不是一份完整的升级备份"

echo "将回滚到: $bak"
[ -f "$bak/VERSION" ] && printf '  版本: %s\n' "$(tr '\n' ' ' < "$bak/VERSION")"
if [ "$DO_RESTART" = "1" ]; then
  confirm '确认回滚?(只动程序与配置, 不动数据库)' || { log '已取消'; exit 0; }
fi

log '停止服务 ...'
service_installed && systemctl stop "$SERVICE_NAME" || warn '停止失败/未安装(继续还原)'

# ---- 程序本体 ----
mkdir -p "$BIN_DIR" "$INSTALL_DIR/frontend"   # install 不自建父目录, 先兜住
install -m 0755 "$bak/dining-backend" "$BIN_DIR/dining-backend.new" \
  && mv -f "$BIN_DIR/dining-backend.new" "$BIN_DIR/dining-backend" \
  && ok '已还原后端二进制'
if [ -d "$bak/dist" ]; then
  rm -rf "$INSTALL_DIR/frontend/dist"
  mkdir -p "$INSTALL_DIR/frontend"
  cp -a "$bak/dist" "$INSTALL_DIR/frontend/dist"
  ok '已还原前端产物'
fi
# 配置必须跟着程序走: 否则出现「二进制回滚了、配置还指着新库」(与 deploy.sh 内置回滚同一口径)。
[ -f "$bak/config.yaml" ] && { install -m 0600 "$bak/config.yaml" "$CONFIG"; ok '已还原 config.yaml'; }
[ -f "$bak/dining-backend.env" ] && {
  install -m 0600 "$bak/dining-backend.env" "/etc/$SERVICE_NAME.env"
  ok '已还原 /etc/dining-backend.env'
}
[ -f "$bak/VERSION" ] && { cp -f "$bak/VERSION" "$INSTALL_DIR/VERSION"; ok '已还原 VERSION'; }

# ---- 运维脚本(新版备份里有 op/)与旧格式(bin/check-health.sh)兼容 ----
if [ -d "$bak/op" ]; then
  rm -rf "$INSTALL_DIR/op"
  cp -a "$bak/op" "$INSTALL_DIR/op"
  chmod +x "$INSTALL_DIR/op"/*.sh 2>/dev/null || true
  ok '已还原 op/ 运维脚本'
elif [ -f "$bak/check-health.sh" ]; then
  mkdir -p "$INSTALL_DIR/op"
  install -m 0755 "$bak/check-health.sh" "$INSTALL_DIR/op/check-health.sh"
  ok '已还原旧格式的 check-health.sh(旧备份) -> op/'
fi
if [ -d "$bak/print-agent-bin" ]; then
  rm -rf "$INSTALL_DIR/print-agent/bin"
  mkdir -p "$INSTALL_DIR/print-agent/bin"
  cp -a "$bak/print-agent-bin/." "$INSTALL_DIR/print-agent/bin/"
  ok '已还原打印代理产物'
fi

chown_install_tree

if [ "$DO_RESTART" != "1" ]; then
  ok '文件已还原(--no-restart); 改完记得 sudo bash op/restart.sh'
  exit 0
fi

log '启动服务 ...'
if restart_service; then
  ok "回滚完成, 健康检查通过: $(health_url)"
  printf '  版本: %s\n' "$(tr '\n' ' ' < "$INSTALL_DIR/VERSION" 2>/dev/null)"
else
  warn '回滚后健康检查未通过(当前状态: '"$(service_state)"')'
  printf '  排查: journalctl -u %s -n 100 --no-pager\n' "$SERVICE_NAME"
  printf '  这份备份自身可能就有问题, 用 --list 换一份再试\n'
  exit 1
fi
