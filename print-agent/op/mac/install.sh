#!/usr/bin/env bash
# 门店打印代理(macOS)一键安装:装好、配好、验完,一条命令。
#
# 用法:
#   ./install.sh                                    交互式问齐(后台地址、令牌、打印机IP)
#   ./install.sh https://后台域名 令牌              打印机 IP 省略 → 用代理见过的地址自动配
#   ./install.sh https://后台域名 令牌 192.168.1.133 一次到位(多台用逗号分隔)
#
# 选项:
#   --no-cups   不配置系统打印通道(保持直连打印机)
#   --no-lid    不设置「合盖继续运行」
#   --minimal   只安装(等价于上面两个都关,且跳过体检)
#
# 它会依次做四件事,任何一步失败都会明确告诉你卡在哪、怎么补:
#   ① 安装    写配置 + 部署 ~/Applications/PrintAgent.app + 注册开机自启
#   ② 打印通道 把打印机交给系统打印服务(macOS 15+ 的「本地网络」权限会拦住直连)
#   ③ 合盖继续运行  关掉系统的合盖睡眠开关(需要管理员密码,只问一次)
#   ④ 体检    一次查完配置/自启动/通道/防睡眠/云端,并给出结论
set -u

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${PRINT_AGENT_DATA_DIR:-$HOME/Library/Application Support/PrintAgent}"
APP="$HOME/Applications/PrintAgent.app"
APP_BIN="$APP/Contents/MacOS/print-agent"

WANT_CUPS=1
WANT_LID=1
WANT_DOCTOR=1
ARGS=()
for a in "$@"; do
  case "$a" in
    --no-cups)  WANT_CUPS=0 ;;
    --no-lid)   WANT_LID=0 ;;
    --minimal)  WANT_CUPS=0; WANT_LID=0; WANT_DOCTOR=0 ;;
    *)          ARGS+=("$a") ;;
  esac
done
set -- ${ARGS[@]+"${ARGS[@]}"}

# ---- 找产物:优先按本机芯片选(Apple Silicon → arm64,Intel → amd64),避免字母序误选;
#      找不到对应芯片版本时再回退到任意 darwin 产物(Intel 版可经 Rosetta 运行)。
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

SERVER="${1:-}"
TOKEN="${2:-}"
PRINTER="${3:-}"

echo "===== 打印代理一键安装(macOS) ====="
echo "程序目录: $DIR"
echo

# 安装完成后,**真正在跑的是 App 里那份**,配置也在它的数据目录
# (~/Library/Application Support/PrintAgent)。所以后面的通道配置与体检都要用 App 里的
# 可执行文件 —— 用解压目录这份会读到另一个位置,体检结果会显示「未配置云端地址」,
# 把人引向完全错误的方向(实测踩到)。
RUNTIME_BIN="$BIN"
if [ -x "$APP_BIN" ]; then
  RUNTIME_BIN="$APP_BIN"
fi

# ============================================================================
# ① 安装
# ============================================================================
echo "---- ① 安装 ----"
if [ "$WANT_LID" -eq 1 ]; then
  # 显式给 --no-sleep:合盖那一步本来就要做,顺带让 caffeinate 顶住空闲睡眠。
  # 显式传参也避免了「非交互时询问不到 / 沿用旧配置」的不确定行为。
  #
  # 若门店在 agent.env 或环境里明确关了防睡眠,则尊重它。
  if [ "${PRINT_AGENT_NO_SLEEP:-}" = "0" ]; then
    "$BIN" --install --server "$SERVER" --token "$TOKEN"
  else
    "$BIN" --install --server "$SERVER" --token "$TOKEN" --no-sleep
  fi
else
  "$BIN" --install ${SERVER:+--server "$SERVER"} ${TOKEN:+--token "$TOKEN"}
fi
RC=$?
if [ "$RC" -ne 0 ]; then
  echo
  echo "[失败] 安装没成功(退出码 $RC),后面几步就不做了。"
  echo "       常见原因:后台地址写成了带 /prod-api 或结尾斜杠的、令牌复制不全。"
  exit "$RC"
fi
echo

# ============================================================================
# ② 打印通道(系统打印服务)
# ============================================================================
if [ "$WANT_CUPS" -eq 1 ]; then
  echo "---- ② 打印通道 ----"
  if [ -n "$PRINTER" ]; then
    "$RUNTIME_BIN" --setup-cups "$PRINTER"
    RC=$?
  else
    # 没给 IP:用代理见过的地址(收到过打印任务就会记下来,见 printers.go)
    "$RUNTIME_BIN" --setup-cups auto
    RC=$?
    if [ "$RC" -ne 0 ]; then
      echo
      echo "[待办] 上面说了原因:本机还不知道打印机 IP。两种补法(任选其一):"
      echo "       1) 到管理后台点一次「测试打印」,等代理收到任务后再执行:"
      echo "            \"$RUNTIME_BIN\" --setup-cups auto"
      echo "       2) 直接给出 IP(在管理后台「打印机管理」里能看到):"
      echo "            \"$RUNTIME_BIN\" --setup-cups 192.168.1.133"
    fi
  fi
  echo
fi

# ============================================================================
# ③ 合盖继续运行
# ============================================================================
if [ "$WANT_LID" -eq 1 ]; then
  echo "---- ③ 合盖继续运行 ----"
  # 两件独立的事,少一件合盖后都会停摆:
  #   ① caffeinate(上一步 --install --no-sleep 已写进自启动)顶住「空闲睡眠」;
  #   ② 关掉系统级的「合盖睡眠」。合盖触发的是 Clamshell Sleep,属于电源管理层的
  #      独立触发器,caffeinate 用的 IOPMAssertion 抑制不了它 —— 实测接电源也照样睡:
  #        Entering Sleep state due to 'Clamshell Sleep' ... Using AC (Charge:100%)
  #   ② 需要 root,所以只能在这里替门店做掉(会要一次管理员密码)。
  if ioreg -r -c IOPMrootDomain -d 1 2>/dev/null | grep -q '"SleepDisabled" = Yes'; then
    echo "[已就绪] 合盖睡眠开关已关闭(合盖后 Mac 不会睡,代理继续取单打单)"
  elif [ -t 0 ]; then
    echo "当前合盖会让 Mac 睡眠,睡眠期间代理停摆(唤醒后自动续上,不丢单)。"
    echo "要让合盖后继续打单,需要关掉系统的合盖睡眠开关(仅这一台机器,一次性):"
    echo "    sudo pmset -a disablesleep 1"
    printf '现在就设置吗?[Y/n] '
    read -r _ans || _ans=""
    case "${_ans:-y}" in
      y|Y|yes|YES|"")
        if sudo pmset -a disablesleep 1; then
          echo "[已就绪] 已关闭合盖睡眠;现在合盖也能继续打单(请保持接交流电源)"
        else
          echo "[待办] 设置失败。请手工执行: sudo pmset -a disablesleep 1"
        fi
        ;;
      *) echo "[已跳过] 之后可随时手工执行: sudo pmset -a disablesleep 1" ;;
    esac
  else
    echo "[待办] 非交互运行,未设置合盖睡眠开关。请手工执行(需管理员密码):"
    echo "        sudo pmset -a disablesleep 1"
  fi
  echo "        注意:合盖长时间运行会持续耗电并发热,请务必接交流电源、留出散热空间。"
  echo "        想恢复「合盖即睡」: sudo pmset -a disablesleep 0"
  echo
fi

# ============================================================================
# ④ 体检
# ============================================================================
if [ "$WANT_DOCTOR" -eq 1 ]; then
  echo "---- ④ 体检 ----"
  "$RUNTIME_BIN" --doctor
  # 体检的非零退出码只代表「有项目需要处理」(例如没接电源),安装本身是成功的,
  # 所以这里不把它当成脚本失败。
fi

echo
echo "===== 完成 ====="
echo "程序(装成 App):  $APP"
echo "配置文件/日志:    $DATA_DIR"
echo "看状态与日志:     cd \"$DIR\" && ./p.sh"
# 注意路径:包里产物叫 print-agent-darwin-arm64/amd64,并没有 ./print-agent 这个文件;
# 真正在跑的是 App 里那份。随手写 ./print-agent 只会让门店得到「没有这个文件」。
echo "再体检一次:       \"$RUNTIME_BIN\" --doctor"
echo "真出张纸验证:     \"$RUNTIME_BIN\" --probe 192.168.1.133 --probe-print   # 换成门店打印机 IP"
echo "升级:             cd \"$DIR\" && ./upgrade.sh"
echo "卸载:             cd \"$DIR\" && ./uninstall.sh"
