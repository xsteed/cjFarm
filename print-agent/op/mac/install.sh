#!/usr/bin/env bash
# 安装门店打印代理(macOS):写配置 + 注册开机自启。
# 用法: ./install.sh [管理后台地址] [代理令牌]   (省略参数则交互式询问)
set -u
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 优先按本机芯片选产物(Apple Silicon → arm64,Intel → amd64),避免字母序误选;
# 找不到对应芯片版本时再回退到任意 darwin 产物(Intel 版可经 Rosetta 运行)。
PREF=""
case "$(uname -m)" in
  arm64)  PREF="$DIR/print-agent-darwin-arm64" ;;
  x86_64) PREF="$DIR/print-agent-darwin-amd64" ;;
esac

BIN=""
if [ -n "$PREF" ] && [ -f "$PREF" ] && [ -x "$PREF" ]; then
  BIN="$PREF"
else
  for f in "$DIR"/print-agent "$DIR"/print-agent-darwin-*; do
    case "$f" in
      *.state|*.log|*.sh|*.md|*.example|*.plist|*.service|*.xml) continue ;;
    esac
    if [ -f "$f" ] && [ -x "$f" ]; then BIN="$f"; break; fi
  done
fi
if [ -z "$BIN" ]; then
  echo "[错误] 未找到代理产物(print-agent 或 print-agent-darwin-*),请先把它放到本脚本同目录"
  exit 1
fi

if [ $# -ge 2 ]; then
  "$BIN" --install --server "$1" --token "$2"
else
  "$BIN" --install   # 交互式询问地址与令牌
fi
RC=$?
if [ "$RC" -ne 0 ]; then
  exit "$RC"
fi

# macOS 15(Sequoia)起新增「本地网络」隐私权限:连接局域网需要授权,而授权按 App 识别。
# 由 launchd 自启动的代理是被拦的那一类(终端及其子进程不受限制),所以 --install 会把
# 程序装进 ~/Applications/PrintAgent.app 以便系统能列出并授权它。这一步门店必须知道,
# 否则第一次打不出小票时只会看到「连不上打印机」,根本想不到是权限问题。
APP="$HOME/Applications/PrintAgent.app"
DATA_DIR="${PRINT_AGENT_DATA_DIR:-$HOME/Library/Application Support/PrintAgent}"
echo ""
echo "== 下一步 =="
echo "代理已部署为 App:  $APP"
echo "配置文件/日志在:   $DATA_DIR"
echo ""
echo "① 体检(建议现在跑一次):    ./print-agent --doctor"
echo "   一次查完配置/自启动/打印通道/防睡眠与合盖/云端连通,并直接告诉您该敲什么命令。"
echo ""
echo "② 打不出小票时(报「无法连接打印机 ... no route to host」):"
echo "   在本目录手工执行 ./print-agent-darwin-arm64 --probe <打印机IP>,若能显示「可达」,"
echo "   说明是系统的「本地网络」隐私权限拦下了开机自启的代理(终端不受该限制)。"
echo "   一条命令解决(不需要管理员密码,可重复执行):"
echo "       ./print-agent --setup-cups <打印机IP>"
echo "   也可到「系统设置 → 隐私与安全性 → 本地网络」里允许「长健农场打印代理」:"
echo "       open \"x-apple.systempreferences:com.apple.preference.security?Privacy_LocalNetwork\""
echo ""
echo "③ 查看状态与最近日志:      ./p.sh"

# ---- 合盖后继续运行 ----
# 这是两件独立的事,少做一件都不成立:
#   ① caffeinate(--install 时选「是」/ PRINT_AGENT_NO_SLEEP=1)顶住「空闲睡眠」;
#   ② 关掉系统级的「合盖睡眠」开关。合盖触发的是 Clamshell Sleep,属于电源管理层的
#      独立触发器,caffeinate 用的 IOPMAssertion 抑制不了它 —— 实测接了电源也照样睡:
#        Entering Sleep state due to 'Clamshell Sleep' ... Using AC (Charge:100%)
#      ② 需要 root,所以只能在这里替门店做掉。
echo ""
echo "== 合盖后继续运行 =="
APP_BIN="$APP/Contents/MacOS/print-agent"
if ioreg -r -c IOPMrootDomain -d 1 2>/dev/null | grep -q '"SleepDisabled" = Yes'; then
  echo "合盖睡眠已关闭(SleepDisabled=Yes):合盖后 Mac 不会睡,代理继续取单打单。"
else
  echo "当前合盖会让 Mac 睡眠 —— 睡眠期间进程全停、网络断开,代理停摆(唤醒后自动续上,不丢单)。"
  echo "要让合盖后继续打单,除了安装时选「防睡眠」,还要关掉系统的合盖睡眠开关:"
  echo "    sudo pmset -a disablesleep 1"
  if [ -t 0 ]; then
    printf '现在就设置吗?(需要输入管理员密码)[y/N] '
    read -r _ans || _ans=""
    case "$_ans" in
      y|Y|yes|YES)
        if sudo pmset -a disablesleep 1; then
          echo "已设置。核对:"
          echo "    ioreg -r -c IOPMrootDomain -d 1 | grep SleepDisabled    # 期望 \"SleepDisabled\" = Yes"
          echo "已启用防睡眠的话,现在合盖也能继续打单了(需保持接电源)。"
        else
          echo "[警告] 设置失败,请手工执行上面那条命令。"
        fi
        ;;
      *)
        echo "已跳过。之后随时可手动执行上面那条命令。"
        ;;
    esac
  else
    echo "(非交互环境,跳过询问;请手工执行上面那条命令。)"
  fi
  echo "注意:合盖长时间运行会持续耗电并发热,请务必接交流电源、留出散热空间。"
  echo "      要恢复「合盖即睡」: sudo pmset -a disablesleep 0"
  if [ -x "$APP_BIN" ]; then
    echo "      核对代理侧的防睡眠是否也就位: \"$APP_BIN\" --install 时会自动报告;"
    echo "      或直接看 ./p.sh 的「合盖/睡眠」一节。"
  fi
fi
