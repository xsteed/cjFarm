# 后台管理系统在鸿蒙（HarmonyOS）上"变成 App"方案调研

> 目标：员工/管理员在鸿蒙设备（手机或平板）上通过桌面图标一键打开后台管理系统，体验接近原生 App——**重点是去掉浏览器地址栏/标签栏，做到全屏独立窗口**。
>
> 适用系统：本文覆盖 HarmonyOS 4.x 及更早（可直接装 APK）与 HarmonyOS 5.0 NEXT / 6 / 7（纯血鸿蒙，只认 HAP）两种情况。
>
> 现状约束（关键）：
> - 生产环境为**纯 IP 访问（111.230.154.50）+ HTTP**，未配置域名/HTTPS。
> - 前端为 Vue 3 + Vite（`frontend/`），接口 `baseURL` 硬编码为相对路径 `/api`（`packages/api/src/http.ts:8`）。
> - 后端为 Go + Gin + SQLite，跑在服务器（或门店局域网小主机）上，**不进手机**。

---

## 0. 先定调：APK/HAP 里只能装"前端壳"

后台管理系统 = `Vue3 前端 + Go/Gin 后端 + SQLite`。打包到设备时：

- ✅ 能装进去的：前端 `dist` + 一个 WebView 壳。
- ❌ **装不进去的：Go 后端**。理由：
  - 纯血鸿蒙（NEXT）应用只能加载随包分发且签名过的 `.so`，不允许 `fork/exec` 独立可执行文件，也不允许 App 监听端口对外服务；后台进程受系统管控会被杀。
  - 安卓 10+ 起应用私有目录不可执行（W^X），把 Go 二进制改名成 `libxxx.so` 塞进 `jniLibs` 才能 exec，还要自己管进程、写库、写日志、接打印机——在手机上跑这套是给自己挖坑。
  - 本仓库后端还要写 `data/dining.db`、监听 8080、接打印机/打印代理，都不适合放在手机里。

**正确架构：手机/平板只做客户端；后端继续跑在服务器或门店局域网小主机上**（后端已支持 `make build-linux-arm64`，可跑在树莓派/旧电脑上，断网也能在门店内网用）。

---

## 1. 你的设备决定走哪条路（2026-10 现状）

| 设备系统 | 能否装 APK | 能否装 HAP | 推荐路线 |
|---|---|---|---|
| **HarmonyOS 4.x 及更早（含 4.3、EMUI/MagicUI）** | ✅ 直接侧载/应用市场装 | — | 路线 A（Capacitor 安卓壳 APK） |
| **HarmonyOS 5.0 NEXT / 6 / 7（纯血鸿蒙）** | ❌ 装不了 | ✅ | 路线 B（ArkWeb 原生壳 HAP）或用路线 A 的 APK 走「卓易通」 |

> 市场背景：截至 2026-10，HarmonyOS 6 装机 7000 万+，HarmonyOS 7 已于 2026-09 公测。新买的华为设备基本都是纯血鸿蒙。
> 查版本：设置 → 关于本机 → 版本号。

---

## 2. 路线 A：Capacitor 安卓壳 APK（4.x 直接装；NEXT 借卓易通装）

**原理**：Capacitor 把 Web 工程包成标准 Android 工程，里面一个全屏 Activity 内嵌 WebView。装到鸿蒙 4.x 上就是原生体验（4.x 有 AOSP 兼容层，APK 直接装）；装到纯血鸿蒙则通过「卓易通」容器运行。

### 2.0 一键构建（仓库已实现，推荐）

仓库已内置 Capacitor 壳的完整构建能力，**一条命令出签名 APK**：

```bash
# 在仓库根目录
SERVER_URL=http://111.230.154.50 bash deploy/build-android-shell.sh
```

首次运行自动完成：安装 Capacitor 依赖 → 生成安卓工程（`frontend/android/`）→ 生成签名库 → 写入 `server.url` → 放行 HTTP 明文 → Gradle 构建 → zipalign 对齐 → apksigner 签名。

产物：`frontend/dist-android/dining-admin-<时间戳>.apk`（约 3.7 MB，minSdk 22 / targetSdk 34）。

| 文件 | 作用 |
|---|---|
| `frontend/capacitor.config.ts` | 壳配置：包名 `com.cjfarm.dining`、应用名「点餐管理」、`server.url` 由 `SERVER_URL` 注入 |
| `deploy/build-android-shell.sh` | 一键构建脚本（环境探测 / 签名 / 对齐 / 产物归档） |
| `deploy/gen-android-icons.py` | 从 `public/favicon-512x512.png` 生成各密度图标与自适应图标（`android/` 是生成物，图标必须每次重建） |
| `frontend/package.json` | 新增 `android:apk` 脚本与 Capacitor 6 依赖 |

常用写法：

```bash
# 推荐：地址写进本地配置文件（不入库），之后一条命令出包
cp deploy/android-shell.env.example deploy/android-shell.env   # 只需做一次
make apk

# 临时覆盖地址（命令行优先级最高）
SERVER_URL=https://dining.example.com make apk

# dist 已是最新，跳过前端构建提速
SKIP_BUILD=1 make apk

# 其他等价入口
bash deploy/build-android-shell.sh
cd frontend && npm run android:apk
```

签名材料 **必须备份**：`frontend/android-release.keystore` + `frontend/android-release.keystore.properties`（均已在 `.gitignore` 排除）。丢失后无法对已安装的 App 发更新，只能卸载重装。

> 环境要求：Node ≥ 18、JDK 17、Android SDK（platform 34 + build-tools 34）。Capacitor 固定在 6.2.x —— 7 需要 JDK 21、8 需要 Node 22。

### 2.1 关键简化：WebView 直接加载线上站点，几乎不用改前端

**强烈建议**让 WebView 的 `src` 直接指向线上地址 `http://111.230.154.50`，而不是把 `dist` 打包进 APK 离线加载。理由：

| 维度 | 加载线上 URL（推荐） | 内置 dist 离线加载 |
|---|---|---|
| `baseURL` 硬编码 `/api` | ✅ 同源 `/api`，无需改 | ❌ 本地 `file://`/`resource://` 与远程不同源，需改 |
| CORS | ✅ 同源请求，不涉及 | ❌ 本地 origin 是 `null`，后端 `cors.go` 白名单匹配不上 |
| 路由 `createWebHistory` | ✅ Nginx 已有 SPA 回退 | ❌ 本地刷新/深链 404，需改 hash 或拦截 |
| 更新前端 | ✅ 刷新即最新 | 需重新打包发 APK |
| 离线可用 | ❌ 需联网 | ✅ |

> 结论：**WebView → `http://服务器IP`** 一条路，本仓库代码零改动即可跑起来。代价只是必须放行 HTTP 明文（见 2.3）。

### 2.2 工程搭建

```bash
cd frontend
npm i -D @capacitor/core @capacitor/cli @capacitor/android
npx cap init "点餐系统" com.dining.admin --web-dir=dist
npm run build                       # 产出 dist
npx cap add android
npx cap open android                # 用 Android Studio 打开
```

### 2.3 必须配置（因为加载 HTTP 明文地址）

`android/app/src/main/AndroidManifest.xml` 的 `<application>` 标签加：

```xml
android:usesCleartextTraffic="true"
```

更稳妥的做法是用 Network Security Config 只对服务器 IP 放行明文：

```xml
<application android:networkSecurityConfig="@xml/network_security_config" ...>
```

`android/app/src/main/res/xml/network_security_config.xml`：

```xml
<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
  <domain-config cleartextTrafficPermitted="true">
    <domain includeSubdomains="true">111.230.154.50</domain>
  </domain-config>
</network-security-config>
```

> 注意：`111.230.154.50` 是 IP 不是域名，`<domain>` 里写裸 IP 即可（Android 接受）。如果以后换成域名 + HTTPS，这项配置就不需要了。

### 2.4 图片上传（菜品图/Logo/店铺 Logo）

管理端有「上传图片」功能（走 `/api/common/upload`）。Capacitor 的 Android 桥已内置 `WebChromeClient.onShowFileChooser`，WebView 的文件选择器会自动调起系统相册/文件管理器，**一般无需额外代码**。真机验证一下上传流程即可。

### 2.5 返回键

默认 Android 返回键会直接关闭 Activity。建议拦截为「先回退网页历史」：在 `MainActivity.java` 重写 `onBackPressed`，先 `webView.canGoBack() ? webView.goBack() : super.onBackPressed()`。Capacitor 也支持通过插件/事件处理，视工程结构而定。

### 2.6 打包签名

```bash
# 生成签名 keystore（务必长期保存，否则无法发更新）
keytool -genkey -v -keystore dining-release.keystore \
  -alias dining -keyalg RSA -keysize 2048 -validity 10000

# 在 android/ 下配置签名后构建
cd android && ./gradlew assembleRelease
# 产出 android/app/build/outputs/apk/release/app-release.apk
```

### 2.7 在鸿蒙上安装

- **4.x 设备**：设置 → 系统和更新 → 关闭「纯净模式（增强防护）」→ 点 APK 安装。
- **纯血鸿蒙（NEXT）**：应用市场搜「卓易通」安装 → 把 APK 传进手机 → 文件管理器点开 → 选「卓易通」打开 → 安装。限制：图标归集在卓易通文件夹里、不能移到桌面；依赖容器版本兼容性；部分能力（如微信登录）受容器限制。仅适合临时/应急。

### 2.8 已知限制（WebView 固有，与打包方式无关）

- **`window.print()` 不生效**：桌台二维码的「打印」按钮不会调起打印对话框。替代方案：用「下载 PNG」，或临时用系统浏览器打开站点打印。
- **`blob:` 下载在部分机型不生效**：二维码单张下载 / 批量导出 ZIP 若点了没反应，同样改用系统浏览器操作。
- 这两项若要根治，需要在壳里加原生下载/打印桥（JS Proxy + 文件保存 + PrintManager），属于额外开发量。

---

## 3. 路线 B：ArkWeb 原生壳 HAP（纯血鸿蒙正路）

**原理**：DevEco Studio 新建 Empty Ability，一个 `Web` 组件加载线上站点。这是 NEXT 上**唯一能确定拿到全屏无浏览器 UI** 的路线。

```ets
// entry/src/main/ets/pages/Index.ets
import { webview } from '@kit.ArkWeb';

@Entry
@Component
struct Index {
  controller: webview.WebviewController = new webview.WebviewController();

  build() {
    // 直接加载线上站点 = 同源，无需改前端 baseURL / CORS / 路由
    Web({ src: 'http://111.230.154.50', controller: this.controller })
      .width('100%').height('100%')
      .javaScriptAccess(true)
      .domStorageAccess(true)            // 登录态在 localStorage，必须开
      .onShowFileSelector(() => true)    // 图片上传走系统选择器
  }

  // 返回键先回退网页历史，而不是直接退出 App
  onBackPress(): boolean {
    if (this.controller.accessStep(-1)) {
      this.controller.backward();
      return true;
    }
    return false;
  }
}
```

`entry/src/main/module.json5` 必配：

```json5
{
  "module": {
    "requestPermissions": [{ "name": "ohos.permission.INTERNET" }],
    // 放行 HTTP 明文（鸿蒙默认禁止，会报 ERR_CLEARTEXT_NOT_PERMITTED）。
    // 字段名随 API 版本有差异：deviceConfig.network.cleartextTraffic /
    // module.networkSecurityConfig.cleartextTrafficPermitted /
    // $profile:network_config 三种写法都出现过，以你 DevEco 的 API 版本文档为准，真机验证。
  }
}
```

**分发签名**（比安卓繁琐，提前准备）：

| 方式 | 可行性 |
|---|---|
| 调试签名（DevEco 自动签名） | ✅ 最简单，但**只能装到 Profile 登记了 UDID 的设备**；AGC 调试证书最多 2 个 |
| 发布签名 + 上架应用市场 | ✅ 正规路径，需审核 |
| 企业证书 + MDM 私有应用商店 / HEM 批量部署 | ✅ 门店批量铺设备；官方说明企业应用主要面向鸿蒙电脑专业版/企业版擎云设备，手机端需向华为确认 |

> 成本：代码量极小（约 30 行），真正的耗时是 DevEco 环境 + AGC 签名，约半天到一天，之后一劳永逸。

---

## 4. 路线 C：PWA 添加至桌面（0 成本，但拿不到全屏）

`frontend/public/manifest.webmanifest` 已配置 `display: standalone`、图标、主题色，`index.html` 已挂 `<link rel="manifest">` 与 `mobile-web-app-capable`。

- **4.x 设备**：浏览器（建议 Chrome/Edge/Via 等）打开站点 → 菜单 → 添加至主屏幕 → 可得 standalone 全屏独立窗口（与安卓一致）。
- **纯血鸿蒙**：华为浏览器「添加至桌面」只生成**网页快捷方式**，**不兑现 `standalone`**——点开仍带浏览器 UI。对此华为官方从未承诺去掉地址栏。配置再对也救不回来，网页侧没有任何 API 能移除宿主浏览器 UI。
- 检查是否走了快应用：看桌面图标右下角有没有**蓝色小闪电标**（鸿蒙对识别到 manifest 的站点会用快应用创建快捷方式）。无论哪种，实测仍带浏览器 UI。

---

## 5. 本仓库需要配合改的点（仅当要"内置 dist 离线加载"时才需要）

如果选"WebView/ArkWeb 直接加载线上 URL"（本文推荐），**以下全都不用改**。只有把 `dist` 打包进壳离线运行时才要处理：

1. **`baseURL` 可注入**（非必须）：`packages/api/src/http.ts:8` 写死 `export const baseURL = '/api'`。离线内嵌时需要支持绝对地址（参考 `apps/miniprogram/src/api/http.ts` 已有的 `configureHttp()` 模式，改成 `window.__API_BASE__` 或 `import.meta.env.VITE_API_BASE`）。
2. **CORS 放 `null` origin**：`backend/interfaces/middleware/cors.go:59` 是精确匹配 `Origin` 头。本地加载时 WebView 发出的 `Origin` 是 `null`，白名单匹配不上，需改后端放行（或直接走线上 URL 同源方案规避）。
3. **路由改 hash 模式**：`src/router/index.ts:34` 用的 `createWebHistory`。本地加载时刷新/深链会 404，需改 hash 或在壳里拦截请求。

---

## 6. 顺手解决明文：上 HTTPS 一劳永逸

不管走哪条路线，当前纯 IP + HTTP 都会带来"明文放行"这种小麻烦。一条命令即可抹平：

```bash
# 1) 域名解析到 111.230.154.50 + ICP 备案（境内服务器必需）
# 2) certbot 签发续期
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d 你的域名
# 3) 一键启用 HTTPS（脚本自动开 80→443 跳转、替换证书路径）
sudo bash deploy/setup-nginx.sh --domain 你的域名 \
  --cert /etc/nginx/ssl/dining.crt --key /etc/nginx/ssl/dining.key
```

上 HTTPS 后：明文配置消失、PWA 在 4.x 上自动升级为完整安装体验、未来想发 TWA 也够了。

---

## 7. 推荐与落地清单

**按你的版本选型：**

- **鸿蒙 4.x（你当前这批设备）**：直接走**路线 A（Capacitor 安卓壳 APK）**，WebView 指向线上站点，零前端改动，半天出包。
- **纯血鸿蒙（未来升级到 NEXT/6/7）**：路线 A 的 APK 借「卓易通」应急，或提前做**路线 B（ArkWeb 壳 HAP）**正路。

**为什么不内置 dist 离线跑：** 见第 0、5 节——纯本地方案要同时改 baseURL、CORS、路由三处，且 CORS 的 `null` origin 还要动后端，得不偿失。直接加载线上 URL 是最省事的。

**体验提醒：** 后台有 13 个管理页 + 侧栏布局，手机上会很挤，**目标设备建议横屏平板（MatePad）而非手机**。

### 后续实施清单

1. [ ] 安卓环境：安装 Android Studio + JDK 17。
2. [ ] `frontend` 下 `npm i` Capacitor 三件套 → `cap init` → `npm run build` → `cap add android`。
3. [ ] `AndroidManifest.xml` 加 `usesCleartextTraffic`（或只对 IP 放行的 Network Security Config）。
4. [ ] 生成签名 keystore，`./gradlew assembleRelease` 出 APK。
5. [ ] 鸿蒙 4.x 设备关闭纯净模式增强防护后安装验证；重点验：全屏无地址栏、登录态持久、图片上传、返回键行为。
6. [ ] （可选）纯血鸿蒙设备：评估路线 B 的 ArkWeb 壳 HAP + AGC 签名；或用卓易通临时跑 APK。
7. [ ] （推荐）趁早做域名 + 备案 + HTTPS，根除明文限制。
