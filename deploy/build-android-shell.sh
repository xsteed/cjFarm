#!/usr/bin/env bash
# ============================================================================
# 点餐管理系统 —— 安卓壳 APK 一键构建（Capacitor）
#
# 产物是什么：
#   一个标准 Android APK，里面只有一个全屏 WebView，直接加载线上前端地址。
#   页面与 /api、/uploads 同源，所以接口一次通、前端零改造、后端零改造。
#   前端更新后 App 内刷新即生效，不用重新发版。
#
# 支持的系统：
#   所有安卓手机/平板/安卓 POS 机；HarmonyOS 4.x 及更早（带 AOSP 兼容层）。
#   纯血鸿蒙（HarmonyOS 5.0 NEXT / 6 / 7）不能装 APK，需走 ArkWeb 壳 HAP，
#   见 docs/harmonyos-app-research.md。
#
# 用法：
#   SERVER_URL=http://111.230.154.50 bash deploy/build-android-shell.sh
#   SERVER_URL=https://dining.example.com  bash deploy/build-android-shell.sh
#
# 可选环境变量：
#   SERVER_URL        必填，壳加载的前端地址（不要带末尾斜杠）
#   KEYSTORE_PASS     签名库口令；首次运行未指定时自动生成随机口令并写入本地属性文件
#   KEY_PASS          密钥口令；默认与 KEYSTORE_PASS 相同
#   SKIP_SYNC=1       跳过 cap sync（仅改签名等、不想重新同步时用）
#   SKIP_BUILD=1      跳过前端构建（dist 已是最新时提速）
#
# 依赖：Node >= 18、JDK 17、Android SDK（platform 34 + build-tools 34）
# ============================================================================
set -euo pipefail

# ---- 路径与基础变量 --------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FRONTEND_DIR="$REPO_ROOT/frontend"
ANDROID_DIR="$FRONTEND_DIR/android"
OUT_DIR="$FRONTEND_DIR/dist-android"
KEYSTORE="$FRONTEND_DIR/android-release.keystore"
KEYSTORE_PROPS="$FRONTEND_DIR/android-release.keystore.properties"
KEY_ALIAS="dining"

log()  { printf '\033[32m[build-apk]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[build-apk]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31m[build-apk]\033[0m %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,27p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0; }
case "${1:-}" in -h|--help) usage ;; esac

# ---- 0. 本地配置（可选） ---------------------------------------------------
# deploy/android-shell.env 是本机私有配置（不入库，模板见 android-shell.env.example）。
# 配好之后直接 `make apk` 或 `bash deploy/build-android-shell.sh` 即可，无需每次带地址。
# 优先级：命令行的真实环境变量 > 本地配置文件 > 脚本默认值。
_env_server_url="${SERVER_URL:-}"
CONF_FILE="$SCRIPT_DIR/android-shell.env"
if [ -f "$CONF_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$CONF_FILE"
  set +a
  log "已读取本地配置 $(basename "$CONF_FILE")"
fi
SERVER_URL="${_env_server_url:-${SERVER_URL:-}}"

# ---- 1. 参数校验 -----------------------------------------------------------
if [ -z "$SERVER_URL" ]; then
  warn "未指定 SERVER_URL，回退到默认地址 http://111.230.154.50"
  SERVER_URL="http://111.230.154.50"
fi
SERVER_URL="${SERVER_URL%/}"
case "$SERVER_URL" in
  http://*|https://*) ;;
  *) die "SERVER_URL 必须以 http:// 或 https:// 开头，当前: $SERVER_URL" ;;
esac

# ---- 2. 环境探测 -----------------------------------------------------------
command -v node >/dev/null 2>&1 || die "未找到 node，请安装 Node.js 18+"
command -v npm  >/dev/null 2>&1 || die "未找到 npm"
command -v python3 >/dev/null 2>&1 || die "未找到 python3（用于生成图标与补写清单）"

[ -f "$FRONTEND_DIR/capacitor.config.ts" ] || die "缺少 $FRONTEND_DIR/capacitor.config.ts"

# Android SDK：优先 ANDROID_HOME / ANDROID_SDK_ROOT，缺失时按常见安装位置探测
if [ -z "${ANDROID_HOME:-}" ] && [ -n "${ANDROID_SDK_ROOT:-}" ]; then
  ANDROID_HOME="$ANDROID_SDK_ROOT"
fi
if [ -z "${ANDROID_HOME:-}" ]; then
  for d in "$HOME/Library/Android/sdk" "$HOME/Android/Sdk" "/usr/lib/android-sdk"; do
    if [ -d "$d" ]; then ANDROID_HOME="$d"; break; fi
  done
fi
[ -n "${ANDROID_HOME:-}" ] && [ -d "$ANDROID_HOME" ] || die "未找到 Android SDK，请设置 ANDROID_HOME"
export ANDROID_HOME

JAVA_BIN="${JAVA_HOME:+$JAVA_HOME/bin/java}"
JAVA_BIN="${JAVA_BIN:-java}"
"$JAVA_BIN" -version >/dev/null 2>&1 || die "未找到 java，请安装 JDK 17（Capacitor 6 要求）"
JAVA_MAJOR="$("$JAVA_BIN" -version 2>&1 | sed -n 's/.*version "\([0-9]*\).*/\1/p' | head -1)"
[ "${JAVA_MAJOR:-0}" -ge 17 ] || die "JDK 版本过低（当前 ${JAVA_MAJOR}），Capacitor 6 需要 JDK 17+"

log "SERVER_URL = $SERVER_URL"
log "ANDROID_HOME = $ANDROID_HOME"

# ---- 3. 依赖与 webDir ------------------------------------------------------
cd "$FRONTEND_DIR"

if [ ! -d node_modules/@capacitor/cli ]; then
  log "安装 Capacitor 依赖 ..."
  npm i -D "@capacitor/cli@^6.2.2" "@capacitor/android@^6.2.2" >/dev/null
  npm i "@capacitor/core@^6.2.2" >/dev/null
fi

# cap sync 会校验 webDir 是否存在。壳走 server.url，本地资源不参与运行，
# 但仍需目录存在，因此 dist 缺失时先构建一次。
if [ "${SKIP_BUILD:-0}" != "1" ] && [ ! -f "$FRONTEND_DIR/dist/index.html" ]; then
  log "构建前端产物（仅用于满足 webDir 校验，运行时加载的是 SERVER_URL）..."
  npm run build
fi
[ -f "$FRONTEND_DIR/dist/index.html" ] || die "缺少 frontend/dist，请先执行 npm run build"

# ---- 4. 生成安卓工程（一次即可，之后复用） ---------------------------------
if [ ! -d "$ANDROID_DIR" ]; then
  log "生成安卓工程（npx cap add android）..."
  npx cap add android
fi

# 让 Gradle 无需环境变量也能定位 SDK
printf 'sdk.dir=%s\n' "$ANDROID_HOME" > "$ANDROID_DIR/local.properties"

# ---- 4.5 生成应用图标（android/ 是生成物，图标必须每次重建） ----------------
ICON_SRC="$FRONTEND_DIR/public/favicon-512x512.png"
if [ -f "$ICON_SRC" ]; then
  log "生成应用图标（源：public/favicon-512x512.png）..."
  python3 "$SCRIPT_DIR/gen-android-icons.py" "$ICON_SRC" "$ANDROID_DIR/app/src/main/res"
else
  warn "未找到 ${ICON_SRC}，沿用 Capacitor 默认图标"
fi

# ---- 5. 签名材料（首次自动生成，之后沿用，务必长期保存） -------------------
if [ ! -f "$KEYSTORE" ]; then
  if [ -f "$KEYSTORE_PROPS" ]; then
    die "属性文件存在但签名库缺失：$KEYSTORE
        keystore 丢失后无法对已安装的 App 发更新（只能卸载重装）。
        如确认要重新开始，请删除 $KEYSTORE_PROPS 后再运行。"
  fi
  # 用 python3 生成：`tr </dev/urandom | head -c N` 会在 pipefail 下因 SIGPIPE 静默退出
  KEYSTORE_PASS="${KEYSTORE_PASS:-$(python3 -c 'import secrets,string;print("".join(secrets.choice(string.ascii_letters+string.digits) for _ in range(20)))')}"
  KEY_PASS="${KEY_PASS:-$KEYSTORE_PASS}"
  log "生成签名库（请长期保存 ${KEYSTORE}，丢失将无法覆盖升级）..."
  "$JAVA_BIN" -version >/dev/null 2>&1
  keytool -genkeypair -v \
    -keystore "$KEYSTORE" \
    -alias "$KEY_ALIAS" \
    -keyalg RSA -keysize 2048 -validity 10000 \
    -storepass "$KEYSTORE_PASS" -keypass "$KEY_PASS" \
    -dname "CN=Dining Admin, OU=Store, O=cjFarm, L=Local, ST=Local, C=CN" >/dev/null
  cat > "$KEYSTORE_PROPS" <<EOF
# 安卓壳签名材料（本地私有，不入库）。
# 丢失 = 无法对已安装的 App 发更新，需卸载重装，请与 keystore 一起备份。
storePassword=$KEYSTORE_PASS
keyPassword=$KEY_PASS
keyAlias=$KEY_ALIAS
EOF
  chmod 600 "$KEYSTORE_PROPS" "$KEYSTORE"
  log "签名口令已写入 ${KEYSTORE_PROPS}（请备份）"
else
  [ -f "$KEYSTORE_PROPS" ] || die "签名库存在但缺少口令文件：$KEYSTORE_PROPS
        无法从 keystore 反推口令，请找回备份，或删除 keystore 重新生成（会改变签名，需卸载旧版）。"
fi

# shellcheck disable=SC1090
. "$KEYSTORE_PROPS"   # 提供 storePassword / keyPassword / keyAlias

# ---- 6. 同步 Capacitor 配置（写入 server.url 与明文放行） -------------------
if [ "${SKIP_SYNC:-0}" != "1" ]; then
  log "同步壳配置（SERVER_URL 将写入 APK）..."
  SERVER_URL="$SERVER_URL" npx cap sync android
fi

# ---- 7. 兜底放行 HTTP 明文 -------------------------------------------------
# capacitor.config.ts 的 server.cleartext 已让 cap sync 写该属性；
# 不同 Capacitor 版本行为略有差异，这里再做一次幂等补写，避免装完白屏。
MANIFEST="$ANDROID_DIR/app/src/main/AndroidManifest.xml"
[ -f "$MANIFEST" ] || die "未找到 AndroidManifest.xml: $MANIFEST"
python3 - "$MANIFEST" <<'PY'
import re, sys
path = sys.argv[1]
src = open(path, encoding='utf-8').read()
if 'usesCleartextTraffic' in src:
    sys.exit(0)
new, n = re.subn(r'(<application\b)', r'\1\n        android:usesCleartextTraffic="true"',
                 src, count=1)
if n == 0:
    sys.exit('AndroidManifest.xml 中未找到 <application> 标签')
open(path, 'w', encoding='utf-8').write(new)
PY

# ---- 8. 构建未签名 APK -----------------------------------------------------
log "Gradle 构建 release APK（首次会下载 Gradle 发行版，可能较慢）..."
cd "$ANDROID_DIR"
chmod +x ./gradlew 2>/dev/null || true
./gradlew --no-daemon assembleRelease

UNSIGNED_APK="$(find "$ANDROID_DIR/app/build/outputs/apk/release" -maxdepth 1 -name '*.apk' -print -quit)"
[ -n "$UNSIGNED_APK" ] || die "未找到 Gradle 产物（app/build/outputs/apk/release/*.apk）"

# ---- 9. 对齐 + 签名 --------------------------------------------------------
BUILD_TOOLS="$(ls -1d "$ANDROID_HOME"/build-tools/* 2>/dev/null | sort -V | tail -1)"
ZIPALIGN="$BUILD_TOOLS/zipalign"
APKSIGNER="$BUILD_TOOLS/apksigner"
[ -x "$ZIPALIGN" ]  || die "未找到 zipalign（${BUILD_TOOLS}），请安装 Android build-tools"
[ -x "$APKSIGNER" ] || die "未找到 apksigner（${BUILD_TOOLS}），请安装 Android build-tools"

mkdir -p "$OUT_DIR"
STAMP="$(date '+%Y%m%d-%H%M')"
ALIGNED_APK="$OUT_DIR/app-aligned.apk"
FINAL_APK="$OUT_DIR/dining-admin-$STAMP.apk"

log "对齐并签名 ..."
"$ZIPALIGN" -f -p 4 "$UNSIGNED_APK" "$ALIGNED_APK"
"$APKSIGNER" sign \
  --ks "$KEYSTORE" \
  --ks-key-alias "$keyAlias" \
  --ks-pass "pass:$storePassword" \
  --key-pass "pass:$keyPassword" \
  --v4-signing-enabled false \
  --out "$FINAL_APK" \
  "$ALIGNED_APK"
rm -f "$ALIGNED_APK"
"$APKSIGNER" verify "$FINAL_APK"

# ---- 10. 结果 --------------------------------------------------------------
SIZE="$(du -h "$FINAL_APK" | awk '{print $1}')"
log "构建完成：$FINAL_APK ($SIZE)"
cat <<EOF

安装方法：
  安卓手机/平板：允许「安装未知来源应用」后，点开 APK 安装
  HarmonyOS 4.x：设置 → 系统和更新 → 关闭「纯净模式(增强防护)」→ 点开 APK 安装
  纯血鸿蒙 5/6/7：装不了 APK，改用 ArkWeb 壳 HAP（见 docs/harmonyos-app-research.md）

已知限制（WebView 固有）：
  - window.print() 无效：桌台二维码的「打印」按钮不会调起打印，请用「下载 PNG」或改用浏览器打印
  - blob: 下载在部分机型不生效：二维码/批量导出若无法保存，先用系统浏览器打开站点操作
EOF
