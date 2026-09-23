#!/usr/bin/env bash
# 打印门店打印代理运行状态(macOS):
#   进程状态 + 最近 15 行日志 + 云端连通自检提示。
#
# 为什么不能只用「launchctl list 里有没有这个 Label」判断:那只代表任务在 launchd
# 里登记过。任务已被卸载/停止但尚未注销、或进程被 kill 时也可能命中,会把「已停止」
# 误报成「运行中」。正确做法是看 launchctl list 的第一列 PID(- 表示无进程)。
set -u

LABEL="com.cjfarm.print-agent"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

# launchd 输出格式跨版本不一致,取值要两手准备:
#   · launchctl list(不带 Label)→「PID 退出码 Label」三列,通用且最快
#   · launchctl list <Label> / launchctl print → 新款 macOS 返回 plist 风格键值块
#     (带 Label 参数还能对不存在的任务返回空输出 + 非零状态码,故只用它判断「是否已登记」)
JOB_LISTED=0
launchctl list "$LABEL" >/dev/null 2>&1 && JOB_LISTED=1
LINE="$(launchctl list 2>/dev/null | awk -v l="$LABEL" '$3==l {print; exit}')"
PID="$(printf '%s\n' "$LINE" | awk 'NF>=3 {print $1}')"
EXIT="$(printf '%s\n' "$LINE" | awk 'NF>=3 {print $2}')"
if [ -z "$PID" ]; then
  INFO="$(launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null)"
  PID="$(printf '%s\n' "$INFO" | sed -n 's/.*"PID" = \([0-9]*\).*/\1/p' | head -n 1)"
  [ -n "$PID" ] && [ -z "${EXIT:-}" ] && EXIT="$(printf '%s\n' "$INFO" | sed -n 's/.*"LastExitStatus" = \([0-9]*\).*/\1/p' | head -n 1)"
fi

echo "== 进程状态 =="
if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
  UP="$(ps -o etime= -p "$PID" 2>/dev/null | tr -d ' ')"
  echo "运行中 PID=$PID(启动至今 ${UP:-未知})"
elif [ "$JOB_LISTED" -eq 1 ]; then
  echo "已停止(launchd 仍登记该任务,但当前无进程)"
  echo "  上次退出状态: ${EXIT:-未知}"
  echo "  恢复: ./start.sh"
elif [ -f "$PLIST" ]; then
  echo "已停止(任务未加载;自启动保留,下次登录或重启后自动拉起)"
  echo "  立即启动: ./start.sh"
else
  echo "未安装(找不到 $PLIST)"
  echo "  安装: ./install.sh [管理后台地址] [代理令牌]"
fi

# 日志文件按可信度依次探测:
#   ① PRINT_AGENT_LOG(显式指定)
#   ② 数据目录 print-agent.log —— --install 装成 .app 后,程序运行在
#      ~/Applications/PrintAgent.app 里,数据(配置/状态/日志)固定在
#      ~/Library/Application Support/PrintAgent(不写进 App,否则会破坏代码签名)
#   ③ 程序同目录 print-agent.log(未装成 App 的直接解压运行形态)
#   ④ plist 的 StandardOutPath —— launchd 捕获的标准输出,代理自身日志落盘失败时(例如
#      旧版二进制把相对路径写到只读目录)这里是唯一的排障线索。
DATA_DIR="${PRINT_AGENT_DATA_DIR:-$HOME/Library/Application Support/PrintAgent}"
echo ""
echo "== 最近日志(末 15 行) =="
LOG=""
if [ -n "${PRINT_AGENT_LOG:-}" ] && [ -f "$PRINT_AGENT_LOG" ]; then
  LOG="$PRINT_AGENT_LOG"
elif [ -f "$DATA_DIR/print-agent.log" ]; then
  LOG="$DATA_DIR/print-agent.log"
elif [ -f "$DIR/print-agent.log" ]; then
  LOG="$DIR/print-agent.log"
elif [ -f "$PLIST" ]; then
  OUT="$(sed -n '/<key>StandardOutPath<\/key>/{n;s|.*<string>\(.*\)</string>.*|\1|p;}' "$PLIST" 2>/dev/null)"
  [ -n "$OUT" ] && [ -f "$OUT" ] && LOG="$OUT"
fi
if [ -n "$LOG" ]; then
  echo "(文件: $LOG)"
  tail -n 15 "$LOG"
else
  echo "<尚无日志>"
  echo "  ① 代理日志:运行后写入程序同目录 print-agent.log(需较新版本二进制)"
  echo "  ② launchd 控制台输出:见 $PLIST 的 StandardOutPath"
fi

# ---- 合盖/睡眠:门店问「合盖能不能继续打单」时,这一节就是答案 ----
# 两件事缺一不可:caffeinate 顶住空闲睡眠 + 关掉系统级的合盖睡眠开关。后者常被漏掉,
# 因为 caffeinate 看起来「已经开了防睡眠」。所以这里把两者分开列出来。
echo ""
echo "== 合盖/睡眠(合盖后能否继续打单) =="
PMSET_SLEEP="$(ioreg -r -c IOPMrootDomain -d 1 2>/dev/null | grep '"SleepDisabled"')"
case "$PMSET_SLEEP" in
  *"= Yes") echo "合盖睡眠开关: 已关闭(合盖不睡)" ;;
  *"= No")  echo "合盖睡眠开关: 未关闭 —— 合盖后系统会睡眠,代理停摆"
            echo "              要让合盖后继续打单: sudo pmset -a disablesleep 1" ;;
  *)        echo "合盖睡眠开关: 读取失败(可手工执行 ioreg -r -c IOPMrootDomain -d 1 | grep SleepDisabled)" ;;
esac
echo "供电:         $(pmset -g batt 2>/dev/null | head -1)"
LID="$(ioreg -r -k AppleClamshellState -d 1 2>/dev/null | sed -n 's/.*"AppleClamshellState" = //p')"
[ -n "$LID" ] && echo "屏幕盖状态:   $LID(Yes=已合盖)"

echo "caffeinate:"
CAFF="$(pgrep -x caffeinate 2>/dev/null)"
if [ -n "$CAFF" ]; then
  ps -o pid,command -p "$CAFF" 2>/dev/null | sed 1d | sed 's/^/  /'
  echo "  → 已启用防睡眠,空闲睡眠被顶住"
else
  echo "  (未发现 caffeinate 进程)"
  echo "  → 安装时没启用防睡眠:合盖后不仅会睡,空闲计时器也会让系统睡。"
  echo "     重跑 ./install.sh 并在询问时选 y(或 PRINT_AGENT_NO_SLEEP=1 后重跑)"
fi

CLAM="$(pmset -g log 2>/dev/null | grep -i 'Clamshell Sleep' | tail -3)"
echo "最近的合盖睡眠记录(有输出=确实因合盖睡过):"
if [ -n "$CLAM" ]; then
  echo "$CLAM" | sed 's/^/  /'
else
  echo "  (无)"
fi

echo ""
echo "== macOS 本地网络权限 =="
APP="$HOME/Applications/PrintAgent.app"
if [ -d "$APP" ]; then
  echo "App 已安装: $APP"
  echo "  数据目录: $DATA_DIR"
  echo "  若日志反复出现「无法连接打印机 ... no route to host」,而本目录里手工执行"
  echo "  --probe 却能通,说明是系统的「本地网络」隐私权限拦下了自启动的代理(终端不受限制)。"
  echo "  处置(二选一):"
  echo "    ① 改用系统打印服务(推荐,不受该权限影响):"
  echo "       在「系统设置 → 打印机与扫描仪」以 IP 方式添加本店打印机,"
  echo "       再在 $DATA_DIR/agent.env 里加 PRINT_AGENT_PRINT_VIA=auto 后重启;"
  echo "    ② 授予权限:「系统设置 → 隐私与安全性 → 本地网络」里允许本 App。"
  echo "  打开权限面板: open \"x-apple.systempreferences:com.apple.preference.security?Privacy_LocalNetwork\""
else
  echo "未发现 $APP(尚未按 .app 形态安装)"
  echo "  重装可执行: ./install.sh   # 会自动部署 App,便于系统识别与授权"
fi

echo ""
echo "== 云端连通自检 =="
BIN=""
if [ -x "$APP/Contents/MacOS/print-agent" ]; then
  BIN="$APP/Contents/MacOS/print-agent"
fi
if [ -z "$BIN" ]; then
  case "$(uname -m)" in
    arm64)  [ -x "$DIR/print-agent-darwin-arm64" ] && BIN="$DIR/print-agent-darwin-arm64" ;;
    x86_64) [ -x "$DIR/print-agent-darwin-amd64" ] && BIN="$DIR/print-agent-darwin-amd64" ;;
  esac
fi
[ -z "$BIN" ] && [ -x "$DIR/print-agent" ] && BIN="$DIR/print-agent"
if [ -n "$BIN" ]; then
  echo "  手动执行: \"$BIN\" --once"
  echo "  打印机连通性自检: \"$BIN\" --probe <打印机IP>"
else
  echo "  <未找到代理产物(App 内或本目录的 print-agent)>"
fi
