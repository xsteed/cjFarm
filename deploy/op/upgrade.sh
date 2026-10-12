#!/usr/bin/env bash
# 用新的发布包升级本机后端(数据与既有配置保留, 只同步 database 段 —— 见 deploy.sh 头部)。
#
# 用法:
#   sudo bash op/upgrade.sh <新包.tar.gz | 已解压的新包目录> [deploy.sh 参数...]
#
# 例:
#   sudo bash op/upgrade.sh /tmp/dining-3716aa5-20261013-030803.tar.gz
#   sudo bash op/upgrade.sh /tmp/dining-3716aa5-20261013-030803 --allow-db-switch
#   sudo bash op/upgrade.sh /tmp/dining-3716aa5-20261013-030803 --refresh-config
#
# 升级动作(自动备份 -> 覆盖产物 -> 同步 database 段 -> 重启 -> 健康检查, 失败自动回滚)
# 全部由新包里的 deploy/deploy.sh 执行; 本脚本只负责「解包 + 定位包根 + 转发参数」。
# 建议: 升级前先 sudo bash op/backup.sh 做一次业务备份(升级备份只保护程序, 不保护数据)。
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

usage() {
  cat <<'EOF'
用法: sudo bash op/upgrade.sh <新包.tar.gz | 已解压的新包目录> [deploy.sh 参数...]

deploy.sh 常用参数:
  --allow-db-switch   允许 sqlite -> mysql 切库(数据不会自动搬迁, 见 docs/mysql-migration.md)
  --refresh-config    让 <INSTALL_DIR>/config.yaml 整份按新包里的 config.prod.yaml 重建(先备份)
  --skip-build        使用包内现成产物(默认已加)
EOF
}

require_root
if [ $# -eq 0 ]; then
  usage
  op_hint
  exit 2
fi

src="$1"; shift
work=""
trap '[ -n "${work:-}" ] && rm -rf "$work"' EXIT

# 若还没有业务备份, 顺手做一份(升级备份只回滚程序, 不保数据)。
if ! ls -1dt "$DATA_BACKUP_DIR"/*/ 2>/dev/null | head -1 | grep -q .; then
  log "尚未做过业务备份, 先自动备份一次 ..."
  bash "$DIR/backup.sh" --keep 3 || warn "自动业务备份不完整, 是否继续由你判断"
fi

pkg=""
case "$src" in
  *.tar.gz|*.tgz)
    have tar || die '缺少 tar 命令'
    [ -f "$src" ] || die "压缩包不存在: $src"
    work="$(mktemp -d)"
    log "解压 $src ..."
    tar -xzf "$src" -C "$work" || die "解压失败: $src"
    pkg="$(__locate_pkg_root "$work")"
    ;;
  *)
    [ -d "$src" ] || die "参数既不是 .tar.gz 也不是目录: $src"
    pkg="$(__locate_pkg_root "$(cd "$src" && pwd)")"
    ;;
esac
[ -n "$pkg" ] || die "在 $src 里找不到 deploy/deploy.sh(包结构不完整?)"
log "使用新包: $pkg"

# INSTALL_DIR/SERVICE_NAME 传给子进程: 让新包的 deploy.sh 落到当前安装目录。
export INSTALL_DIR SERVICE_NAME

log "执行 $pkg/deploy/deploy.sh --skip-build ..."
if bash "$pkg/deploy/deploy.sh" --skip-build "$@"; then
  ok "升级完成(安装目录 $INSTALL_DIR)"
  printf '  版本信息: cat %s/VERSION\n' "$INSTALL_DIR"
  printf '  回滚入口: sudo bash %s/op/rollback.sh\n' "$INSTALL_DIR"
else
  rc=$?
  die "升级失败(退出码 $rc)。deploy.sh 已尝试自动回滚; 也可手工回滚: sudo bash $DIR/rollback.sh"
fi
