#!/usr/bin/env bash
# 启动门店打印代理(macOS):launchctl 加载;未安装时给出安装引导。
#
# 幂等:未加载 → load;已加载 → 重启任务(kickstart -k),这样升级替换二进制后
# 不用先 ./stop.sh 也能立刻让新版本生效(KeepAlive 任务被 kill 会自动重启)。
set -u
LABEL="com.cjfarm.print-agent"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UID_NUM="$(id -u)"
if [ ! -f "$PLIST" ]; then
  echo "[未安装] 找不到 $PLIST"
  echo "  请先安装(一条命令,自动配置 + 注册自启动):"
  echo "    ./print-agent --install --server https://你的管理后台域名 --token 你的令牌"
  exit 1
fi

if launchctl list "$LABEL" >/dev/null 2>&1; then
  # 已加载:优先用 kickstart 重启进程(加载的是 plist 当前内容 / 新二进制)
  if launchctl kickstart -k "gui/$UID_NUM/$LABEL" >/dev/null 2>&1; then
    echo "[已重启] $LABEL 已在运行,已重新加载(新版本即刻生效)"
    exit 0
  fi
  pkill -x print-agent >/dev/null 2>&1
  echo "[已加载] $LABEL 已在 launchd 中;请执行 ./stop.sh 后再 ./start.sh"
  exit 0
fi

if launchctl load "$PLIST" 2>/dev/null; then
  echo "[已启动] launchd 已加载 $LABEL"
else
  echo "[启动失败] launchctl load 报错,常见原因是 plist 语法错误或路径失效:"
  echo "  plutil -lint \"$PLIST\"    # 校验 plist"
  echo "  tail -n 20 \$($PLIST 的 StandardOutPath)"
  exit 1
fi
