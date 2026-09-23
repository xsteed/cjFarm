#!/usr/bin/env bash
# 双击本文件即可安装(macOS)。结束前会停住等你按回车,方便把结果看完。
#
# 双击被系统拦下时(「无法打开,因为来自身份不明的开发者」):
#   在文件上右键 →「打开」,或在本目录执行:
#     xattr -d com.apple.quarantine "双击安装.command"
#
# 这个文件只是 install.sh 的「双击壳」:真正干活的是 install.sh,命令行下直接
# 执行 ./install.sh 效果完全一样(脚本里的输出也更完整)。
cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1
./install.sh
RC=$?
echo
echo "(退出码 $RC)按回车键关闭此窗口…"
read -r _ || true
exit "$RC"
