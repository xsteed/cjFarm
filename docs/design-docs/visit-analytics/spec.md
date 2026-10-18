# Feature：站点访问统计与用户量（PV / UV / 新访客 / 来源渠道）

**作者**：CodeBuddy（设计起草）
**日期**：2026-10-17
**状态**：待评审
**关联代码**：`backend/internal/store/{schema.go,dialect.go}`、`backend/internal/store/dao/visit*.go`（新增）、`backend/internal/service/visit_service.go`（新增）、`backend/internal/handler/visit.go`（新增）、`backend/main.go`、`backend/internal/conf/conf.go`、`frontend/packages/{shared,api,types}`、`frontend/apps/{customer,miniprogram,admin}`、`backend/migrations/*`

---

## 0. 需求与一句话方案

**需求**：店家想在后台看到「站点有多少人访问、用户量有多大」——扫码 H5、小程序、管理端三个入口的访问量（PV）、独立访客（UV）、新增访客、按天/按渠道的趋势与构成。

**一句话方案**：**前端轻量打点 + 后端批量落库 + 日聚合台账 + 管理端报表接口与图表**，完全复用现有 DDD 分层与两库方言基建；访客标识用「前端生成的随机访客 ID」为主、「IP+UA 哈希」兜底，会员按 `customer_id` 精确去重；明细表短期保留、日聚合表长期保留，与「打印任务/打印日志」的分工同构。

**一期做什么**：PV/UV/新访客/累计访客/会员数；按日趋势 + 渠道构成 + **扫码入口（桌台码）Top 榜**；Dashboard「今日访问」卡片 + 报表页访问趋势图。

**一期不做什么**：热力图、停留时长、跳出率、漏斗、实时在线人数、入口→下单转化率（列入二期）。

---

## 1. 指标口径定义（先对齐定义，再谈实现）

| 指标 | 定义 | 数据来源 | 精度说明 |
|---|---|---|---|
| PV 页面访问 | 一次「进入点餐页/启动小程序/登录后台」记 1 次 | `tb_visit_log` 行数 | 精确 |
| UV 独立访客 | 同一访客在同一周期内只计 1 次，按访客键去重 | `tb_visit_log` 按 `visitor_key` `COUNT(DISTINCT)` | 匿名为近似值，会员精确 |
| 新访客 | 当日 UV 中，历史上首次出现的访客 | `tb_visitor.first_visit_date = 当日` | 精确（访客注册表） |
| 累计访客 | 建站以来出现过的独立访客总数 | `tb_visitor` 行数 | 同上 |
| 会员数 | `tb_customer` 中 `status=1` 的档案数 | 现有表，零成本 | 精确（这是「用户量」的硬指标） |
| 会员 UV | 周期内有过访问的登录会员数 | `tb_visit_log` 中 `m:` 前缀访客键 | 精确 |
| 渠道 | `h5`（扫码点餐页）/ `miniprogram`（小程序启动）/ `admin`（后台登录） | 打点时显式传入 | 精确 |

**访客键（UV 去重键）优先级**：

1. `m:<customer_id>` —— 小程序登录会员，最可靠；
2. `c:<visitorId>` —— 前端生成并持久化的随机 ID（localStorage / wx.storage），同设备再次访问可识别；
3. `a:<ipHash>:<uaHash>` —— 前两者都缺失时的兜底（如禁用了存储）。

> **口径边界**：匿名访客换手机、换浏览器、移动网络换 IP 都会被算作新访客，UV 是**近似值**，只用于看趋势与量级；「用户量」的精确口径以**会员数**为准。报表页需明示此口径（与营业额口径提示同级的文案）。

**时间口径**：全部按服务器本地时间的自然日（`YYYY-MM-DD`），与现有报表一致（`report_service.go` 的 `dayStart`/`dayLayout`）；区间左闭右开。小程序 PV 口径对齐微信官方「启动次数」——每次冷启动记 1 次（`onLaunch`），前后台切换不重复计。

---

## 2. 总体架构与数据流

```
┌─────────────┐   POST /api/customer/visit    ┌──────────────────────────────┐
│ H5 点餐页    │ ───────────────────────────▶ │ handler.VisitTrack（公开组）  │
│ 小程序 app   │ ───────────────────────────▶ │  · 限流（每 IP 60/min）       │
└─────────────┘                              │  · 参数清洗（channel 白名单） │
                                             │  · 算访客键，投递到 channel   │
┌─────────────┐   AdminLogin 成功埋点         └──────────────┬───────────────┘
│ 管理端登录   │ ────────────────────────────────────────────┤（同一 channel）
└─────────────┘                                             ▼
                                     ┌───────────────────────────────────┐
                                     │ 批量写 worker（service 包）        │
                                     │  · 攒批 200 条 / 2s  flush         │
                                     │  · 一个事务：明细 INSERT +        │
                                     │    访客注册表 upsert              │
                                     │  · 缓冲满丢弃 + warn（fail-open）  │
                                     └───────────────┬───────────────────┘
                                                     ▼
                        ┌──────────────────────────────────────────────┐
                        │ tb_visit_log（明细，保留 N 天，默认 90）       │
                        │ tb_visitor（访客注册表，长期保留，量极小）      │
                        └───────────────────┬──────────────────────────┘
                                             │ 每日 rollup（后台任务 + 启动补跑）
                                             ▼
                        ┌──────────────────────────────────────────────┐
                        │ tb_visit_daily（日聚合台账，长期保留）         │
                        └───────────────────┬──────────────────────────┘
                                             ▼
                        GET /api/admin/report/visits（report:view）
                                             ▼
                        报表页 VisitChart + Dashboard 今日概览
```

**设计原则**（与仓库既有哲学一致）：

- **fail-open**：统计写失败只告警，绝不影响点餐/登录（同审计中间件，见 `audit.go:23-35` 的注释）。
- **批量单写者**：所有访问明细走一个 channel + 一个 worker，SQLite 单写者模式下不放大锁竞争；MySQL  likewise 合并写入。
- **明细可清、台账长存**：与 `tb_print_job`（待办，7 天清理）/ `tb_print_log`（台账，长期保留）完全同构（见 `migrations/README.md` 的表清单说明）。

---

## 3. 数据模型

三张新表，风格对齐 `schema.go` 的 `{{PK}}/{{OPTS}}` 模板占位符。

### 3.1 `tb_visit_log` —— 访问明细（待办性质，定期清理）

```sql
CREATE TABLE IF NOT EXISTS tb_visit_log (
	visit_id     {{PK}},
	visit_date   VARCHAR(10)  NOT NULL,          -- 'YYYY-MM-DD'，冗余一列供日聚合与清理直接按天切
	channel      VARCHAR(16)  NOT NULL,          -- h5 / miniprogram / admin
	visitor_key  VARCHAR(80)  NOT NULL,          -- 访客去重键（§1 的三种前缀之一）
	is_member    INTEGER      DEFAULT 0,         -- 1 = 登录会员访客（冗余，免得到账时解析前缀）
	customer_id  INTEGER      DEFAULT 0,         -- 会员访客的 customer_id，匿名为 0
	entry        VARCHAR(16)  DEFAULT '',        -- 入口标识：桌台点餐码（h5）/ 场景值（小程序，一期留空）
	ip_hash      VARCHAR(64)  DEFAULT '',        -- sha256(ip+pepper) 前 32 位，不存明文 IP
	ua_hash      VARCHAR(64)  DEFAULT '',        -- sha256(ua+pepper) 前 32 位
	create_time  {{TSTYPE}} DEFAULT {{NOWDEF}}   -- 与全库一致的时间列约定
){{OPTS}};

-- 日聚合：按天扫明细
CREATE INDEX IF NOT EXISTS idx_visitlog_date   ON tb_visit_log(visit_date);
-- UV 去重：按天 + 访客键（聚合查询的主路径）
CREATE INDEX IF NOT EXISTS idx_visitlog_visitor ON tb_visit_log(visit_date, visitor_key);
-- 渠道 UV（窗口内近似）：按天 + 渠道 + 访客键
CREATE INDEX IF NOT EXISTS idx_visitlog_channel ON tb_visit_log(visit_date, channel, visitor_key);
```

- **`visit_date` 冗余**：清理与 rollup 都按自然日整体操作，冗余一列避免对 `create_time` 做函数索引（两库都不支持）。
- **IP/UA 只存哈希**：pepper 取自主密钥文件（`data/master.key`，见 `InitSecretKeys`），库文件泄露也无法反推 IP。
- **无 `update_time`**：明细只插不改（同 `tb_oper_log`，只插不更新）。
- **无种子数据** → `SeedCounts()` / `sqlgen_test.go` 均无需登记（同 `tb_customer` 的先例，见 customer-member spec §2.1）。

### 3.2 `tb_visitor` —— 访客注册表（台账，长期保留）

```sql
CREATE TABLE IF NOT EXISTS tb_visitor (
	visitor_key      VARCHAR(80) NOT NULL,       -- 主键即访客键（不用自增 ID：天然去重）
	first_visit_date VARCHAR(10) NOT NULL,
	last_visit_date  VARCHAR(10) NOT NULL,
	visit_count      INTEGER     DEFAULT 1,
	is_member        INTEGER     DEFAULT 0,
	customer_id      INTEGER     DEFAULT 0,
	create_time      {{TSTYPE}} DEFAULT {{NOWDEF}},
	PRIMARY KEY (visitor_key)
){{OPTS}};

-- 新访客统计：按首访日期圈选
CREATE INDEX IF NOT EXISTS idx_visitor_first ON tb_visitor(first_visit_date);
```

- 一行一个独立访客，**量级 = 累计独立访客数**（单店预估数百到数千行），永久保留无压力。
- 「新访客」精确化就靠它：`first_visit_date = 当日` 即当日新增。
- 主键直接是 `visitor_key`：upsert 时命中主键即「老客回访」，未命中即「新客建档」，与 `NextPickupNoTx` 的「先 UPDATE 后 INSERT」模式配合（见 `dao/order.go:798-803`：本仓库不依赖 `ON DUPLICATE KEY`/`ON CONFLICT`，两库语义靠同一段代码保证）。

### 3.3 `tb_visit_daily` —— 日聚合台账（长期保留）

```sql
CREATE TABLE IF NOT EXISTS tb_visit_daily (
	id         {{PK}},
	visit_date VARCHAR(10) NOT NULL,
	channel    VARCHAR(16) NOT NULL,             -- 'all' = 全站合计行；其余为单渠道行
	pv         INTEGER     DEFAULT 0,
	uv         INTEGER     DEFAULT 0,
	new_uv     INTEGER     DEFAULT 0,
	member_uv  INTEGER     DEFAULT 0
){{OPTS}};

-- rollup「先 UPDATE 后 INSERT」的定位键，同时兜底并发重复行
-- （唯一性由索引保证，不在建表语句里再写 UNIQUE，避免同义索引建两份）
CREATE UNIQUE INDEX IF NOT EXISTS idx_visit_daily_uk ON tb_visit_daily(visit_date, channel);
```

- `channel='all'` 行的 `uv` 是**跨渠道去重后**的全站 UV（rollup 时从明细一次算出），各渠道行之和会 ≥ 全站 UV（同一访客可能两个渠道都来过）——报表展示时注明。
- rollup 采用**绝对重算**（不是累加）：同一 (date, channel) 重复执行结果一致，崩溃后可安全重跑。

### 3.4 保留策略

| 表 | 角色 | 保留 | 配置 |
|---|---|---|---|
| `tb_visit_log` | 待办 | 默认 90 天（0 = 永不清理） | `VISIT_RETENTION_DAYS` |
| `tb_visitor` | 台账 | 永久 | — |
| `tb_visit_daily` | 台账 | 永久 | — |

清理时机：每日 rollup 之后顺带执行（该日已落台账，明细才可弃），与 `CleanExpiredOperLogs` 同模式（`service/audit_service.go`）。

---

## 4. 采集点设计（三个入口）

### 4.1 H5 顾客端（主入口，占绝大部分流量）

- **位置**：`frontend/apps/customer/src/OrderPage.vue` 的 `onMounted`。
- **动作**：生成/读取 `visitorId`（`packages/shared` 新增 `visitorId()` 工具，localStorage 键加入现有存储键常量），`POST /api/customer/visit`，body：

```json
{ "visitorId": "3f8a...", "channel": "h5", "entry": "K7M3PQ9X" }
```

  `entry` 取路由参数里的桌台点餐码（`/order/:tableId`），让「哪张桌的码被扫得最多」有数据可查（一期落库不展示）。
- **不阻塞渲染**：fire-and-forget（`.catch(() => {})`），统计失败对用户完全无感。
- **`PrintTicket.vue` 不埋**：票据页是下单后的衍生页，计 PV 会重复计数；口径为「一次扫码 = 一次 PV」。

### 4.2 微信小程序

- **位置**：`frontend/apps/miniprogram/src/app.ts` 的 `onLaunch`（与 `restoreMember()` 并列）。
- **桌台码（entry）**：扫桌台小程序码 / 扫普通二维码跳小程序都会带桌台码，
  由 `resolveLaunchTableCode(options)` 解析后一并上报 —— 后台「扫码入口 Top」
  看的是「哪张桌引来的客人」，小程序侧不带上就等于这一路客人全部不进榜。
  注意 `App.onLaunch` 的 `options.scene` 是**数字场景值**（如 1047），真正的
  码值在 `options.query.scene`；直接拿数字解析会产生 "1047" 这种假入口，
  故解析只接受字符串形态（见 `utils/scene.ts`）。没带码的直接打开为空串，属正常。
- **动作**：同一接口，`channel: "miniprogram"`，`visitorId` 存 `wx.setStorageSync`。
- **已登录会员**：若本地有会员令牌，把 `customerId` 一并带上（服务端仍以令牌解析为准，见下「可信度」），访客键升级为 `m:<customer_id>`。
- **口径**：PV = 启动次数（对齐微信官方统计）；`onShow` 不埋（前台切换会虚高）。

### 4.3 管理端

- **位置**：`handler.AdminLogin` 成功分支（`backend/interfaces/handler/auth.go`），与 `WriteLoginLog` 并列调用一次 `service.RecordVisit`。
- **参数**：`channel: "admin"`，访客键 `u:<userID>`（登录用户即访客，UV = 登录人数）。
- **零前端改动**：天然覆盖。

### 4.4 可信度规则（防伪造）

- `customer_id` **只认服务端从令牌解析的值**：打点接口若带 `Authorization: Bearer <顾客token>`（`CustomerAuthOptional` 同款逻辑），按令牌取 `customer_id`；请求体里的 `customerId` 一律忽略（防伪造归属，同下单接口的约定，见 `customer.go:101`）。
- `channel` 白名单校验（h5/miniprogram/admin），非法值归一为 `h5` 并 warn。
- `entry` 限长 16 字符并做 `[A-Za-z0-9]` 过滤。
- `visitorId` 限长 40、过滤非 `[A-Za-z0-9-]` 字符；缺失时服务端用 `ip_hash:ua_hash` 兜底键。

---

## 5. 接口设计

### 5.1 `POST /api/customer/visit`（公开，无需登录）

- **注册位置**：`main.go` 的顾客端公开组，与 `customer/table/:id` 并列。
- **限流**：进程内每 IP 60 次/分钟（仿 `loginGuard`：mutex + map + 闲置回收 + 硬上限淘汰，见 `auth_service.go:276-399`）。超限返回 200 但不落库（统计接口不打扰用户，与登录限流返回错误的定位不同）。
- **响应**：恒为 `{code:200, msg:"操作成功"}`——**参数非法也返回 200**，只记 warn 日志。埋点接口的正确性不应传导给用户（同 `customerFail` 的收敛哲学）。
- **投递**：算出访客键后 `select` 投递到带缓冲 channel（默认 4096）；**缓冲满则丢弃并 warn**（fail-open，绝阻塞请求）。

### 5.2 `GET /api/admin/report/visits`（需 `report:view`）

入参与现有报表完全一致：`start=YYYY-MM-DD&end=YYYY-MM-DD` 或 `days=N`（默认 30，上限 366，复用 `resolveRange`）。

出参：

```json
{
  "range": { "start": "2026-09-18", "end": "2026-10-17" },
  "summary": {
    "pv": 3820, "uv": 1260, "newUv": 310, "memberUv": 205,
    "totalVisitors": 1580, "memberCount": 132,
    "todayPv": 96, "todayUv": 61, "todayNewUv": 9,
    "yesterdayPv": 88, "yesterdayUv": 55
  },
  "daily": [
    { "date": "2026-10-17", "pv": 96, "uv": 61, "newUv": 9,
      "channels": { "h5": 78, "miniprogram": 16, "admin": 2 } }
  ],
  "channels": [
    { "channel": "h5", "pv": 3100, "uv": 980 },
    { "channel": "miniprogram", "pv": 660, "uv": 240 }
  ]
}
```

- `summary` 读 `tb_visit_daily` + `tb_visitor`（台账，快）；`channels[].uv` 在 retention 窗口内从明细 `COUNT(DISTINCT)`（窗口外只有 pv，接口对超窗日期只回 pv、uv 回 0 并在文档注明）。
- `memberCount` 直接 `SELECT COUNT(*) FROM tb_customer WHERE status=1`。
- **环比**：`yesterdayPv/UV` 由后端给绝对值，涨跌箭头由前端统一渲染（同 `report/summary` 的既有约定，见 `report.go:33` 的注释）。

### 5.3 权限

- **不新增权限点**：挂在既有 `report:view` 上（`perm.go:152-159` 的报表组）。访问统计是报表的自然组成部分；新增权限点要动 13 模块/28 点的目录、4 个内置角色矩阵、`menuOnlyPerms` 不变量与前端目录渲染，收益不抵风险。
- 新路由必须登记进 `routePerms`，否则 `routes_test.go` 的路由覆盖率测试直接失败（fail-closed，这是仓库的既有保险）。

---

## 6. 后端实现清单

| 文件 | 改动 |
|---|---|
| `internal/store/schema.go` | `schemaTemplate` 追加 3 张表；`indexes` 追加 5 个索引。启动时 `CREATE TABLE IF NOT EXISTS` 自动覆盖老库，**无需手工迁移** |
| `internal/po/visit.go`（新增） | `VisitLog` / `VisitDaily` / `Visitor` 三个 PO（风格对齐 `po/customer.go`） |
| `internal/store/dao/visit.go`（新增） | `WriteVisitBatch(logs, visitors)`（一个事务：多行 INSERT 按 200 行分块 + 访客 upsert，先 `UPDATE ... SET last_visit_date/visit_count` 后 `INSERT`，同 `NextPickupNoTx` 模式）；`RollupDay(date)`（从明细绝对重算当日 all + 各渠道行，唯一冲突视为并发已写）；`CleanVisitLogsBefore(cutoff)`；`VisitDailyRange`；`ChannelUVInRange` / `DistinctVisitorsInRange` / `DistinctMemberVisitorsInRange` / `NewVisitorsInRange`；`TopVisitEntries`（桌台码 Top，`table_code` 或 `table_id` 两种关联）；`VisitLogDates` / `MaxRolledUpDate`（启动补跑）；`CountVisitors` / `CountMembers`；四个配置读取 |
| `internal/service/visit_service.go`（新增） | `VisitEnabled()` / `RecordVisit(...)`（投递 channel）；批量写 worker（goroutine：攒批 200 条或 2s，一个事务写明细 + 访客 upsert，失败降级为仅明细重试一次，再失败 warn 丢弃）；`StartVisitRollup()`（每日 ticker + **启动补跑**：把明细中存在但台账缺失/落后的日期全部补算，防半夜停机漏算）；`VisitStats(start, end)`（用例级出参，口径注释同 `report_service.go`）；`CleanExpiredVisits()` |
| `internal/handler/visit.go`（新增） | `VisitTrack`（打点，含限流与参数清洗）；`ReportVisits`（报表） |
| `internal/handler/auth.go` | `AdminLogin` 成功分支调 `service.RecordVisit(channel=admin, key=u:uid)` |
| `internal/conf/conf.go` | `EnvVisitStatEnabled`/`EnvVisitRetentionDays`/`EnvVisitRatePerMin` + 默认值 |
| `main.go` | 注册 `POST /api/customer/visit` 与 `GET /api/admin/report/visits`（后者进 admin 组自动获得 `AdminAuth + RequirePerm + AuditLog` 链）；boot 阶段 `StartVisitRollup()` + 启动补跑 + `CleanExpiredVisits()`（与 `StartStaleOrderGuard`/`CleanExpiredOperLogs` 并列） |
| `config.example.yaml` | 补 `visit.*` 段（总开关/保留天数/限流） |
| `migrations/incr/{sqlite,mysql}/20261017010000_tb_visit_log_visitor_daily_create.sql`（新增，时间戳需晚于现有最大 `20261017000000`） | 三张表 + 5 个索引的手工升级脚本 |
| `migrations/full/{sqlite,mysql}/*` | `go run ./cmd/gensql` 重新生成（`sqlgen_test.go` 的 `-check` 一致性测试会强制校验） |
| README 配置表 / migrations README 表清单 | 各补一行/一张表说明 |

**批量写 worker 事务边界**：一个 flush 周期内，`BEGIN → INSERT 明细多行 → UPSERT 访客多行 → COMMIT`。访客 upsert 的「先 UPDATE 后 INSERT」在批内按 key 去重后执行（同批同访客只 upsert 一次）。SQLite 变量数上限（modernc 驱动默认 32766）下，200 行 × 13 列 ≈ 2600 参数，留有余量；分块常数 500 行是硬保护。

**启动补跑的必要性**：rollup ticker 若在 00:00 恰好错过（停机/重启），启动时对「明细中最大日期 < 台账最大日期+1」的缺口全部补算，保证台账连续。补算是绝对重算，重复执行无害。

---

## 7. 前端实现清单

| 文件 | 改动 |
|---|---|
| `packages/types/src/*` | `VisitChannel`、`VisitStats`、`VisitDailyRow`、`VisitChannelRow` 类型（camelCase 口径同 `ReportSummary`） |
| `packages/shared/src/*` | 新增 `visitorId()`：读 localStorage → 没有则 `crypto.randomUUID()`（降级 `Math.random` 拼 32 位 hex）→ 落库返回；存储键加入现有键常量表 |
| `packages/api/src/visit.ts`（新增） | `reportVisit(payload)`（公开接口，不带登录态）；`getVisitStats(params)`（走现有 http 实例，自动带管理端令牌） |
| `apps/customer/src/OrderPage.vue` | `onMounted` 里 fire-and-forget 调 `reportVisit({visitorId: visitorId(), channel:'h5', entry: route.params.tableId})` |
| `apps/miniprogram/src/app.ts` | `onLaunch` 里调 `reportVisit(...)`（storage 版 visitorId；有会员令牌时带 `Authorization`） |
| `apps/admin/src/views/report/VisitChart.vue`（新增） | ECharts 双轴图：PV/UV 按日折线（对齐 `TrendChart.vue` 的既有写法）+ 渠道构成小图（对齐 `HourlyMixCharts.vue`）；标题下注明 UV 口径提示 |
| `apps/admin/src/views/Report.vue` | 引入 `VisitChart`，`loadAll` 里并行拉 `getVisitStats` |
| `apps/admin/src/views/Dashboard.vue` | KPI 区追加「今日访问」卡（今日 UV/PV，`canReport` 降级逻辑内，无 `report:view` 时不渲染，同现有 KPI 卡模式） |
| `apps/admin/src/composables` 或视图内 | 环比箭头组件复用现有 `deltas` 计算模式 |

小程序埋点注意：`dist/` 体积增加约 2KB（visitorId 工具 + 一次请求），主包 2MB 上限无压力。

---

## 8. 配置项

| config.yaml | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `visit.enabled` | `VISIT_STAT_ENABLED` | `true` | 总开关；关闭后打点接口直接 200 不落库，worker 不起 |
| `visit.retention_days` | `VISIT_RETENTION_DAYS` | `90` | 明细保留天数，0 = 永不清理（台账不受影响） |
| `visit.rate_per_min` | `VISIT_RATE_PER_MIN` | `60` | 单 IP 每分钟打点上限 |

---

## 9. 测试与验收

| 层 | 用例 |
|---|---|
| dao | 批量插入分块正确；访客 upsert「新客建档/老客回访」；rollup 重算幂等（连跑两次数字不变）、跨渠道 UV 去重、缺日补零；清理按 cutoff 边界（含当天不含） |
| service | 区间解析复用 `resolveRange`（days 上限夹取）；channel 满丢路径（灌满 buffer 后请求不阻塞、有 warn 日志）；启动补跑（造缺口 → 启动 → 台账连续） |
| handler | 打点接口：非法 channel/超长 entry/缺失 visitorId 的归一与恒 200；限流（超限不落库）；带顾客令牌时 customer_id 以令牌为准、请求体伪造被忽略；报表接口权限（`routes_test.go` 自动覆盖，忘登记会红） |
| 前端 | `visitorId()` 生成与持久化（vitest）；`reportVisit` 不抛异常（mock 500）；VisitChart 渲染与空数据态 |
| 端到端（手动） | 手机扫码进 H5 → 后台报表当日 PV/UV +1；小程序冷启动一次 → miniprogram 渠道 +1；后台登录 → admin 渠道 +1；把 `retention_days` 设 0/小值验证清理；停服跨午夜重启验证补跑 |
| 回归 | 现有 customer/admin 全量测试绿；`go run ./cmd/gensql -check` 通过 |

---

## 10. 安全与隐私

- **不存明文 IP**：`sha256(ip + pepper)`，pepper 来自 `data/master.key`（与配置加密同密钥体系，搬迁库时已约定必须带走）。
- **访客 ID 是随机 UUID**，不含任何用户信息；localStorage 是第一方存储，不涉及第三方 Cookie。
- **会员关联最小化**：仅存 `customer_id`（内部 ID），不存 openid/昵称。
- **公开接口防护**：限流 + 参数白名单 + 恒 200 收敛，探测不到内部状态。
- **小程序隐私指引**：需补充「访问统计（生成随机设备标识用于统计访问量）」声明，作为上线前置（与会员体系的隐私声明同批更新，见 customer-member spec §6）。
- **管理端数据面**：访问报表走 `report:view`，与经营报表同权，不产生新的越权面。

---

## 11. 性能与容量估算

以单店峰值估算（20 桌 × 日均 15 轮扫码 + 小程序启动 + 后台登录）：

- 写入：日均 ~400 行明细，批量后约 2~3 个事务/天，**可忽略**。
- 读取：报表查台账（行数 = 天数 × 4），亚毫秒级；渠道 UV 的 `COUNT(DISTINCT)` 仅在 90 天窗口内扫明细（~3.6 万行），有 `(visit_date, channel, visitor_key)` 索引，十毫秒级。
- `tb_visitor` 按年增数千行，主键点查。
- 结论：现有 SQLite 单文件方案零压力；MySQL 部署同样无感（批量 INSERT 反而降低往返）。

**实时在线人数（二期候选）**：worker 里维护「最近 5 分钟去重访客」的内存集合，`summary.onlineNow` 输出。一期不做（避免为伪需求引入内存态复杂度）。

---

## 12. 分期

| 期 | 内容 | 依赖 |
|---|---|---|
| **一期（本方案）** | 打点 + 明细/访客/台账三表 + 批量写 + 日聚合与清理 + 报表接口 + Report 访问趋势图 + Dashboard 今日访问卡 | 无（纯增量） |
| 二期 | 入口桌台 Top 榜、回访频次分布、会员 UV 与下单转化（UV→下单率，可关联 `tb_order.customer_id`）、实时在线、导出 CSV | 一期数据积累 |
| 互补（不排期） | Nginx access log + goaccess 做请求级原始分析（不动代码，与自建统计互为验证） | 运维侧 |

---

## 13. 风险与权衡

| 风险 | 影响 | 对策 |
|---|---|---|
| 匿名 UV 精度有限（换机/换网/换浏览器） | 数字偏大 | 口径明示为「近似」；会员数与会员 UV 为精确口径；visitorId 已把同设备回访识别率做到现有方案的上限 |
| 缓冲满丢弃 | 高峰时极少数埋点丢失 | 400/天量级离 4096 缓冲有两个数量级余量；丢弃有 warn 可观测 |
| rollup 漏跑 | 台账断日 | 启动补跑 + 绝对重算幂等 |
| 明细清理后渠道 UV 不可查 | 超 90 天渠道构成只剩 PV | 台账保留 pv（精确）+ 当日 uv；渠道 UV 窗口限制写进接口注释与报表提示 |
| 小程序隐私声明未过审 | 小程序被下架 | 上线前置检查项（§10） |
| 权限面变动 | 新增权限点引发角色矩阵连锁改动 | 复用 `report:view`，零新增权限点 |

---

## 14. 拍板结论（2026-10-17，已按此实施）

| # | 决策 | 结论 |
|---|---|---|
| D1 | 访问明细保留天数 | **90 天**（`VISIT_RETENTION_DAYS=90`；日聚合台账与访客注册表长期保留） |
| D2 | 一期是否上 Dashboard 卡片 | **上**：管理首页新增「今日访问」KPI（UV 为主、PV 与新访客为副，随 `report:view` 显隐） |
| D3 | 小程序打点是否带会员身份 | **带**：`onLaunch` 打点经会员拦截器自动附顾客令牌，后端升级为 `m:<customerId>` 会员访客键；令牌过期时按匿名记录（`CustomerAuthOptional` 不拦截） |
| D4 | 桌台码入口是否一期展示 | **展示**：报表页新增「扫码入口 Top」横向条形图（桌名 · 桌号，Top 8） |

---

## 15. 增补：终端与来源信息（2026-10-18，已实施）

店家反馈想看到「客人都用什么手机、从哪来的」。一期只存了 IP/UA 的哈希（够 UV 去重、
不够排查），本期在 `tb_visit_log` 补六个列,采集双通道:

| 列 | H5 / 管理端 | 小程序 | 说明 |
|---|---|---|---|
| `ip` | 服务端 `ClientIP()`（可信代理已配） | 同左 | **明文**,与操作日志 `oper_ip` 同一取舍;明细 90 天清理,有限期暴露 |
| `city` | ip2region 离线库解析 | 同左 | 格式「中国 江苏省 南京市」/「内网」;**缺 xdb 文件时为空**,不影响其余统计 |
| `os` | UA 解析（iOS/Android/Windows/macOS/鸿蒙/Linux/其他） | 端上 `system` 直传（更准） | 归一展示值,不带版本号(版本进不了分布统计,机型列有详情) |
| `browser` | UA 解析（微信/支付宝/QQ浏览器/Chrome/Safari/Edge/Firefox/其他） | 固定「微信小程序」 | H5 能识别微信内打开(`MicroMessenger`) |
| `device` | UA 解析（手机/平板/电脑/其他） | 端上 `platform` 归一 | Android 平板的 UA 不含 Mobile,据此区分 |
| `model` | 留空(UA 只能到品牌档,不准的不存) | 端上 `model` 直传（如 iPhone 15 Pro） | **只信小程序渠道**:H5 正常请求拿不到机型,带上的一律是伪造 |

实现要点:

- **UA 解析手写**（`service.parseUA`）,不引第三方库:分布统计只需要粗粒度结论。
  判定顺序敏感——浏览器先于 Safari/Chrome(Edge/Opera 的 UA 带 Chrome 关键字),
  平板先于手机(Android 平板的 UA 不含 Mobile)。
- **城市用 ip2region v2 离线库**（`infrastructure/geoip`）:纯内存查询、无外呼,
  文件路径 `visit.ip2region_path`（默认 `data/ip2region.xdb`,gitignored 运行期资产,
  从 ip2region v2.11.0 release 下载）;缺文件时 city 为空、只告警一次。
- **分布查询走维度白名单**（`dao.VisitDistribution`）:GROUP BY 的列名不能来自
  用户输入;空值行不过滤、原样返回,由上层归一「未知」——分布各项之和恒等于
  区间总 PV,「未知」占比即采集覆盖率的直观呈现。
- 报表页新增「终端构成」三张小图(设备类型/操作系统/浏览器),横条 Top 6。
- **隐私**:小程序「用户隐私保护指引」需补「设备信息」声明(`wx.getDeviceInfo`
  属于收集设备信息,上线硬前置);IP 明文的取舍与操作日志一致,报表本就仅
  `report:view` 可见。

历史行(增补上线前)六个列均为空,不做回填。
