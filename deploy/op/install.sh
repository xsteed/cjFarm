#!/usr/bin/env bash
# 首次安装后端服务 —— 入口别名, 真正的安装/更新逻辑在包内的 deploy/deploy.sh。
#
# 用法(在**发布包根目录**执行; 安装完成后包里的 op/install.sh 就用不上了):
#   sudo bash deploy/op/install.sh                     # 首次安装(sqlite 或模板里的数据库)
#   sudo bash deploy/op/install.sh --allow-db-switch   # 允许 sqlite -> mysql 切库(先搬数据)
#   sudo bash deploy/op/install.sh --refresh-config    # 让 <INSTALL_DIR>/config.yaml 整份按模板重建
#
# 安装内容与目录结构见包内 deploy/op/README.md。
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$DIR/_common.sh"

require_root

PKG_ROOT="$(cd "$DIR/../.." && pwd)"
if [ ! -f "$PKG_ROOT/deploy/deploy.sh" ]; then
  die "找不到 $PKG_ROOT/deploy/deploy.sh —— 本脚本要在发布包根目录下执行
     (解压 dining-*.tar.gz 得到的 dining-<版本>-<时间戳> 目录)"
fi

if [ -x "$BIN_DIR/dining-backend" ]; then
  warn "检测到已安装: $BIN_DIR/dining-backend —— 这属于升级场景"
  printf '  升级入口(在已安装的机器上): sudo bash %s/op/upgrade.sh <新包.tar.gz>\n' "$INSTALL_DIR"
  printf '  仍要在本包上重跑安装流程, 直接执行: sudo bash deploy/deploy.sh --skip-build\n'
  confirm '仍要继续吗?' || { log '已取消'; exit 0; }
fi

log "调用 $PKG_ROOT/deploy/deploy.sh --skip-build ..."
exec bash "$PKG_ROOT/deploy/deploy.sh" --skip-build "$@"
