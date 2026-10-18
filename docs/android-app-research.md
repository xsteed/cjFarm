# 后台管理系统 Web 端在安卓上"变成 App"方案调研

> 目标：员工/管理员在安卓手机上通过桌面图标一键打开后台管理系统，体验接近原生 App。
>
> 现状约束（关键）：
> - 生产环境为**纯 IP 访问（111.230.154.50）+ HTTP**，未配置域名。
> - `deploy/nginx-production.conf` 中 HTTPS 块默认注释；纯 IP 无法申请公信 CA 证书（Let's Encrypt 等均不支持给裸 IP 签发，部分 CA 商业证书可以但贵且需备案配合）。
> - 前端为 Vue + Vite（`frontend/`），已复用现有图标资源。

---

## 方案总览对比

| | 方案 1：PWA 添加到主屏幕 | 方案 2：TWA 打包 APK | 方案 3：WebView 壳 App |
|---|---|---|---|
| 开发成本 | 极低（已完成） | 中（无需写原生代码） | 中高（需维护 Android 工程） |
| 是否需要 HTTPS | **必须**（standalone 安装） | 必须（数字资产链接校验走 HTTPS） | **不需要，HTTP/IP 可用** |
| 是否需要域名 | 需要（配合 HTTPS） | 需要 | 不需要 |
| 桌面图标一键打开 | ✅ | ✅ | ✅ |
| 全屏无浏览器 UI | ✅（standalone） | ✅（全屏 Activity） | ✅（全屏 Activity） |
| 独立任务卡片、不混在浏览器里 | ✅ | ✅ | ✅ |
| 分发方式 | 用户在浏览器菜单里操作 | 发 APK 安装包 / 应用市场 | 发 APK 安装包 |
| 版本更新 | 刷新即最新（零成本） | APK 内页面即 Web，刷新即最新 | 同左 |
| 离线/推送等原生能力 | 需 Service Worker + 推送服务 | 有限 | 可通过插件扩展（扫码、推送等） |

---

## 方案 1：PWA（已实现）

### 已完成的改动

1. 新增 `frontend/public/manifest.webmanifest`：
   - `display: standalone`（全屏、无浏览器 UI）
   - `theme_color: #FF6B35`（与现有品牌色一致）
   - 图标复用现有 `favicon-192x192.png` / `favicon-512x512.png`（512 版同时声明 `maskable` 用途，安卓自适应图标需要）
2. `frontend/index.html` 增加：
   - `<link rel="manifest" href="/manifest.webmanifest">`
   - `mobile-web-app-capable` / `apple-mobile-web-app-capable` meta（兼容旧安卓浏览器与 iOS）

### 使用方式（HTTPS 环境下）

手机 Chrome 打开站点 → 右上角菜单 → **「添加到主屏幕」/「安装应用」** → 桌面出现图标，点开即独立全屏窗口。

### ⚠️ 当前生产环境的限制

- Chrome 对 PWA "安装"（standalone 模式）要求 **HTTPS 安全上下文**。
- 当前纯 IP + HTTP 部署下，"添加到主屏幕"只会生成**普通快捷方式**：桌面图标有、一键打开有，但点开后仍带浏览器地址栏/标签页 UI，**不是**独立 App 窗口。
- 部分国产浏览器（微信内置 X5/QQ 浏览器等）对 PWA manifest 支持不一致，建议引导用户使用 Chrome / Edge / Via 等。

### 启用 HTTPS 的路径（按优先级）

1. **申请域名 + 备案 + Let's Encrypt（推荐）**
   - 域名解析到 111.230.154.50，完成 ICP 备案（境内服务器必需，见 nginx 配置文件内注释）。
   - 用 `sudo bash deploy/setup-nginx.sh --domain <域名>` 一键启用 HTTPS（脚本自动开 80→443 跳转、替换证书路径）。
   - 证书用 certbot 自动签发续期：
     ```bash
     sudo apt install certbot python3-certbot-nginx
     sudo certbot --nginx -d <域名>
     ```
2. **商业 CA 的 IP 证书**：少数 CA（如 ZeroSSL 付费版）支持给公网 IP 签证书，成本高，一般不推荐。
3. **自签证书**：手机端需手动信任根证书，Chrome 对不受信证书仍视为不安全上下文，PWA 安装不可靠，不推荐。

---

## 方案 2：TWA（Trusted Web Activity）打包 APK

**原理**：用 Chrome Custom Tab 承载网页的官方打包方式。APK 本身只有一个全屏 Activity，内容 100% 是你的 Web 站点，无需写任何 Java/Kotlin 业务代码。

### 工具链

- **PWABuilder**（https://www.pwabuilder.com）：网页输入 PWA 地址，直接在线生成 Android APK/AAB，最简单。
- **Bubblewrap CLI**（Google 官方，PWABuilder 底层即它）：
  ```bash
  npm i -g @bubblewrap/cli
  bubblewrap init --manifest https://<域名>/manifest.webmanifest
  bubblewrap build          # 产出 app-release-signed.apk
  ```
- **Android Studio + SVGOMG/PWABuilder 生成工程**：需要更深度定制时用。

### 前提条件

1. 站点必须是 **HTTPS + 有效证书**（硬性要求：Chrome 启动 TWA 前会校验 Digital Asset Links，走 `https://<域名>/.well-known/assetlinks.json`）。
2. 需要域名（assetlinks 校验基于域名）。
3. 生成 APK 需要：
   - 一个签名密钥库（keystore），`bubblewrap` 首次构建时会自动生成，**务必长期保存**，否则无法发布更新。
   - 从 assetlinks.json 反向操作：拿到 APK 签名指纹后，把 `assetlinks.json` 部署到服务器 `/.well-known/` 路径（nginx 加一个 location 即可）。

### 与当前约束的关系

- **当前纯 IP + HTTP 无法使用本方案**，必须先完成方案 1 中的 HTTPS + 域名改造。
- 域名 + HTTPS 就绪后，本方案约半天工作量即可产出可分发的 APK，且后续站点更新**无需重新发 APK**（内容就是网页）。
- 未上架应用市场时，APK 分发需用户允许"安装未知来源应用"。

### 结论

适合作为"HTTPS 改造完成之后"的进阶选项。如果届时 PWA 安装已满足需求，本方案可不做。

---

## 方案 3：WebView 壳 App

**原理**：一个原生 Android 工程，里面一个全屏 Activity 内嵌 WebView 加载 `http://111.230.154.50`。**不要求 HTTPS、不要求域名**，是当前约束下唯一能拿到"真 APK、全屏、带桌面图标"效果的路线。

### 子路线对比

| 子路线 | 说明 | 适合度 |
|---|---|---|
| **Capacitor**（推荐） | Ionic 出品，`npm` 生态，把 Web 工程直接包成 Android/iOS 工程，插件生态丰富 | ★★★ |
| 原生 Android WebView 工程 | 手写一个 ~200 行的 WebView Activity（含返回键处理、文件上传桥接、SSL 容错） | ★★ |
| Cordova | 老牌方案，生态老化，新项目不推荐 | ★ |

### Capacitor 要点

```bash
cd frontend
npm i @capacitor/core @capacitor/cli @capacitor/android
npx cap init "点餐系统" com.dining.admin --web-dir=dist
npm run build && npx cap add android
npx cap open android   # 用 Android Studio 打开，配置允许明文流量后打 APK
```

> **本仓库已实现，无需手工操作**：`SERVER_URL=http://111.230.154.50 bash deploy/build-android-shell.sh`
> 自动完成依赖安装、工程生成、明文放行、签名与出包，产物在 `frontend/dist-android/`。
> 详见 [`docs/harmonyos-app-research.md`](harmonyos-app-research.md)「2.0 一键构建」。

关键配置（因为加载 HTTP 明文地址）：
- `android/app/src/main/AndroidManifest.xml` 加 `android:usesCleartextTraffic="true"`（或 network security config 只对 `111.230.154.50` 放行明文，更安全）。
- WebView 的文件上传需处理 `WebChromeClient.onShowFileChooser`（Capacitor 已内置）。
- 打包产物为 APK：`cd android && ./gradlew assembleRelease`，同样需要签名 keystore。

### 优缺点

- ✅ 不依赖 HTTPS/域名改造，当前环境立即可行。
- ✅ 可演进：以后加扫码登录、消息推送等原生能力有现成插件。
- ⚠️ 需要一次 Android 开发环境（Android Studio + JDK 17）和签名管理；WebView 版本依赖用户手机 Chrome/WebView 组件。
- ⚠️ 换服务器 IP 时需要重新发 APK（可把地址做成启动配置或远程配置缓解）。

---

## 鸿蒙（HarmonyOS）支持情况

> 更详尽的版本选型、Capacitor/ArkWeb 工程步骤与本仓库改造点，见独立文档 **[`docs/harmonyos-app-research.md`](harmonyos-app-research.md)**（2026-10 整理）。

**结论先行：能不能装 APK，取决于设备鸿蒙版本——这是分水岭。**

| 鸿蒙版本 | 能否装 APK | 能否装 HAP | 推荐路线 |
|---|---|---|---|
| **4.x 及更早（含 4.3、EMUI/MagicUI）** | ✅ 直接侧载/应用市场装（保留 AOSP 兼容层） | — | 本文方案 3（Capacitor 安卓壳 APK）直接可用 |
| **5.0 NEXT / 6 / 7（纯血鸿蒙）** | ❌ 不能装 APK | ✅ 只认 HAP | 鸿蒙原生 ArkWeb 壳 HAP；或方案 3 的 APK 借「卓易通」容器运行 |

> 截至 2026-10：HarmonyOS 6 装机 7000 万+，HarmonyOS 7 已于 2026-09 公测。新购华为设备基本是纯血鸿蒙。
> 查版本：设置 → 关于本机 → 版本号。

### 各路线在鸿蒙上的实际表现

- **PWA 添加至桌面（方案 1）**：
  - 4.x：浏览器（Chrome/Edge/Via 等）打开站点 → 添加至主屏幕 → 可得 `standalone` 全屏独立窗口，与安卓一致。
  - 纯血鸿蒙：华为浏览器「添加至桌面」只生成**网页快捷方式**，**不兑现 `display: standalone`**——点开仍带浏览器地址栏/标签栏。图标若带蓝色小闪电标说明走了快应用通道，但实测仍带浏览器 UI。网页侧没有任何 API 能移除宿主浏览器 UI。
- **TWA（方案 2）**：本质是 Chrome Custom Tab，纯血鸿蒙没有 Chrome，路线失效；4.x 上同安卓。
- **WebView 壳 APK（方案 3，Capacitor）**：
  - **4.x：直接装，全屏原生体验**，本文首选。WebView 指向线上站点即可，前端零改动（见下）。
  - 纯血鸿蒙：应用市场搜「卓易通」（华为官方安卓兼容容器，iSulad 引擎）安装 → 导入 APK → 安装。限制：图标归集在卓易通文件夹、不能移到桌面；依赖容器版本；部分能力（如微信登录）受容器限制。仅应急。

### 方案 3 在鸿蒙上的要点（4.x 直接可用）

1. **WebView 直接加载线上 `http://服务器IP`**，不要内置 dist 离线跑——这样 `baseURL: '/api'` 同源、CORS 不涉及、路由 `createWebHistory` 由 Nginx 回退兜底，本仓库代码零改动。
2. **放行 HTTP 明文**：`AndroidManifest.xml` 的 `<application>` 加 `android:usesCleartextTraffic="true"`（或更精细的 Network Security Config 只对服务器 IP 放行）。
3. **签名**：`cd android && ./gradlew assembleRelease`，需长期保存 keystore。
4. **安装**：4.x 设备先关闭「纯净模式（增强防护）」再点 APK 安装。

### 纯血鸿蒙的正路（NEXT/6/7）

要走确定全屏 + 可批量分发，只能做鸿蒙原生 ArkWeb 壳 HAP（DevEco Studio + AGC 签名；调试签名仅限登记 UDID 的设备，批量需企业证书/MDM）。完整步骤见 `docs/harmonyos-app-research.md`「路线 B」。



- **短期（当前 IP + HTTP，鸿蒙 4.x 设备）**：方案 1 的改动已就位——HTTP 下桌面快捷方式仍带浏览器 UI；要全屏 App 体验，直接走**方案 3（Capacitor 安卓壳 APK）**，4.x 可原生安装，WebView 指向线上站点即可零前端改动。
- **短期（纯血鸿蒙 NEXT/6/7 设备）**：PWA 加桌面只给网页快捷方式（带浏览器 UI）；要全屏只能做鸿蒙原生 ArkWeb 壳 HAP（见 `docs/harmonyos-app-research.md`），或用方案 3 的 APK 借「卓易通」应急。
- **中期（申请域名 + 备案 + HTTPS 后）**：方案 1 在 4.x 上自动升级为完整 PWA 安装体验；如仍需发 APK，用**方案 2（Bubblewrap/PWABuilder）**半天即可完成，且一次打包长期复用。HTTPS 还能顺手抹平各路线的明文放行配置。
- 不建议：自签证书路线；以及在没有 HTTPS 前提下强推 TWA。

## 后续实施清单（可选）

1. [ ] 若走域名+HTTPS：购买/注册域名 → ICP 备案 → certbot 签发 → `deploy/setup-nginx.sh --domain` 启用 HTTPS。
2. [ ] 若走 Capacitor：`frontend` 下初始化 Capacitor、放行 IP 明文流量、生成签名 keystore、构建 APK。
3. [ ] 若走 TWA：域名 HTTPS 就绪后，PWABuilder 生成 APK，部署 `/.well-known/assetlinks.json`。
4. [ ] 可选：为 PWA 补充简单 Service Worker（管理后台非必需）。
