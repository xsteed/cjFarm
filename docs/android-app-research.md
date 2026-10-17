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

结论先行：**鸿蒙老系统（4.x 及以前）完全支持；鸿蒙 NEXT（5.0+ 纯血鸿蒙）能加桌面图标一键打开，但无法保证 Chrome-less 全屏独立窗口。**

| 鸿蒙版本 | 内核 | PWA 支持度 | 预期体验 |
|---|---|---|---|
| HarmonyOS 4.x 及以前（含 EMUI/MagicUI） | AOSP，可装 Chrome | **完整支持**，与安卓完全一致 | standalone 全屏、返回键、安全区全部等同安卓 |
| HarmonyOS 5.0 / NEXT（纯血鸿蒙） | 无 AOSP，只有华为浏览器等原生应用 | **部分支持**：能加桌面，但无完整 PWA 安装承诺 | 桌面图标一键打开可用；是否去掉浏览器 UI 不确定 |

### HarmonyOS NEXT 实测口径（华为官方支持页 + 公开资料）

1. **能加桌面**：华为浏览器「网页添加至桌面」在 HarmonyOS NEXT 5.0.0.123 SP19 + 华为浏览器 5.0.8.300 以上版本提供，入口在网页底部「∷ → 添加至桌面」。HarmonyOS 5.0 以下系统同样支持（EMUI 5.0 ~ HarmonyOS 4.x 均覆盖）。
2. **PWA 站点有特殊处理**：华为官方 FAQ 明确说明——支持 PWA 的网页加桌面后，系统会通过**快应用（Quick App）**创建快捷方式（图标带蓝色小闪电标），不影响功能使用。说明 NEXT 对 manifest 有识别能力。
3. **全屏独立窗口不保证**：公开教程与官方文档均只描述为「网页快捷方式 / 一键直达此网页」，**没有承诺 `display: standalone` 的全屏无浏览器 UI 效果**；Service Worker、离线、Web Push 的支持情况华为无公开承诺。鸿蒙 6 的浏览器 PWA 支持在华为开发者论坛有用户提问（2025-12），尚无官方明确结论。
4. **安全区**：NEXT 华为浏览器基于 Web 标准内核，`env(safe-area-inset-*)` 大概率有效，但无官方文档背书，**需真机验证**（建议拿到鸿蒙真机后按 iOS 的检查清单走一遍：顶栏是否被状态栏压、底部是否多白边）。

### 鸿蒙上的原生 App 正路

若要在鸿蒙 NEXT 上获得确定的原生 App 体验，只能走鸿蒙自己的生态：

- **鸿蒙原生应用（ArkTS）**：唯一完整体验（Web 组件内嵌网页或原生重写）。
- **元服务 / 快应用**： HarmonyOS 的轻应用形态，可分发到桌面，但需按快应用标准（rpk 包）开发。
- **注意**：NEXT 不能运行 APK，所以「安卓 WebView 壳 APK」路线在鸿蒙 NEXT 上直接失效；HarmonyOS 4.x 及以前则 APK 路线可用。

### 建议

- 存量鸿蒙 4.x 用户：现有 PWA 方案直接可用，无需任何改动。
- 鸿蒙 NEXT 用户：引导其用华为浏览器打开并「添加至桌面」，至少获得桌面图标一键打开；全屏效果以真机为准。
- 若鸿蒙 NEXT 用户占比高且对体验要求强，再评估鸿蒙原生/快应用路线。



- **短期（当前 IP + HTTP）**：方案 1 的改动已就位——HTTP 下桌面快捷方式依然可用（有图标、一键打开，仅带浏览器 UI）；若要全屏 App 体验，走**方案 3（Capacitor 壳）**。
- **中期（申请域名 + 备案 + HTTPS 后）**：方案 1 自动升级为完整 PWA 安装体验；如仍需发 APK，用**方案 2（Bubblewrap/PWABuilder）**半天即可完成，且一次打包长期复用。
- 不建议：自签证书路线；以及在没有 HTTPS 前提下强推 TWA。

## 后续实施清单（可选）

1. [ ] 若走域名+HTTPS：购买/注册域名 → ICP 备案 → certbot 签发 → `deploy/setup-nginx.sh --domain` 启用 HTTPS。
2. [ ] 若走 Capacitor：`frontend` 下初始化 Capacitor、放行 IP 明文流量、生成签名 keystore、构建 APK。
3. [ ] 若走 TWA：域名 HTTPS 就绪后，PWABuilder 生成 APK，部署 `/.well-known/assetlinks.json`。
4. [ ] 可选：为 PWA 补充简单 Service Worker（管理后台非必需）。
