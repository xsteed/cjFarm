#!/usr/bin/env bash
# 卸载后端服务: 停服务 + 移除 systemd 单元 + 移除 Nginx 站点。
# 默认**保留全部数据**(data/、config.yaml、backups/、backup/) —— 卸载 ≠ 删数据。
#
# 用法:
#   sudo bash op/uninstall.sh [--keep-nginx]
#   sudo bash op/uninstall.sh --all       # 彻底删除整个安装目录(会先自动备份数据, 需二次确认)
#
#   --keep-nginx   不动 Nginx 站点配置(比如这台机器还要换别的东西顶上)
#   --all          彻底卸载: 先自动备份一次数据, 确认后删除整个 <INSTALL_DIR>
#
# 与 stop.sh 的区别: stop 只停本次运行(自启动保留); 本脚本移除 systemd 单元, 不再开机拉起。
# 重新安装: 解压新发布包后执行 sudo bash deploy/op/install.sh(data/ 里的库和密钥会被直接沿用)。
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

NGINX_CONF="${NGINX_CONF:-/etc/nginx/conf.d/dining-system.conf}"
KEEP_NGINX=0
ALL=0
while [ $# -gt 0 ]; do
  case "$1" in
    --keep-nginx) KEEP_NGINX=1; shift ;;
    --all) ALL=1; shift ;;
    -h|--help) sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "未知参数: $1" ;;
  esac
done

require_root

# ---- 1. 服务 ----
if service_installed; then
  log "停止并移除 systemd 单元 $SERVICE_NAME ..."
  systemctl disable --now "$SERVICE_NAME" 2>/dev/null || systemctl stop "$SERVICE_NAME" 2>/dev/null || true
  rm -f "/etc/systemd/system/$SERVICE_NAME.service"
  systemctl daemon-reload 2>/dev/null || true
  ok "已移除 $SERVICE_NAME 单元"
else
  log "未安装 systemd 单元 $SERVICE_NAME(跳过)"
fi

# ---- 2. Nginx 站点 ----
if [ "$KEEP_NGINX" = "1" ]; then
  log "按 --keep-nginx 保留 Nginx 配置: $NGINX_CONF"
elif [ -f "$NGINX_CONF" ]; then
  rm -f "$NGINX_CONF"
  if have nginx; then
    if nginx -t >/dev/null 2>&1 && nginx -s reload >/dev/null 2>&1; then
      ok "已移除 Nginx 站点并重载: $NGINX_CONF"
    else
      warn "已删除 $NGINX_CONF, 但 nginx -t / reload 失败, 请手工检查: nginx -t"
    fi
  else
    ok "已移除 Nginx 站点: $NGINX_CONF(本机无 nginx 命令, 跳过重载)"
  fi
else
  log "无 Nginx 站点配置(跳过)"
fi

# ---- 3. 历史遗留的 /etc/dining-backend.env ----
if [ -f "/etc/$SERVICE_NAME.env" ]; then
  log "保留历史环境变量文件 /etc/$SERVICE_NAME.env(若不再需要可手工删除)"
fi

# ---- 4. 数据与安装目录 ----
if [ "$ALL" != "1" ]; then
  ok "卸载完成; 以下内容**原样保留**(重装后直接沿用):"
  printf '  %s\n' "$INSTALL_DIR/data(数据库文件/加密主密钥)"
  printf '  %s\n' "$CONFIG"
  printf '  %s  %s\n' "$DATA_BACKUP_DIR" "$APP_BACKUP_DIR"
  printf '  %s\n' "$INSTALL_DIR/op(本套运维脚本)"
  printf '\n彻底删除请用: sudo bash %s --all\n' "$0"
  exit 0
fi

# ---- --all: 彻底删除(先自动备份) ----
log "--all: 先自动备份一次数据(万一删错还有得救) ..."
if bash "$DIR/backup.sh" >/dev/null 2>&1; then
  saved="$(ls -1dt "$DATA_BACKUP_DIR"/*/ 2>/dev/null | head -1 | tr -d '\n')"
  ok "数据已备份到: $saved"
else
  warn '自动备份失败! 继续删除将**永久丢失**全部业务数据'
fi

echo "即将删除整个安装目录: $INSTALL_DIR"
printf '  包含数据库文件/加密主密钥/日志/全部备份\n'
confirm '彻底删除?(不可撤销)' || { log '已取消(数据未动)'; exit 0; }

rm -rf "$INSTALL_DIR"
ok "已删除 $INSTALL_DIR"
printf '  保留的备份: %s\n' "${saved:-(无)}"
