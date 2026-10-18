# 微信小程序顾客端

原生小程序（TS + WXML + WXSS）+ esbuild 构建，独立工程目录 `frontend/apps/miniprogram/`，
**复用** Web 端的接口层与类型契约，**不复用** UI（TDesign Vue 组件在小程序不可用）。

- 状态：**第一版可用**（点餐主链路已实现），但**不能直接上线** —— 见「三、上线硬前置」。
- 设计原则：后端接口零改动，全部走现成的公开接口 `/api/customer/*`。

### 导读

| 你想做什么 | 直接看 |
|---|---|
| 改默认主页（没扫码进来时看到什么） | `src/pages/home/*` |
| 印桌牌（小程序码 / H5 二维码） | **二** |
| 上线（域名 / 备案 / 合法域名） | **三** |
| 继续开发、补功能 | **四** |
| 改代码之前，了解复用边界与工程约束 | **五、六** |
| 对照 H5 的行为差异（回归用） | **七** |

---

## 一、这一版做了什么

| 能力 | 实现位置 | 说明 |
|---|---|---|
| 默认主页 | `src/pages/home/*` | 小程序首页（`app.json` 第一项）。没扫桌台码时落地在这里：微信内直接唤起扫码进桌、上次桌台 / 进行中订单续用（**桌台详情确认存在才显示**，没有数据不出入口）、本店招牌橱窗。**扫码进桌仍然直达菜单**：后端出码的 `PagePath` 固定为 `pages/index/index`，扫码顾客不会多点一次 |
| 扫小程序码 / 普通二维码进入 | `src/pages/index/index.ts` + `src/utils/scene.ts` | 支持 `scene=tableCode=X`、裸码值、`?tableCode=X`；无参数时用本地缓存兜底 |
| 菜单浏览 | `src/pages/index/*` | 左侧分类 + 右侧菜品，右栏是**所有分类首尾相连的连续列表**（内容少的分类不会留白）；点分类平滑定位到对应分组标题，滚动时左侧高亮跟随，分组标题吸顶。菜品图/描述/价格（规格太多时显示「起」） |
| 菜品搜索 | `src/pages/index/index.ts` + `src/core/search.ts` | 菜名 / 描述 / 规格名模糊搜索：按「包含」匹配，并支持菜名的顺序包含（「红肉」→「红烧肉」）；多关键词是「与」。搜索态隐藏左栏、结果回到顶部。实现收在 `src/core/search.ts`（算法语义在单测里钉死） |
| 行内规格 / 份数 | `src/pages/index/index.ts` | 规格 ≤3 个的菜在菜品行里直接点 chip 切换（与 H5 同口径：只算**所选规格**的份数），并在行内加减；规格更多只能进弹层 |
| 本店招牌分组 | `src/pages/index/index.ts` | 菜单页的**前端合成分类**（后端只在菜品上打 `tb_dish.signature` 标记，零接口改动）：左栏第一项「本店招牌」+ 右栏置顶分组，分组内容是与首页橱窗同款的**横滑卡片条**（图带「招牌」角标、菜名、价格「起」、售罄遮罩；点卡片进规格弹层）。只取 `signature=1` 且有规格的菜、跨分类重复只收一次、**不限量**；无招牌菜时整项不出现；搜索态隐藏。卡片条收进右栏扁平列表的一行（`kind: 'sig'`，内层 wx:for 沿用默认 item 别名）；左栏角标在合成分类与真实分类**各自计一次**（与美团/饿了么同口径）。数据取自已加载的菜单，不额外请求 |
| 售罄（人工） | 后端 `tb_dish.sold_out` + 管理端「售罄」开关 + 小程序/H5 顾客端 | 与「下架」正交：下架不再下发，售罄仍下发但置灰、盖「售罄」遮罩、不可下单 |
| 售罄（自动 · 库存） | 后端 `tb_spec.stock` + 管理端规格库存输入 + 顾客端规格级/菜品级售罄 | 规格按库存计数：下单/加菜扣减、取消未支付订单回补、改单回补旧明细再扣新明细；扣到 0 该规格售罄、全部售罄则菜品售罄；`-1` 不限量。后端 `DecrementSpecStockTx` 带守卫原子扣减，并发抢光回滚整单 |
| 加载骨架屏 | `src/pages/index/*` | 按真实布局铺骨架（渐变头部 + 搜索 + 左栏 + 菜品行），而不是转圈 |
| 规格 / 单项备注 | `src/components/spec-sheet/*` | 底部弹层，备注组合规则与 H5 完全一致（`、` 连接，原样进后厨票）；带**数量步进器**（1~99，页脚价格 = 单价 × 数量），「确定」按所选份数加购 |
| 购物车 | `src/store/cart.ts` | 加/减/移除/清空、茶位费与满减试算（与 H5 同规则，仅供展示） |
| 购物车持久化 | `src/store/cartPersist.ts` | 被杀进程 / 冷启动重进后恢复未提交的车（人数与整单备注一起）。按「桌台码 + 订单号」隔离、时间上限兜底；恢复时按当前菜单复核（下架/售罄丢弃、价格取现值）。**刻意不照搬 H5 的「会话起点」判断** —— 小程序没有 sessionStorage，照抄会让冷启动永远恢复不了 |
| 下单 / 加菜 | `src/pages/cart/index.ts` | 首次下单与「加菜」自动判别；本桌已有进行中订单会提示并入 |
| 幂等提交 | `src/pages/cart/index.ts` | `clientRequestId` 复用策略与 H5 一致，超时重试不会重复下单 |
| 订单进度 | `src/pages/order/index.ts` | 3s 轮询、状态步骤条、菜品明细、手动刷新 |
| 催菜 | `src/pages/order/index.ts` | 与 H5 `useUrge` 同策略：silent 请求 + 冷却倒计时（读后端结构化 `cooldown`） |
| 改就餐人数 | `src/pages/order/index.ts` + `POST /api/customer/order/person` | 订单页「N 人用餐 · 修改」。人数决定茶位费，金额由后端重算并回传最新订单；只允许「进行中且未支付」的单。**不能复用加菜接口**：它要求明细非空，回传原明细会重复扣库存、重复出厨单 |
| 历史订单详情 / 再来一单 | `src/pages/history-detail/*` | 「我的」→ 点订单行进入。只读详情 + 「再来一单」（按当前菜单复核后并入当前购物车，赠菜不复制）。归属走会员令牌（`/customer/order/mine/:orderNo`），**不能用 tableId** —— 历史单在别的桌上 |
| 结账 | `src/pages/pay/index.ts` | 已接入小程序内支付（JSAPI，见 4.1）；未开通时降级为门店收款码 + 支付状态轮询 |
| 本桌出餐中订单提示 | `src/pages/index/index.ts` | 复用后端 `table.servingOrders`；标签**可点击**，带单号跳订单页看出餐进度（H5 只展示、点不了） |
| 订单恢复 | `src/store/session.ts` + 各页 `recover()` | 冷启动/从「最近使用」进入时，用本地缓存的 `tableCode`/`orderNo` 回到原桌原单；本桌还有出餐中订单时保留 `orderNo` 不清 |
| 底部导航（首页 / 点餐 / 订单 / 我的） | `src/components/tab-bar/*` + `src/utils/nav.ts` | 常驻显示（未下单时订单页是空态，也能切过去）；点餐页带 `append=1` 打开，否则会被「本桌已有进行中订单」弹回订单页 |
| 没带桌台码时扫码自救 | `src/pages/index/index.ts` + `src/utils/scan.ts` | 从底部导航直接切到点餐、而本次没扫过码时，错误态给的是「扫码点餐」而不是无效的「重新加载」；扫到码就地加载菜单，不用退回首页 |
| 接口层 | `src/api/http.ts` + `src/api/client.ts` | **自研 wx.request 封装**：业务码解包、中文错误文案、按路径注入顾客令牌、silent 静默、401 处置，不依赖 axios |
| 类型自包含 | `src/types.ts` | 全部实体与请求/响应类型独立维护（后端响应形态为唯一权威来源） |
| 工具自包含 | `src/shared/*` + `src/core/*` | 金额格式化、状态字典、状态判定、搜索 / 规格 / 人数口径独立实现，语义与 H5 对齐（各文件注释与单测说明） |
| 提示 / 401 可注入 | `src/shared/ui` + `api/http 的 setUnauthorizedHandler` | 逻辑层不绑定 UI 实现；401 由会员模块静默重登 |

### 目录结构

```
frontend/apps/miniprogram/
├── build.mjs              # esbuild 构建：TS → 自包含 CJS + 拷贝 wxml/wxss/json
├── project.config.json    # 微信开发者工具配置（miniprogramRoot = src/，原生编译 TS，无需预构建）
├── tsconfig.json          # wx 全局类型（类型全部自包含，无工作区路径映射）
├── eslint.config.js       # 独立配置：只有小程序全局，没有 globals.browser
├── vitest.config.ts       # 单测（Node 环境，与 Web 端 jsdom 那套互相独立）
├── test/                  # 单测脚手架：wx 替身 + App/Page 全局替身 + 页面实例构造
├── scripts/               # audit-wxml.mjs / audit-wxss.mjs / smoke.mjs
└── src/
    ├── app.ts/json/wxss   # 入口与全局样式（token 对齐 Web 端 style.css）
    ├── config.ts          # ⚠️ PROD_API_ORIGIN（唯一上线必改项）+ 按环境自动切换的 API_ORIGIN
    ├── types.ts           # 后端响应形态 + 跨页共享状态类型（唯一来源）
    ├── shared/            # keys（存储键）/ storage（wx.storage 安全封装）/ visitor / money / dicts / orderStatus / orderDisplay / ui
    ├── core/              # 纯函数口径：search（菜单搜索）/ spec（可点与售罄判定）/ person（人数边界）
    ├── api/               # http.ts（wx.request 传输层）+ client.ts（装配 + 全部端点）
    ├── store/             # cart（购物车）/ cartPersist（购物车持久化）/ session（桌台与订单）/ poller（轮询）/ member（会员）
    ├── utils/             # toast / scene（桌台码解析）/ scan（扫码取桌台码）/ nav（四页切页）
    ├── components/        # tab-bar（底部主导航）/ cart-bar（底部结算栏）/ spec-sheet（规格备注弹层）
    └── pages/             # home（默认主页）/ index（菜单）/ cart（确认下单）/ order（进度）/ pay（结账）/ profile（我的）/ history-detail（历史订单详情）
```

### 命令

```bash
cd frontend
npm run mp:verify    # typecheck + eslint + WXML/WXSS 审计 + 单测 + 构建 + 产物冒烟
npm run test -w @dining/app-miniprogram   # 只跑单测（用例数与覆盖见第六节）/ 加 :watch 进入监听
```

**日常开发不需要任何构建**：项目已是原生结构（`miniprogramRoot: src/`），微信开发者工具
「导入项目」指向 `frontend/apps/miniprogram/` 即可，工具原生编译 TypeScript（配置见 `src/tsconfig.json`）。
`dist/` 只在跑 `verify`/CI 时由 `build.mjs` 临时生成（产物冒烟用），gitignore，与开发流程无关。
测试放在 `src/` 之外的 `tests/`（小程序源码与测试分离），工具扫描 `src/` 时不会碰到 `import 'vitest'`。

---

## 二、桌牌二维码与小程序码（已实现 · 两码并存）

按「**存量二维码不动**」落地：桌台出码处新增**码类型**切换，默认仍是 H5 二维码
（旧桌牌行为零变化），选「小程序码」才走后端出码。

### 2.1 怎么用（按顺序做）

| 步骤 | 操作 | 说明 |
|---|---|---|
| 1. 填 AppSecret | 「系统配置 → 支付 → **小程序桌牌码**」填小程序 **AppSecret** | AppID 复用上方的「微信 AppID」，**必须填小程序的 AppID**（注意点见 2.2）；AppSecret 加密存储、不回显 |
| 2. 发布小程序版本 | 先发一个含 `pages/index/index` 的版本（功能不全也行） | 微信要求 `page` 必须存在于目标版本，否则一律报 `41030` |
| 3. 体验版自测 | 「小程序出码版本」选**体验版** → 出码 → 真机扫码进桌 | 体验版/开发版会自动关掉 `check_path`（页面未发布也能出码） |
| 4. 正式版出码、印桌牌 | 正式版发布后，出码版本改回**正式版**，确认能扫开再印 | 体验版码只有体验成员能扫开 —— 界面上会醒目提醒，别印错 |

> 小程序码**中心是小程序头像**：想让桌牌中间是店铺 logo，把小程序头像换成店铺 logo 即可，
> 不需要在码上做任何事（也因此「中心显示 Logo / 桌号」在小程序码模式下自动禁用）。

### 2.2 实现位置

| 端 | 内容 |
|---|---|
| 后端 | `internal/weapp/code.go`：`cgi-bin/stable_token` 取 `access_token`（进程内缓存 + 提前 5 分钟续期）→ `wxa/getwxacodeunlimit` 出码 → 按「参数指纹」落盘缓存到 `data/weapp-code/`（同一桌台只有第一次真打微信接口）；接口 `GET /api/admin/table/weapp-code?tableCode=&width=`（权限 `table:view`） |
| 配置 | `wx_app_secret`（敏感项，AES-GCM 加密）、`wx_code_env_version`（`release`/`trial`/`develop`，默认 `release`）；`migrations/full/*/seed.sql` 已同步重新生成 |
| 管理端 | `SingleQrDialog.vue` / `BatchQrDialog.vue` 加「码类型」切换；下载/打包的扩展名按真实类型给（微信回的是 JPEG）；体验版出码时提醒「只有体验成员能扫开」 |
| 小程序侧 | 无需改动：`src/utils/scene.ts` 已支持 `scene`（裸桌台码）与 `tableCode` 两种入参 |

⚠️ **AppID 共用一项的注意点**：出码复用 `wxpay_appid`（即「微信 AppID」那一格），该项必须填
**小程序**的 AppID。日后若接 Native 支付且它需要的是公众号 AppID、与小程序不同，
要把出码 AppID 拆成独立配置项（`internal/weapp` 只读一处 `cfg("wxpay_appid")`，改动很小）。

### 2.3 关键实现约束（改代码前先看）

- 微信接口**成功回图片二进制、失败回 JSON，而两者 HTTP 状态码都是 200** —— 必须按
  `Content-Type` 分支，否则会把 `{"errcode":41030}` 当成一张图存进缓存（已有回归测试）。
- `scene` ≤ **32 字符**且**不支持 `%`**（不能 urlencode）；8 位桌台码天然满足。
- `check_path` 只在 `release` 下为 `true`；体验版/开发版必须关掉，否则页面未发布时
  一律报 `41030`，自测流程走不通。
- 图片分辨率上限 **1280px**（出码宽度在 `normWidth` 收敛到 280~1280）。
- `token` 用 **stable_token** 而非老的 `cgi-bin/token`：后者「新 token 使旧 token 失效」，
  多实例部署会互相踢。

### 2.4 已考虑、暂未采用的替代方案

| 方案 | 说明 | 未采用原因 |
|---|---|---|
| B. 扫普通链接二维码打开小程序 | 在微信公众平台配置「扫普通链接二维码打开小程序」，把现有 H5 二维码的 URL 规则映射到 `pages/index/index` | 零开发量、桌牌不用重印；代价是需配置 URL 规则，且部分机型识别体验有差异 |
| URL Link / URL Scheme | 把 `https://wxaurl.cn/xxx` 这类链接编进**普通二维码**，码仍是自己画的（可嵌 Logo、任意高清打印、存量码不用换） | 链接有效期与生成额度需到官方文档核实（对桌牌是硬伤），暂不引入 |

> 无论哪个方案都**不改变已印桌牌**：方案 B 直接用旧码；本方案（A）需要重新印桌牌。
> 代码侧已同时支持 `scene` 与 `tableCode` 两种入参（见 `src/utils/scene.ts`）。

---

## 三、上线硬前置（不满足则跑不起来）

| # | 事项 | 现状 | 要做什么 |
|---|---|---|---|
| 1 | **HTTPS 域名** | 现网是纯 IP 部署（`server_name 111.230.154.50`），无域名 | 解析域名 + 申请证书。`deploy/setup-nginx.sh --domain <域名> --cert ... --key ...` 已支持一键生成 HTTPS 站点 |
| 2 | **ICP 备案** | 未知 | 境内服务器必须备案，否则小程序侧无法请求（微信强制 https + 域名） |
| 3 | **request 合法域名** | 未配置 | 微信公众平台 → 开发管理 → 服务器域名 → request 合法域名，填 `https://<域名>` |
| 4 | **downloadFile 合法域名** | 未配置 | 同上（`<image>` 加载菜品图与收款码走这条白名单） |
| 5 | **AppID** | ✅ 已填入正式 AppID（见 `project.config.json` 的 `appid`） | 换主体时同步替换即可 |
| 6 | **主体与类目资质** | 未确认 | 餐饮/点餐类目需企业主体 + 营业执照 + 食品经营许可证；个人主体做不了微信支付 |
| 7 | **用户隐私保护指引** | 未填写 | 若后续接 openid / 订阅消息 / 手机号，必须在公众平台声明采集项 |

> 不采集手机号。顾客身份走的是 `wx.login` 静默换 openid（会员一期已实现，见 4.2 节），
> 因此上线前**必须**先在公众平台补「用户隐私保护指引」声明（openid / 昵称 / 头像）——
> 也就是硬前置第 7 项，不能再缓。

### 3.1 上线前必改的代码

`frontend/apps/miniprogram/src/config.ts` 里**只有一处**：把生产域名占位符换掉。

```ts
/** 生产 / 体验环境后端地址。⚠️ 上线前必须换成真实域名。 */
const PROD_API_ORIGIN = 'https://YOUR-DOMAIN.example.com';
```

> `API_ORIGIN` 由 `PROD_API_ORIGIN` / `DEV_API_ORIGIN` + 环境档位自动派生，
> 不再需要手工切换（见下方「本地联调 vs 上线」）。

**占位符期间的行为**（备案域名还没下来的阶段）：

| 场景 | 表现 |
|---|---|
| 开发者工具 / 开发版 | **完全正常**，走 `DEV_API_ORIGIN` 连本机，不受占位符影响 |
| `mp:precheck` | 报错「还是占位符」—— 这是预期。域名到位前本来就不该提审 |
| 体验版 / 正式版 | 请求全部失败，且启动时控制台会打印 `[config] PROD_API_ORIGIN 仍是占位符…` |

> 所以占位符期间可以照常开发联调，**只是不能打体验版给门店试用**（体验版走 `PROD_API_ORIGIN`）。
> 若确需在域名下来前做真机验收，可临时把 `PROD_API_ORIGIN` 指向一个可用的测试 HTTPS 域名
> （仍需在公众平台加入合法域名白名单）。

`project.config.json` 的 `appid` 已有正式值（换主体时同步替换）。

#### 本地联调 vs 上线：域名按环境自动切换（不需要手工改）

`API_ORIGIN` 由 `wx.getAccountInfoSync().miniProgram.envVersion` 自动选择：

| 环境 | `envVersion` | 实际连的地址 |
|---|---|---|
| 开发者工具 / 真机调试 | `develop` | `DEV_API_ORIGIN`（`http://127.0.0.1:8080`） |
| 体验版 / 正式版 | `trial` / `release` | `PROD_API_ORIGIN` |

**上线前唯一要改的**是 `PROD_API_ORIGIN`（把占位符换成已备案的 HTTPS 域名），
改一次即可，此后不用再为联调来回切 —— 开发版自动连本机，体验版/正式版自动连生产。
`npm run mp:precheck` 会检查它是否还是占位符 / 是否 http / 是否裸 IP。

本地联调仍需要开发者工具配合：

| | `setting.urlCheck` | 说明 |
|---|---|---|
| **本地联调** | 关掉（「详情 → 本地设置 → 不校验合法域名、web-view、TLS 版本以及 HTTPS 证书」） | 本机 http 地址不在白名单里，工具会直接拒绝请求 |
| **体验版/上线** | 勾回校验（`true`） | 并把 `PROD_API_ORIGIN` 加进 request / downloadFile 合法域名 |

> `urlCheck` 是**本机开发设置**，由开发者工具写进 `project.private.config.json`（不入库）；
> `mp:precheck` 会在本机把它查出来提醒勾回，不会把本地的 `false` 带上生产。
> 仓库里 `project.config.json` / `project.private.config.json` 的 `urlCheck` 由开发者工具管理，
> 提交前留意别把本地的 `false` 带上生产。

> 关于 `project.config.json` 与 `project.private.config.json`：
> 微信开发者工具会在打开工程/保存设置时**整份重写**这两个文件（例如它把基础库版本写成
> `project.private.config.json` 的 `libVersion`，当前 3.17.3）。因此：
> ・`project.private.config.json` 是本机私有文件，已由 `apps/miniprogram/.gitignore` 排除；
> ・两者都列入 `frontend/.prettierignore`（工具自有格式，纳入检查会与工具互相改写）；
> ・工具会往 `project.config.json` 的 `setting` 里补一批默认项，属正常现象，不必手工回退。

#### 开发期便利：兜底桌台码与编译模式

扫码进入才是正式路径，但开发者工具默认用的是**「普通编译」**（不带任何 `scene` / query 参数），
于是启动后落在**默认主页** `pages/home/index`（不是菜单页）。想直接进菜单调试，两种办法：

| 办法 | 做法 | 注意 |
|---|---|---|
| 编译模式（贴近真实） | 点「编译」左边的模式下拉，选 `直接指定桌台码（调试用）` 或 `扫小程序码进入（scene=桌台码）`；下拉里没有就「添加编译模式」，启动页 `pages/index/index`、启动参数 `tableCode=<桌台码>` | 工具**只在打开项目时**读一次 `project.private.config.json`：手改配置后要重开项目，下拉里才会出现 |
| 开发期兜底（最省事） | 什么都不用选，直接编译，再点底部「点餐」tab | 见下方 `DEV_TABLE_CODE`：兜底只作用于**菜单页**的 onLoad；首页的「继续点餐」只认真实桌台数据（确认存在才显示），不参与兜底 |

`src/config.ts` 里的兜底值：

```ts
/** 开发期兜底桌台码（填本机库里真实存在的那个）。置空即关掉这个便利。 */
export const DEV_TABLE_CODE = '8YG3Q335';
```

`devTableCode()` 只在 `envVersion === 'develop'` 时返回它，体验版与正式版一律返回空串 ——
所以线上不会出现「没扫码也默认进某张桌」。**这个值可以一直留在仓库里，提测前不需要清空**
（`mp:precheck` 只检查这个守卫还在不在，而不是强制它为空）。

> 注意它与域名切换在「拿不到环境信息」时的行为不同：域名兜底到 develop（宁可连不上本机，
> 也不在开发时误连生产），兜底桌台码兜底到「不生效」（宁可让顾客扫码，也不把他带进别人的桌）。
> 两处失效方向相反是有意的，别合并成一个判断。
首页的「继续点餐」**不消费这个兜底**：它必须先拉到桌台详情（确认桌台还在）才显示，
没有数据时宁可不出入口，也不亮一个点了只会报错的卡片。

---

## 四、功能现状与后续计划（按优先级）

### 4.1 在线支付（✅ JSAPI 已接通）

**现状**：小程序内支付走微信 **JSAPI**，链路已完整落地：

```
购物车提交 → 订单页 → 去支付 → wx.login 拿 code
  → POST /api/customer/pay/jsapi
     （服务端 code2session 换 openid → transactions/jsapi 下单 → 商户私钥签好调起参数）
  → wx.requestPayment 拉起收银台
  → 成功后主动查单（GET /api/customer/pay/query）确认到账，才置「已支付」
```

实现位置：`pages/pay/index.ts` 的 `onPayOnline` + `api/client.ts` 的 `payJsapi`；
后端 `handler.PayJsapi` + `pay.Wxpay.CreateJsapi` + `paySign`。

**为什么付款成功后还要查单**：`wx.requestPayment` 的 success 只代表「顾客完成了付款动作」，
服务端要等微信回调才知道钱到没到。回调有延迟时直接显示「支付成功」会与订单真实状态矛盾，
所以 success 之后一律以 `queryPay` 结果为准；没确认到就留在支付页继续轮询。

**降级路径**（未开通时）与 H5 一致：展示门店收款码 + 引导到前台结账、每 5s 轮询支付状态。

**开关与前提：**

| 项 | 说明 |
|---|---|
| 顾客端入口 | 由公开配置 `wxpay_jsapi_enabled` 决定（管理端「系统配置 → 在线支付 → 小程序内支付」，默认关）。fail-closed：未开通时按钮根本不存在，不会出现「点了拉不起收银台」 |
| AppID | `wxpay_appid` 必须是**小程序**的 AppID（与桌牌码共用同一项，注意点见 2.2）。填成公众号 AppID 时 Native 仍能跑、JSAPI 必失败 |
| AppSecret | 必须配置 `wx_app_secret`（code2session 用）。缺它时接口返回「小程序内支付尚未开通，请用收款码或到前台结账」 |
| 回调 | 复用现有 `/api/customer/pay/notify/wxpay`，无需额外配置 |
| openid | 用 `wx.login` 的 code 现换、不落库 —— 未登录顾客同样能付款（会员表里那份 openid 属于登录链路，不复用） |

同一订单只会保留一张能付款的支付单：JSAPI 下单前会先关掉该单其它待支付流水，
避免顾客手里同时存在两张都能付成功的码。

#### 接口契约（后端按这个实现，前端无需再改）

```
POST /api/customer/pay/jsapi
请求体：{ "orderNo": "D2026...", "code": "<wx.login 的 code>" }
响应体：{ "code": 200, "data": {
           "timeStamp": "1700000000", "nonceStr": "…",
           "package": "prepay_id=wx…", "signType": "RSA", "paySign": "…" } }
```

- 失败时返回业务错误码 + 中文 `msg`，前端据此提示「小程序内支付暂不可用，请用上方收款码或到前台结账」
  （不会重试、不会卡在支付页）。
- 是否展示「微信支付」按钮，由公开配置 `wxpay_jsapi_enabled` 决定（`'1'` 才显示）；
  `CustomerSettings()` 已下发该项，管理端配置里也有对应开关（默认关）。
- `signType` 取值限定 `MD5` / `HMAC-SHA256` / `RSA`（`PayJsapiResult` 已按 `wx.requestPayment` 收窄）。

⚠️ 关键约束：`openid` 是**同一个微信号在小程序下的** openid，与公众号下的不同 ——
所以「换 openid 用的 AppID」（code2session）与「下单用的 AppID」（`wxpay_appid`）必须是同一个，
且都是小程序 AppID，否则微信会拒付。

### 4.2 顾客身份（openid / 我的订单）

- **现状**：✅ **一期已实现**（2026-10-08，按推荐方案落地）——
  微信一键授权静默注册（免表单/免密码/无弹窗）、`tb_customer` 会员档案、
  `tb_order.customer_id` 归属（0=匿名单，历史单不回填）、顾客令牌（2h，过期静默重登）、
  「我的」tab（会员卡 + 跨桌历史订单 + 退出登录）。
  完整设计与实现约束见 [`docs/design-docs/customer-member/spec.md`](design-docs/customer-member/spec.md)。
- **身份层（2026-10-18）**：账号与登录方式已拆分到 `tb_customer` + `tb_customer_identity`
  （`provider` + `external_id`），本期仍只实现 `wx_mp`；App / 网站后续接同一套接口，
  只需新增一个凭据校验器，令牌与订单归属不动。
  见 [`docs/design-docs/customer-member-identity/spec.md`](design-docs/customer-member-identity/spec.md)。
- **上线动作**：公众平台「用户隐私保护指引」补声明（openid / 昵称 / 头像）—— 即硬前置第 7 项；
  后端需配置小程序 AppID + AppSecret（与桌牌码共用）。
- **已实现（2026-10-19）**：手机号绑定（`getPhoneNumber`，企业主体 + 按次收费）、
  储值（含余额支付，本金 / 赠金分桶）、注销会员入口。
  储值设计与合规约束见 [`design-docs/customer-wallet/spec.md`](design-docs/customer-wallet/spec.md)
  与 [`compliance/prepaid-card.md`](compliance/prepaid-card.md)。
- **二期候选**：积分、优惠券、会员等级、H5 端会员（无静默 openid 路径，不做）。

### 4.3 订阅消息（出餐通知）

- **现状**：未接，顾客只能停留在订单页看轮询。
- **要做的**：公众平台申请模板（餐饮类目可选「取餐提醒」「订单状态变更」）→ 前端在提交订单成功后
  调 `wx.requestSubscribeMessage` → 后端在订单状态流转时调订阅消息发送接口。
  涉及 `backend/internal/handler/order.go`（状态流转处）与新增 `internal/wx/subscribe.go`。

### 4.4 其他未做的小功能

- 未做清台提示、用餐评价、开发票等入口；
- 未做断网重试队列（下单失败只提示，需手动重试）；
- 订单页与支付页的加载态仍是「转圈 + 文案」，没像菜单页那样铺骨架屏。

---

## 五、复用边界与已知取舍

### 5.1 与 Web 端的实现关系：同规则、各自实现

小程序端**不引用**工作区包（`@dining/{api,types,shared,customer-core}`）：类型、接口层、
工具与纯函数口径全部自包含在 `src/`（`types.ts` / `shared/` / `core/` / `api/`）。
原 `packages/customer-core` 的 `useCart` / `useOrderPolling` / `useUrge` 建立在 **Vue 响应式**
之上，原生小程序没有 Vue 运行时，无法复用 —— 小程序侧另写了等价实现
（`src/store/cart.ts`、`src/store/poller.ts`、`pages/order` 里的催菜）；
原共享层的纯函数（金额格式化、状态字典、搜索、规格与人数口径）也已收进
`src/shared/` 与 `src/core/` 独立维护。

**代价（要记住的一点）**：计价规则（茶位费 / 满减 / 合计口径）、催菜冷却策略、
搜索算法现在与 H5 是**两处实现**，改动时两端必须同步（各端都有单测钉住语义，
漂移会在测试里暴露）。要消除这份重复，只有两条路：

1. 改用 **uni-app**（Vue 3 + Vite）：`customer-core` 可原样复用，但需要引入 `@dcloudio/*` 工具链，
   且会与 Web 端共用同一套依赖树（有把已验证的 Web 构建一起搞崩的风险）；
2. 把 `customer-core` 重写成**不依赖 Vue**（换成普通回调 + 可注入的调度器），两端都复用它。

当前选了「原生 + 独立一份薄逻辑」，理由是：小程序端获得完全的独立性与零工具链风险，
可完全本地验证（见第六节）。

### 5.2 有意不复用的部分

- `@dining/auth`（管理端登录态与权限缓存）：小程序顾客端是公开访问，不需要；
- `apps/admin/src/utils/tableQr.ts`（浏览器端二维码生成）：小程序侧不在本地生成二维码；
- `frontend/src/views/PrintTicket.vue`（打印票据页）：属于门店侧，不在顾客端范围;
- `useScrollSpy`（H5 的双栏滚动联动）：没有直接复用（H5 版依赖 `IntersectionObserver` 与 Vue 响应式），
  小程序用 `scroll-view@bindscroll` + `createSelectorQuery` 量分组标题偏移来自实现，交互效果对齐（见第七节）。

### 5.3 工程上的简化（不是缺陷，但要知道）

- **`dist/` 仅为验证产物**：`verify`/CI 里的构建冒烟仍走 esbuild 打出每页自包含的 CJS
  （9 个产物 JS 合计约 105KB，整个 `dist/` 约 160KB），用于在无 DOM 宿主里执行产物、
  抓浏览器 API 泄漏。日常开发不产生也不需要它（开发者工具直接编译 `src/`）。
- **跨页状态挂在 App 实例上**：`getApp<T>().session / .cart`。原生结构下模块文件本来
  全局共享（这一约束源自旧的「每页独立打包」方式），但 App 实例约定继续保留 ——
  它不依赖模块加载顺序，页面代码也更直白。修改 `src/store/*` 时务必保持这个约定
  （详见 `src/types.ts` 顶部注释）。
- **WXML 表达式不支持函数调用**：所有要显示的值都在 JS 里预计算成扁平字段
  （如 `specChips[].on`、`dishes[].priceText`）。新增模板逻辑时不要写 `array.indexOf(...)` 之类。
- **CSS 版本基线是 iOS 14.5+**：布局大量使用 flex `gap` 与 `rgb(r g b / a%)` 颜色写法；
  刻意没用的有 `inset` 简写（遮罩里写成四个方向的显式声明）。要下探到更老机型的话，
  把这些换成 margin 与 `rgba()` 即可，改动集中在 `.wxss`。
- **安全区不做成工具类**：`padding-bottom` 与元素自身 padding 必须在同一条声明里合并，
  否则「页面样式覆盖全局样式」会把它顶掉（见 `src/app.wxss` 末尾注释）。
- **自定义组件样式自包含（重要纪律）**：`components/*` 用默认的 `isolated`，
  **组件里不能用 app.wxss 的类与变量**（写了不生效，且不报错）。所以三个组件
  （`cart-bar` / `spec-sheet` / `tab-bar`）各自带一份用到的 token 与按钮/¥ 号样式副本；
  组件内要省略号之类的工具样式也得自带。
  这条纪律由 `scripts/audit-wxss.mjs` 按作用域强制 —— 它就是在做这轮复查时抓出了
  `spec-sheet` 误用全局 `.ellipsis` 的真实 bug。
- **跨页状态只能挂 App 实例**：见 `src/types.ts` 顶部注释（CJS 分包导致模块级单例不共享）。

---

## 六、质量现状与验证方式

已接入的六道门（`npm run mp:verify` 全跑一遍，任一门失败即中断）：

| 门 | 做什么 | 能拦住什么 |
|---|---|---|
| `tsc --noEmit` | 用 `miniprogram-api-typings` 的 `wx` 类型对全量源码（含单测）做类型检查 | 类型/接口字段写错、端点与 `src/types.ts` 定义不同步 |
| `eslint .`（独立配置） | 只有小程序全局（`wx`/`App`/`Page`/`Component`），**没有** `globals.browser` | 误用浏览器 API（如 `document`、`localStorage`）写进小程序代码 |
| `scripts/audit-wxml.mjs` | 静态交叉核对每个 `.wxml` 的 `{{字段}}`、`bind*/catch*`、传给子组件的 kebab-case 属性，是否都在同名 `.ts` 的 `data`/`properties`/方法里 | **模板是字符串里的弱类型绑定**：字段名/方法名/属性名拼错时 tsc 与 eslint 都不会报，只在真机上渲染成空白 |
| `scripts/audit-wxss.mjs` | 用 postcss 解析全部 `.wxss`，检查 `var(--x)` 有声明，并**按作用域**校验模板用到的每个 class 都有定义（页面 = 自身 + app.wxss；组件 = 仅自身，因为组件默认样式隔离） | 样式语法错误、token 名拼错、类名拼错、**组件误用全局类**（这类只表现为「样式没生效」，肉眼极难发现） |
| `vitest run` | 覆盖范围见下方「单测覆盖了什么」（文件数与用例数以运行输出为准） | 适配器报文与状态码判定、计价口径与 H5 漂移、桌台码解析、主页面切页策略与底部导航、菜单搜索与行内规格、加菜跳转、下单/加菜/支付的请求与失败分支 |
| `scripts/smoke.mjs` | 主动删掉宿主的 `window/document/fetch/localStorage`，用最小 `wx` 桩把全部产物执行一遍 | 运行期的 DOM 依赖（比类型检查更硬的保证）、`Page/Component/App` 注册缺失 |

> **这些计数会随代码变动而漂移，以 `npm run mp:verify` 的实际输出为准**：审计脚本每轮会打印
> 「N 个模板 / 校验 N 处字段引用、N 处事件绑定」与「class 定义 N 个」，vitest 打印 `Test Files` / `Tests`。
> 写这份文档时的参考值：**13 个用例文件 / 104 个用例全绿**；**90 处字段引用、48 处事件绑定、203 个 class 全部对得上**。
>
> 同理，任何出现在本文档里的计数（用例数、class 数、产物体积）都只是「写文档那一刻」的快照。

### 单测覆盖了什么

跑在 Node 环境（不是 jsdom）—— 这本身就是一道约束：**被测代码只要碰 DOM 就会当场失败**。
脚手架在 `test/`：`mock-wx.ts`（web 请求队列 / toast / 导航 / 内存 storage / 可编辑 `showModal` 的 wx 替身）、
`harness.ts`（`App` / `Page` / `Component` / `getApp` / `getCurrentPages` 全局替身，造带 `setData` 的页面或
组件实例 —— 组件会摊平 `methods`、并入传入的 properties，`observers` 用 `fireObserver()` 手动触发；
另有页面栈替身 `setCurrentPages()` 与 `createSelectorQuery` 链式桩）。

| 用例文件 | 覆盖点 |
|---|---|
| `src/store/__tests__/cart.test.ts`（14） | 跨页共享（状态在 App 实例上）、同规格合并/异规格分行、减到 0 移除、备注覆盖、浮点收敛、茶位费/满减倍数/上限/合计口径 |
| `src/pages/cart/__tests__/submit.test.ts`（7） | 空车拦截、首次下单与加菜判别（含人数沿用订单值）、幂等键在同一次重试中复用、业务错误时「不跳转 + 不丢购物车 + 保留幂等键」、提交中重复点击不发第二次请求、人数联动合计、移除需确认 |
| `src/pages/cart/__tests__/remark.test.ts`（5） | 就地改单项备注：只改文字不加份数；老基础库拿不到输入内容时不清掉已有备注；下单成功但详情回拉失败时带 `orderNo` 跳订单页兜底 |
| `src/pages/index/__tests__/entry.test.ts`（5） | 首屏失败后「重新加载」用的是本次解析出的桌台码；`append=1` 时留在菜单页；平时进入遇进行中订单直接跳订单页；快速「+」不抹掉已填备注；坏图重建后不闪回破图 |
| `src/pages/index/__tests__/menu-interact.test.ts`（11） | 搜索：按菜名/描述过滤、顺序包含（红肉→红烧肉）、搜不到给空态、清空恢复。行内规格：按数量分档（≤3 出行内 chip、更多进弹层、无规格标不可点）、切规格后价格/选中态/份数跟着走、「+」加的是**所选**规格、规格太多只开弹层。售罄：仍展示但不可下单（orderable 与 soldOut 分离）、「+」/点菜名都提示已售罄 |
| `src/pages/order/__tests__/append.test.ts`（6） | 加菜跳转（页面栈里没有菜单页时不能死循环）、首屏加载失败的重试、无订单时进入空态并在拉到订单后退出 |
| `src/pages/pay/__tests__/paid.test.ts`（2） | 支付成功先落「已付款」再回拉详情；延时跳转期间顾客手动返回不重复导航 |
| `src/components/spec-sheet/__tests__/confirm.test.ts`（3） | 弹层「确定」的防重入：连点只加一份 |
| `src/pages/home/__tests__/home.test.ts`（9） | 默认主页：扫码进菜单（用 navigateTo，首页留在栈里）、扫到无关内容提示重扫 / 取消不提示、上次桌台**确认存在才显示**续用入口（拉不到桌台详情就收起）、进行中订单卡片（已完成的单不进）、招牌菜只取 `signature=1` 且多规格给最低价 |
| `src/pages/index/__tests__/signature.test.ts`（9） | 菜单页「本店招牌」合成分类：左栏第一项 + 右栏分组标题 + 横滑卡片条（初始高亮在它上）；卡片只取 `signature=1` 且跳过无规格菜、跨分类重复只收一次、多规格给最低价 +「起」、售罄置灰；角标合成分类与真实分类各计一次；点卡片开规格弹层不跳转、售罄只提示不进车；卡片坏图只回写那一张；搜索态整组消失；无招牌菜整项不出现 |
| `src/utils/__tests__/scene.test.ts`（21） | 小程序码 scene / 裸码值 / urlencode / 多参数 / 非法编码 / 空值；小程序内扫码结果解析（`/order/<码>`、`?tableCode=`、小程序码 path、无关链接返回空串） |
| `src/api/__tests__/http.test.ts`（8） | 接口传输层：业务码解包、业务失败转 ApiError（保留响应体供催菜冷却读取）、silent 不弹全局提示、HTTP 错误文案优先级、网络失败与超时、401 触发处理器、/customer/ 路径自动带顾客令牌、GET 查询参数拼 URL |
| `src/core/__tests__/search.test.ts`（11） | 菜单模糊搜索：关键词拆分（空白/中英文逗号）、菜名与描述与规格名命中、顺序包含、多关键词「与」、空词短路、过滤时不改写原菜单 |
| `src/utils/__tests__/nav.test.ts`（7） | 底部导航切页：已在目标页不跳、目标页在栈里就 `navigateBack`（含跨页 delta）、不在栈里 `redirectTo`（点餐页必须带 `append=1`，否则被弹回订单页） |
| `src/utils/__tests__/toast.test.ts`（3） | 只有带图标的提示才截断到 7 字，纯文字提示完整保留 |

### 还没做的测试

- **两个纯展示组件没有用例**：`components/tab-bar`、`components/cart-bar`（切页策略已单测，但它抽在
  `utils/nav.ts` 里）；`pages/index` 的滚动联动（scroll-spy）与 `pages/order` 的轮询/催菜目前只有静态审计 +
  冒烟覆盖 —— 它们都依赖节点测量与定时器，属于「真机上才看得准」的部分。
- **下架态仍不实现（也不该实现）**：顾客菜单接口（`dao.ListEnabledDishes`）只查 `status=1`，下架菜
  根本不会下发到顾客端，界面无处可渲染。顾客端唯一「看不到价格」的菜是**没有规格**的历史数据，
  按「暂不可点」处理。
- **售罄有两层**：人工售罄 `tb_dish.sold_out`（管理端开关，无条件置灰），自动售罄 `tb_spec.stock`
  （按规格计数，卖完自动置灰）。库存流转：下单/加菜扣减、取消未支付订单回补、改单回补旧明细再扣新明细；
  退款**不回补**（已支付订单的餐已做）。规格编辑为**增量更新**（按 `specId`），改价格/名字不会把库存清零。
- **没有包体检查**：`lazyCodeLoading` 已开，分包策略未做（当前体量不需要）。

### 真机回归清单（上线前逐条过一遍，过一项勾一项）

静态检查与单测都跑在 Node 里，下面这些**只能在微信开发者工具 + 真机上验**：

- [ ] **默认主页**：从「最近使用」/ 直接打开小程序，落在 `pages/home/index`（不是菜单页），店名与招牌菜正常
- [ ] **首页扫码**：点首页「扫码点餐」→ 微信相机 → 扫 H5 桌牌码 / 小程序码，能落到对应桌台；取消或扫到无关码不报错、不跳转
- [ ] **首页续用**：「继续点餐」带上次的桌台进菜单；本桌有进行中订单时仍会先问一句归属
- [ ] **扫码进桌**：用真实桌台码生成小程序码，微信扫码落到对应桌台，顶部桌台名正确（**不经过首页**）
- [ ] **菜单页招牌分组**：左栏第一项「本店招牌」+ 右栏置顶横滑卡片条（图带「招牌」角标）；点卡片开规格弹层可直接加菜；两侧角标都亮；售罄招牌卡置灰、点击只提示；搜索时分组隐藏、清空搜索词后回到顶部可见；无招牌菜的店左栏不出现该入口
- [ ] **加菜闭环**：本桌有进行中订单时扫码 → 进订单页 → 点「加菜」→ **停在菜单页**并出现加菜提示条 →
      点菜 → 购物车显示「提交加菜」→ 提交后订单明细出现「加菜1」批次标签
- [ ] **下单与打印**：下单后后厨小票正常出单；加菜只打印新增的菜（不重打整单）
- [ ] **轮询恢复**：切后台 30s 再回来，订单状态与金额是新的；支付页停留时轮询继续
- [ ] **催菜冷却**：连点两次，第二次提示「已催过啦，请等待 N 秒」，倒计时能正常归零
- [ ] **支付降级**：收款码能长按保存 / 用「扫一扫 → 相册」识别；前台收完款顾客端能翻成「已支付」
- [ ] **iOS 安全区**：底部操作栏（购物车、订单页按钮）在带 Home 指示条的机型上不被遮住
- [ ] **scroll-view 手感**：左栏点分类定位准确、滚动时高亮跟随不抖；菜品图懒加载不白屏
- [ ] **弱网**：断网后菜单页显示错误态且「重新加载」能自愈；下单失败提示完整原因且不丢购物车
- [ ] **包体**：「详情 → 基本信息」里主包 < 2MB（`lazyCodeLoading` 已开）

---

## 七、与 H5 的行为差异（回归对照用）

| 场景 | H5 (`apps/customer`) | 小程序 |
|---|---|---|
| 分类切换 | 双栏 + 滚动联动高亮（scroll-spy） | 同左（滚动联动 + 分组标题吸顶），点分类用 `scroll-into-view` 定位；左栏高亮项会自动滚回可视区 |
| 规格 / 备注 | 规格 chip 全量在菜品行内，备注是独立弹层 | 规格 **≤3 个**时才在行内点选，更多进弹层；规格与备注合并在同一个弹层 |
| 菜单内加减份数 | 行内可增减 | 规格 ≤3 个的菜行内即有步进器（同样只算所选规格）；规格更多的菜只能「+」进弹层，增减在购物车页 |
| 菜品搜索 | 有：全屏搜索层（与小程序**共用** `customer-core/search`） | 有：菜名 / 描述 / 规格名模糊搜索 + 顺序包含，搜索态隐藏左栏 |
| 售罄 | 显示「售罄」、不可下单 | 同左（遮罩 + 置灰 + 不可点），口径与后端一致 |
| 在线支付 | 二维码（Native 扫码支付） | 小程序内调起微信支付（JSAPI，见 4.1）；未开通时才是收款码 + 前台结账 |
| 购物车持久化 | localStorage，仅**刷新**可恢复（关掉重开即丢） | 本地存储，**冷启动也恢复**；按桌台码 + 订单号隔离 |
| 改就餐人数 | 仅购物车抽屉 | 购物车页 + 订单页（仅「进行中且未支付」） |
| 历史订单 | 无（无会员体系） | 「我的」页列表 + 只读详情 + 再来一单 |
| 页面可见性 | `visibilitychange` 暂停轮询 | 页面 `onHide` / `onShow`（小程序生命周期天然覆盖切后台） |
| 未登录态 | 管理端 401 跳登录页 | 顾客端无鉴权，401 仅提示 |
| 订单恢复 | 依赖 `table.currentOrder` | 同左，另加本地缓存 `tableCode/orderNo` 兜底 |
| 没带桌台码 | 落到点餐页，提示扫码（H5 本身就在浏览器里，通常由二维码带着码进） | 落到**默认主页**：微信内扫码进桌 + 上次桌台/进行中订单续用 + 招牌菜；从点餐 tab 误入菜单时，错误态也能就地扫码 |
| 出餐中订单 | 顶部提示条只展示，点不动 | 提示条可点击，带 `orderNo` 进订单页看出餐进度 |
| 订单空态 | 插画（菜单卡 + 筷子 + 冒热气的茶杯）浮动 + 星点闪烁 + 热气升腾，文案「上桌啦，点起来吧」+ 去点餐 | 与 H5 一致：同一段 SVG 的 CSS 复刻（`pages/order/index.wxss`），动画节奏相同；WXSS 无 `prefers-reduced-motion`，不做降级 |

---

## 八、下一步建议顺序

1. **先确认三、里的第 1–6 项能否落地**（域名备案、资质、AppID）。落地不了，后面的投入都白费。
2. 配置 `src/config.ts` 的 `PROD_API_ORIGIN` + 检查 `project.config.json` 的 `appid`，用开发者工具跑通
   「扫码进桌 → 下单 → 看进度 → 催菜」全链路（本地联调时域名会自动指向本机，后端一行都不用改）。
3. 桌牌码试跑：先发一个含 `pages/index/index` 的版本（体验版即可），按 2.1 出**体验版**小程序码
   自测「扫码进桌」，确认体验后再发正式版、出正式版码。
4. 视数据决定是否投入 4.1（JSAPI 支付）与 4.3（订阅消息）。
5. 上线前补第六节里提到的单测与真机回归。

