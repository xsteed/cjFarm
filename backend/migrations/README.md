# 数据库迁移脚本（backend/migrations）

本目录集中管理点餐管理系统的 **建库与升级脚本**，位于后端工程内，
与 `infrastructure/store` 的表结构 / 种子定义保持一一对应，并**按数据库产品分为两套**：
SQLite（默认、单机）与 MySQL（多实例 / 高并发部署）。

## 一、目录结构与命名规则

```
backend/migrations/
├── README.md                                              # 本说明
├── full/                                                  # 全量脚本：全新部署，一次建好最终结构 + 存量数据（内联历史上所有增量变更）
│   ├── sqlite/
│   │   ├── schema.sql                                     #   建表 DDL（表 / 索引数量以 gensql -check 输出为准）
│   │   └── seed.sql                                       #   种子 / 存量数据
│   └── mysql/
│       ├── schema.sql                                     #   同上（InnoDB / utf8mb4 / AUTO_INCREMENT）
│       └── seed.sql
└── incr/                                                  # 增量脚本（唯一落点）：老库按文件名时间前缀的字典序升级；到期（90 天）清理
    ├── sqlite/                                            #   当前：2026-10-19 储值合规改造（见第三节）
    └── mysql/                                             #   同名脚本，方言差异仅体现在 ALTER/索引语法
```

> **当前在册的增量脚本**（只落 `incr/`，`full/` 不放副本）：
>
> | 时间戳 | 文件 | 内容 |
> |---|---|---|
> | `20261019120000` | `tb_wallet_txn_recharge_agreement_gift.sql` | 储值本金 / 赠金分桶（`tb_wallet.balance_gift`、`tb_wallet_txn.gift_amount/gift_after`、`tb_recharge.gift_amount`）+ 新表 `tb_wallet_agreement`（《储值规则》同意留痕） |
> | `20261019120100` | `tb_config_backfill_wallet_settings.sql` | 储值合规参数与出厂规则文案的种子（`INSERT IGNORE`，不覆盖门店已改过的值） |
> | `20261019130000` | `tb_config_backfill_print_guest_large_font.sql` | 顾客小票（点菜单 / 结账小票）整张放大开关 `print_guest_large_font`（默认 `0`；纯数据项、无结构变更，与启动时 `EnsureSettingDefaults` 等效） |
> | `20261024150000` | `tb_remember_token_add_ip_column.sql` | 登录设备页来源 IP（`tb_remember_token.ip`：签发时写入、每次免登录使用刷新为最近 IP；与启动时 `store.alterCols` 补列等效） |

> 本 README 只记录**固定规范与约定**，不收录增量脚本逐条流水；
> 脚本的作用见其头部注释，历史演进以 git 提交记录为准。
>
> 另有 `tb_print_log`（打印日志）与 `tb_printer` 的飞鹅云字段，因早于增量脚本机制建立，
> 老库升级由 `store.alterCols` 在启动时自动补列/建表，`full/` 脚本已包含最终结构。

**命名规则**

| 目录 | 规则 | 示例 |
|---|---|---|
| `full/` | **通用命名**：`schema.sql` 为建表脚本；`seed.sql` 为种子 / 存量数据 | `schema.sql`、`seed.sql`（SQLite 库为 `data/dining.db`，MySQL 库为 `dining`） |
| `incr/` 增量脚本 | **`YYYYMMDDHHMMSS_表名_动作.sql`**，时间戳 14 位、**精确到秒** | `20260920022000_tb_order_add_pay_columns.sql` |

`incr/` 时间前缀的**硬性约束**（保证「文件名字典序 = 执行顺序」，缺一不可）：

1. 时间戳固定 14 位 `YYYYMMDDHHMMSS`，精确到**秒**；靠前补零，不得缩位
   （如不得用 `YYYYMMDDHHMM` 十二位，否则同小时内多个脚本无法排序）。
2. 同一目录（`sqlite/`、`mysql/` 各自）内时间戳**全局唯一且严格递增**：
   同一时刻（同一秒）发生多个变更时，后一个脚本取比前一个**晚至少 1 秒**的时间戳，
   禁止两个脚本共用同一时间前缀 —— 字典序并列时执行顺序不确定。
3. 新增脚本的时间取「**晚于现有最大时间戳**」，建议直接取编写当天日期时间；
   已落盘文件的时间戳与文件名**不得再改**（外部环境可能已按旧名执行过）。
4. 两库文件名保持一致，仅方言专属脚本例外（如仅 SQLite 需要的触发器脚本，
   MySQL 由建表语句原生 `ON UPDATE` 承担），例外必须在目录树注释中说明。
5. **只在 `incr/` 落盘**：`full/{sqlite,mysql}/` 下只有生成产物（`schema.sql` / `seed.sql`），
   **不放单独的变更脚本** —— 结构变更一律由 `go run ./cmd/gensql` 内联进 `full/schema.sql`
   （种子类变更进 `seed.sql`），同一处变更不在两处各留一份，避免日后漂移。

- **第一层按数据库产品分目录**（`sqlite/`、`mysql/`），便于对照排查；切换数据库时执行对应目录下的脚本即可。
- 常用动作词：`create`（建表/建索引）、`add_xxx_columns`（加列）、`drop_xxx`（删列/删表）、
  `backfill`（历史数据回填）、`fix_xxx`（数据修正）。
- 一次变更涉及多张表时，表名部分用下划线连接（如 `tb_payment_refund_create`）。

**生命周期：到期同步进 full 后清理**

增量脚本只服务于「老库窗口期」的升级，**不是永久资产**；`full/schema.sql` 由
`go run ./cmd/gensql` 从 `infrastructure/store` 生成、始终内联**最终结构**（含历史上所有
`ALTER TABLE` 追加的列），因此「同步进 full 建表 SQL」无需手工搬 SQL，按以下流程定期清理：

1. **到期判定**：脚本落盘超过 **90 天**，且确认线上已无按旧结构运行的存量库
   （等价变更已由启动时 `store.alterCols` 幂等补列兜底的，可提前清理）。
2. **同步核验**：`go run ./cmd/gensql -check` 通过 —— 即 `full/{sqlite,mysql}/schema.sql`
   已包含全部等效变更（建表 / 索引 / 种子），视为「已同步进 full」。
3. **清理动作**：删除 `incr/{sqlite,mysql}/` 下的同名文件（`full/` 本就不放副本）；
   历史记录以 git 提交为准，本 README 不补录流水。
4. 全部清理后 `incr/` 目录本身保留（必要时以 `.gitkeep` 占位），
   命名规范对后续新脚本继续生效。

> **脚本是生成的，不是手写的**：`full/` 下 `schema.sql` / `seed.sql` 由
> `go run ./cmd/gensql` 从 `infrastructure/store` 的表结构与种子数据定义自动生成
> （单一事实来源，避免两库脚本各自漂移）。改结构流程见第六节；
> `incr/` 下的脚本为手工编写的增量升级记录。

## 二、全新部署（full）

**SQLite（默认，零依赖）**

```bash
cd backend
sqlite3 data/dining.db < migrations/full/sqlite/schema.sql
sqlite3 data/dining.db < migrations/full/sqlite/seed.sql
```

**MySQL**

```bash
# 0) 先建库与账号（只需一次）
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS dining
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;"

# 1) 建表 + 种子数据
mysql -u root -p dining < migrations/full/mysql/schema.sql
mysql -u root -p dining < migrations/full/mysql/seed.sql
```

> 也可**不跑脚本**：直接启动后端，程序启动时会自动执行等价的建表与种子逻辑。
> 本目录脚本用于「独立管理数据库结构」的场景（DBA 审核、离线建库、CI 校验）。
>
> 幂等性：建表均为 `IF NOT EXISTS`，种子均为 `INSERT OR IGNORE`（MySQL 为 `INSERT IGNORE`）
> 且显式指定主键，重复执行不会报错、不会产生重复数据。
> **例外**：MySQL 的 `CREATE INDEX` 没有 `IF NOT EXISTS` 语法，重复执行会报
> `Duplicate key name`，属正常现象、可忽略。

## 三、老库升级（incr）

增量脚本正本在 `incr/{sqlite,mysql}/`，按文件名时间顺序依次执行。

> 更早的增量脚本已到期清理，等效变更已内联进 `full/` 建表 / 种子脚本（见 git 历史）。
> 当前在册的脚本见第一节的「当前在册的增量脚本」表（2026-10-19 储值合规改造）。
>
> **两条升级路径等价，任选其一即可**：
> ① 由 DBA 按文件名顺序执行 `incr/` 下的脚本（离线、可审核、不依赖进程重启）；
> ② 直接重启后端，由启动时的自动迁移兜底（`migrate()` 建缺失表、
>    `store.alterCols` 补缺失列，均幂等）—— 这也是**默认部署方式**，
>    绝大多数门店升级走的是这条。
>
> 两条路径同时存在不会冲突：自动补列会跳过已存在的列；
> `CREATE TABLE IF NOT EXISTS` 也保证建表幂等。

**SQLite**

```bash
cd backend
for f in migrations/incr/sqlite/*.sql; do sqlite3 data/dining.db < "$f"; done
```

**MySQL**

```bash
cd backend
for f in migrations/incr/mysql/*.sql; do mysql -u dining -p dining < "$f"; done
```

> ⚠️ `add_*_columns` 系列使用 `ALTER TABLE ADD COLUMN`，**两种数据库都不支持**
> `IF NOT EXISTS`。列已存在时会报 `duplicate column name` / `Duplicate column name`，
> 属正常现象、可忽略。执行前可自查：
> - SQLite：`PRAGMA table_info(tb_order);`
> - MySQL：`SHOW COLUMNS FROM tb_order;`
>
> `drop_*` 系列（如删 `tb_order` 遗留 `begin_time` / `end_time`）同样没有
> `DROP COLUMN IF EXISTS` 语法，重复执行报 `no such column` / `check that column/key exists`，
> 同样属正常现象、可忽略。
>
> `create` 系列为纯建表（`IF NOT EXISTS`），可安全重复执行。
>
> ⚠️ 「时间列改由数据库自动维护」类变更（SQLite 建 `update_time` 触发器；
> MySQL 用 `DATETIME` + `DEFAULT`/`ON UPDATE`）已内联在 `full/schema.sql`；
> 若需在老库手工执行同类变更，注意：先清洗空串与 `'0000-00-00'` 历史零值；
> `CURRENT_TIMESTAMP` 取会话时区，执行前确认 `time_zone` 与部署时区一致；
> 大表（`tb_order` / `tb_oper_log`）的 `MODIFY` 会重建表，请低峰执行。

## 四、表清单

> 表与索引的**最终结构一律以 `full/{sqlite,mysql}/schema.sql` 为单一事实来源**；
> 历史上经历过增量脚本演进的表不在此逐一标注，细节见 git 历史。

| # | 表名 | 说明 | 种子数据 |
|---|---|---|---|
| 1 | `tb_table` | 桌台 | 8 桌 |
| 2 | `tb_category` | 菜品分类 | 6 类 |
| 3 | `tb_dish` | 菜品 | 21 道 |
| 4 | `tb_spec` | 菜品规格 | 32 条 |
| 5 | `tb_remark` | 备注选项 | 6 项 |
| 6 | `tb_printer` | 打印机（ESC/POS 网络 + 飞鹅云） | 2 台（停用） |
| 7 | `tb_config` | 系统配置（键值对） | 39 项 |
| 8 | `tb_order` | 订单主表 | 运行时产生 |
| 9 | `tb_order_item` | 订单明细 | 运行时产生 |
| 10 | `tb_order_urge` | 催菜/加菜记录 | 运行时产生 |
| 11 | `tb_review` | 订单评价（仅商家可见的内部反馈；桌台/人数/会员昵称与手机号提交时快照，IP 只存胡椒哈希） | 运行时产生 |
| 12 | `tb_review_image` | 评价图片（顾客上传的用户内容，独立于商家资产表 tb_image；BLOB 入库，孤儿 24h 清理） | 运行时产生 |
| 11 | `tb_payment` | 支付流水（在线支付） | 运行时产生 |
| 12 | `tb_refund` | 退款流水 | 运行时产生 |
| 13 | `tb_print_log` | 打印日志（成功/失败留痕、票据标识与票面快照、支持补打与预览回放） | 运行时产生 |
| 14 | `tb_role` | 角色（权限点集合） | 4 个内置角色 |
| 15 | `tb_user` | 员工账号 | 首次启动引导超管（不写种子） |
| 16 | `tb_oper_log` | 操作日志（审计留痕：谁在何时改了什么） | 运行时产生 |
| 17 | `tb_remember_token` | 记住我令牌（7/30 天免登录，过期由后端查库裁决） | 运行时产生 |
| 18 | `tb_print_job` | 本地打印代理任务队列（云后端入队，门店代理取单后直发 9100） | 运行时产生 |
| 19 | `tb_print_agent` | 本地打印代理注册与令牌状态 | 运行时产生 |
| 20 | `tb_image` | 图片内容表（菜品图 / 收款码 / 上传图二进制） | 启动时写入出厂图 |
| 21 | `tb_day_seq` | 按营业日维护「当日取餐号」的计数器 | 运行时产生 |
| 22 | `tb_customer` | 会员档案（账号，与登录方式无关；用于「我的订单」与订单归属） | 运行时产生 |
| 23 | `tb_visit_log` | 访问明细（站点 PV/UV 打点，「待办」性质，按保留天数清理） | 运行时产生 |
| 24 | `tb_visitor` | 访客注册表（独立访客档案，「新访客」精确口径，长期保留） | 运行时产生 |
| 25 | `tb_visit_daily` | 日聚合台账（每日每渠道 PV/UV/新访客，报表主读，长期保留） | 运行时产生 |
| 26 | `tb_customer_identity` | 会员登录方式（provider + external_id 绑定，账号与登录方式解耦；为 App/网站预留） | 运行时产生 |
| 27 | `tb_wallet` | 会员储值账户（本金 balance / 赠金 balance_gift 分桶 + 累计充值/消费/赠送，主键即 customer_id） | 运行时产生 |
| 28 | `tb_wallet_txn` | 储值流水（台账只增不改，balance_after 逐行累加必须等于当前余额；gift_amount/gift_after 是赠金桶镜像） | 运行时产生 |
| 29 | `tb_recharge` | 储值充值单（微信 JSAPI 支付，单号 R 开头与订单 D 开头靠前缀分流回调；gift_amount 冻结赠送额） | 运行时产生 |
| 30 | `tb_wallet_agreement` | 《储值规则》同意留痕（合规：发卡须明示章程，记会员同意的是哪一版） | 运行时产生 |

> `tb_print_job` 与 `tb_print_log` 的分工：前者是「待办」（送达即结案，可定期清理），
> 后者是「台账」（给商家查「这单打了没」，长期保留），两者由 `print_log_id` 关联。
> 不用 `agent` 通道时该表始终为空，行为与升级前完全一致。
> 部署与排障见 [`../../docs/print-agent.md`](../../docs/print-agent.md)。
>
> `tb_user` **不写种子数据**：由启动引导 `store.EnsureAdminUser()` 按
> `ADMIN_USER` / `ADMIN_PASS`（默认 `admin`/`admin123`）生成首个超级管理员。
> `tb_role` 的 4 个内置角色（admin / manager / cashier / staff）每次启动由
> `store.SyncBuiltinRoles()` 幂等校准；其中 `admin` 的权限**强制恢复为全量**，防止误改锁死。

## 五、索引清单

| 索引名 | 表 | 类型 | 用途 |
|---|---|---|---|
| `idx_order_no` | tb_order | UNIQUE | 订单号唯一 |
| `idx_order_pickup` | tb_order | UNIQUE（2 列） | 同一营业日的取餐号不重复（`biz_date` 为 NULL 的历史行不参与判定） |
| `idx_order_table` | tb_order | 普通 | 按桌台查单 |
| `idx_order_table_unpaid` | tb_order | 普通（3 列） | 未结账单判定 + 看板按桌分组 |
| `idx_order_status` | tb_order | 普通 | 按状态查单 |
| `idx_order_create_time` | tb_order | 普通 | 报表按时间 |
| `idx_order_pay_time` | tb_order | 普通 | 报表按收款到账时间 |
| `idx_order_credit_settle_time` | tb_order | 普通 | 报表按挂账核销时间 |
| `idx_order_finish_time` | tb_order | 普通（2 列） | 已完成订单数与用餐时长按结束时间归属 |
| `idx_order_customer` | tb_order | 普通（2 列） | 会员「我的订单」按时间倒序分页 |
| `idx_identity_provider_external` | tb_customer_identity | UNIQUE（2 列） | 会员登录查档主路径；同时兜底「并发首登建档」的幂等性（`idx_customer_openid` 已随增量演进删除） |
| `idx_identity_customer` | tb_customer_identity | 普通 | 查「这个人绑了哪几种登录方式」（解绑 / 多端合并） |
| `idx_identity_unionid` | tb_customer_identity | 普通 | 同一人的多个 openid 共享 unionid，按它反查归并；必然重复故非唯一 |
| `idx_recharge_no` | tb_recharge | UNIQUE | 充值单号唯一，回调与前端轮询按单号定位 |
| `idx_wallet_txn_customer` | tb_wallet_txn | 普通（2 列） | 会员储值流水按时间倒序翻页 |
| `idx_order_item_order` | tb_order_item | 普通 | 明细按订单 |
| `idx_payment_order` | tb_payment | 普通 | 流水按订单号 |
| `idx_payment_channel_trade` | tb_payment | UNIQUE（2 列） | 渠道交易号防重 |
| `idx_refund_no` | tb_refund | UNIQUE | 退款单号唯一 |
| `idx_refund_order` | tb_refund | 普通 | 退款按订单 |
| `idx_refund_update_time` | tb_refund | 普通 | 报表按退款到账时间 |
| `idx_urge_status` | tb_order_urge | 普通（2 列） | 看板待处理催菜 |
| `idx_urge_order` | tb_order_urge | 普通 | 催菜按订单 |
| `idx_review_order_reviewer` | tb_review | UNIQUE（2 列） | 一人一评（order_no+reviewer_key 唯一，同桌各评一次、并发双击兜底） |
| `idx_review_time` | tb_review | 普通 | 评价列表按时间翻页 |
| `idx_review_rating` | tb_review | 普通（2 列） | 按评分档位筛选 |
| `idx_review_image_name` | tb_review_image | UNIQUE | 图片文件名唯一（展示/绑定凭据） |
| `idx_review_image_review` | tb_review_image | 普通 | 按评价取图/级联删除 |
| `idx_review_image_time` | tb_review_image | 普通 | 扫描未绑定孤儿 |
| `idx_print_log_order` | tb_print_log | 普通 | 打印日志按订单倒查 |
| `idx_print_log_time` | tb_print_log | 普通 | 打印日志按时间翻页 |
| `idx_print_log_status` | tb_print_log | 普通（2 列） | 按结果筛失败记录 |
| `idx_print_job_pick` | tb_print_job | 普通（3 列） | 代理取单（按状态 + 可执行时间，3 秒轮询一次） |
| `idx_print_job_printer` | tb_print_job | 普通（2 列） | 按打印机看积压 / 清空队列 |
| `idx_print_agent_token` | tb_print_agent | 普通（2 列） | 代理令牌哈希与状态查询 |
| `idx_table_code` | tb_table | 部分唯一 | 桌台点餐码唯一 |
| `idx_user_username` | tb_user | 部分唯一 | 登录名唯一（仅未删除行） |
| `idx_user_role` | tb_user | 普通 | 员工按角色统计 |
| `idx_role_key` | tb_role | UNIQUE | 角色标识全局唯一（含已删除行，重建同名角色走「复活原行」） |
| `idx_operlog_time` | tb_oper_log | 普通 | 操作日志按时间翻页（主查询） |
| `idx_operlog_operator` | tb_oper_log | 普通（2 列） | 按人追责「谁今天干了什么」 |
| `idx_operlog_target` | tb_oper_log | 普通（2 列） | 按对象倒查「这张单被谁动过」 |
| `idx_operlog_status` | tb_oper_log | 普通（2 列） | 只看失败 / 被拒绝的操作 |
| `idx_remember_token` | tb_remember_token | 部分唯一 | 记住我令牌唯一，查库裁决是否过期 |
| `idx_image_name` | tb_image | UNIQUE | 图片文件名唯一 |
| `idx_visitlog_date` | tb_visit_log | 普通 | 访问明细按天扫（rollup 与清理） |
| `idx_visitlog_visitor` | tb_visit_log | 普通（2 列） | UV 去重：按天 + 访客键 |
| `idx_visitlog_channel` | tb_visit_log | 普通（3 列） | 渠道 UV 窗口查询 |
| `idx_visitor_first` | tb_visitor | 普通 | 新访客按首访日期圈选 |
| `idx_visit_daily_uk` | tb_visit_daily | UNIQUE（2 列） | 日聚合定位键，rollup 幂等重算 |

> **部分唯一索引的差异**：`idx_table_code` / `idx_user_username`
> 在 SQLite 下是**部分唯一索引**（带 `WHERE` 条件，分别只约束非空桌台码、
> `del_flag='0'` 的未删除行）；MySQL 不支持部分索引，会降级为普通索引。
> 为此代码在应用层补了存在性校验 —— `store.NewUniqueTableCode()`、
> `store.EnsureUsernameAvailable()` / `EnsureRoleKeyAvailable()`，
> 保证两种后端下的唯一性语义一致（员工/角色软删除后同名可复用）。
>
> `idx_role_key` 不是部分唯一索引：历史上 MySQL 降级为普通索引后，
> 启动引导 `SyncBuiltinRoles()` 依赖「INSERT IGNORE 撞唯一约束」的幂等失效，
> 每次启动都会重复插入 4 个内置角色。现已升级为两库统一的全局唯一索引，
> 软删除的角色行仍占用 `role_key`，重建同名角色时由 `dao.InsertRole` 复活原行；
> 老库升级时由启动迁移 `dedupeRoleKeys()` 先清理历史重复行（保留未删除行中
> role_id 最小的一行，员工引用改挂后物理删除其余），再原地重建唯一索引。

## 六、两套数据库的差异与同步维护

| 差异点 | SQLite | MySQL | 处理方式 |
|---|---|---|---|
| 自增主键 | `INTEGER PRIMARY KEY AUTOINCREMENT` | `INT NOT NULL AUTO_INCREMENT PRIMARY KEY` | 由 `dialect.go` 的 `autoIncPKFor` 生成 |
| 建表选项 | 无 | `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci` | `tableOptionsFor` |
| 索引幂等 | `CREATE INDEX IF NOT EXISTS` | 无此语法 | 生成时不加，重复执行报错可忽略 |
| 部分索引 | 支持（`WHERE ...`） | 不支持 | MySQL 降级为普通索引 + 应用层校验 |
| 插入忽略 | `INSERT OR IGNORE` | `INSERT IGNORE` | `InsertIgnoreInto` / `InsertReplaceInto` |
| 插入替换 | `INSERT OR REPLACE` | `REPLACE INTO` | 同上 |
| 列是否存在 | `PRAGMA table_info` | `information_schema.COLUMNS` | `columnExists` |
| 索引是否存在 | `sqlite_master` | `information_schema.STATISTICS` | `indexExists` |
| 字符串类型 | `TEXT` | `VARCHAR(n)`（强制长度校验） | 生成时按列定义给出长度 |

**改了表结构后要做的三件事**（缺一不可）：

```bash
cd backend
# 1) 修改 infrastructure/store 里的表结构 / 种子数据定义,
#    并在 schema_comment.go 登记新表/新列的注释(测试强制)
# 2) 重新生成两库的 full 脚本
go run ./cmd/gensql
# 3) 跑测试
go test ./...
```

> 新增增量脚本只落在 `incr/{sqlite,mysql}/`；**不要**复制到 `full/{sqlite,mysql}/` ——
> 结构变更由 `go run ./cmd/gensql` 内联进 `full/schema.sql`（见第一节命名规则第 5 条）。
> 老库的结构升级也可依赖后端启动时的 `store.alterCols` 自动补列/建表（幂等，与 full 脚本等价）。

`go run ./cmd/gensql -check` 可用于 CI：只校验磁盘脚本是否与 Go 定义一致，不写盘。
`infrastructure/store/sqlgen_test.go` 也会在 `go test` 时自动做这项一致性校验
（含「MySQL 脚本不得出现 AUTOINCREMENT / PRAGMA / TEXT」等方言断言）。

## 七、约定与注意事项

1. **金额单位**：所有金额字段一律以「**分**」存 INTEGER（如 `2800` = 28.00 元）。
   API 层由 `po.ToYuan / ToCents` 在边界换算，脚本中不要写元。
2. **时间字段**：统一以 `YYYY-MM-DD HH:MM:SS` 的本地时间文本呈现，两库对外行为一致
   （便于跨库搬迁，MySQL 侧无需处理时区类型差异）。**物理类型按方言分叉**：

   | | MySQL | SQLite |
   |---|---|---|
   | 物理类型 | `DATETIME` | `VARCHAR(32)`（SQLite 的类型只是亲和性提示） |
   | `create_time` | `DEFAULT CURRENT_TIMESTAMP` | `DEFAULT (datetime('now','localtime'))` |
   | `update_time` | `DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP` | `AFTER UPDATE` 触发器 |
   | 自动更新实现 | 列定义原生支持 | `trg_<表>_touch` 触发器 |

   MySQL 用时间类型是**必须**的 —— `DEFAULT CURRENT_TIMESTAMP` / `ON UPDATE` 只对
   DATETIME/TIMESTAMP 生效，VARCHAR 给不出这种语义。读取侧由 `dao.TS()` 统一折算
   （MySQL 在 `parseTime=true` 下返回 `time.Time`，SQLite 返回字符串），
   因此 service / handler / 前端 JSON 拿到的仍是同一格式的字符串。

   填写口径：**`create_time` 只在 INSERT 时产生，`update_time` 只在行更新时产生**
   （插入时留 NULL，便于区分「创建后没动过」与「刚创建」）。应用层仍会显式写入
   这两个值（保证老库、跨方言、批量写入下都确定），数据库层的默认值与触发器是
   **第二道保证**：任何绕过应用层的写入（运维手工 SQL、外部导入、脚本）同样拿到
   正确时间。触发器带 `WHEN NEW.update_time IS OLD.update_time` 守卫，
   不会覆盖应用显式写入的值 —— 与 MySQL 的 `ON UPDATE` 语义一致。

   例外：走 REPLACE 写入的 `tb_config` 写不了 `create_time`（整行替换会冲掉首次
   落库时间）——只在种子语句里写一次 `create_time`，之后改配置只推进 `update_time`。
   用餐时长对应的两个落库列是 `tb_order` 的 `meal_start_time` / `meal_end_time`
   （下单写起点、走到终态即 4 已完成 / 5 已取消写终点；撤销结算时终点清空）。
   老库由 `store.alterCols` 自动补列 + `backfillMealTimes()` 按 `create_time` /
   `finish_time` 幂等回填，读侧 `COALESCE` 回落旧列，升级前后统计口径一致。
3. **图片/收款码**：图片内容存于 `tb_image` 表；出厂图片内嵌在后端二进制中、
   首次启动自动写入数据库，`seed.sql` 不含图片数据；老版本升级时旧 `uploads` 目录中的
   存量图片由后端启动时自动导入。业务表仍保存 `/uploads/<文件名>` 虚拟路径（该前缀由
   后端 `store.UploadURLPrefix` 统一定义，启动时会把老库里的 `/picture/` 前缀自动改写）。
4. **软删除**：业务表用 `del_flag`（`'0'` 正常 / `'1'` 删除），查询需带 `del_flag='0'`。
5. **full 与增量脚本的关系**：`full/schema.sql` 始终体现**最终结构**（已内联历史上所有
   `ALTER TABLE` 追加的列）；`incr/`（`full/` 下留同步副本）承载**逐步演进过程**，
   供老库窗口期按序升级，到期同步进 full 后清理（见第一节生命周期）。
6. **主密钥别丢**：`tb_config` 中的敏感项是 AES-256-GCM 密文，主密钥在
   `backend/data/master.key`。搬迁数据库时必须把它一并带走，否则密文无法解密。
7. **加列必须带 `AFTER`**（仅 MySQL 增量脚本）：`ALTER TABLE ... ADD COLUMN ... AFTER <已有列>`
   不能省略。`full/schema.sql` 里新列写在语义相邻列**中间**，若增量脚本不写 `AFTER`，
   MySQL 会把列追加到表**末尾**，导致 `store.alterCols` 启动补列后的列顺序与全量建表对不上。
   `AFTER` 的取值以 `full/mysql/schema.sql` 中该列实际位置为准（如 `viewed_at AFTER meal_end_time`）。
   SQLite 不支持 `AFTER` 且加列只能追加末尾，**无需也不能写**；两库最终列顺序都以
   `full/schema.sql` 为单一事实来源。曾落盘的增量脚本均有 `AFTER` 示范（见 git 历史）。
8. **字段注释规范**：MySQL 建表语句的每张表、每一列都带 `COMMENT` 简释字段含义
   （枚举值直接写进注释，如 `order_status` 的「1已下单 2制作中…」），DBA / 运维
   `SHOW CREATE TABLE` 即可看懂结构。注释定义在
   `infrastructure/store/schema_comment.go` 的 `tableComments` / `columnComments`
   两张 map（键为 `表名` / `表名.列名`），由生成器在 MySQL 方言下注入；
   **新增表 / 列必须同步登记，`sqlgen_test.go` 的 `TestSQLCommentsCoverEveryColumn`
   强制覆盖率**。注释文本不写英文单引号。
   SQLite 无注释语法，同一份 DDL 不生成 `COMMENT`；字段说明以建表模板里的 `--`
   行内注释与本文档为准（因此两库脚本除注释外逐字符一致）。

### MySQL 大图写入注意

`tb_image` 单条 `INSERT` 可达 64MB，MySQL 5.7 服务端默认 `max_allowed_packet=4MB` 会拒绝，
需提前调大，例如：

```sql
SET GLOBAL max_allowed_packet=128M;
```

客户端侧后端默认 DSN 已带 `maxAllowedPacket=134217728`，无需额外配置。

## 八、校验记录

> 以下为人工复核快照；**表 / 索引清单见第四、五节，种子行数以 `go run ./cmd/gensql -check`
> 的输出为准**，加表加列后不必回来改这里的数字（`schemaTemplate` 与 `SeedCounts()`
> 自动核对，`go test ./...` 中 `sqlgen_test.go` 会同步做一致性校验）。

**SQLite（3.53）**

- `full/`：建库 + 重复执行验证幂等（建表 `IF NOT EXISTS`、种子 `INSERT OR IGNORE`，
  重复执行数量不变）；`tb_order` 全列齐全。
- 历史 `incr/` 脚本曾按序在旧结构上验证：加列 / 回填 / 建表均正确
  （脚本已到期清理，细节见 git 历史）。
- **2026-10-19 储值合规脚本**：用「改动前的 `full/sqlite/{schema,seed}.sql`」造了一个
  真实老库，按序执行 `incr/sqlite/` 下两个脚本后验证 ——
  `tb_wallet` / `tb_wallet_txn` / `tb_recharge` 的列集合与新建库**完全一致**，
  `tb_wallet_agreement` 与 `trg_tb_wallet_agreement_touch` 均已创建，
  9 个 `wallet_*` 配置项就位；随后以 `DB_PATH` 指向该库启动后端，
  启动迁移无冲突、`/api/version` 正常返回 200。
  （SQLite 的 `ADD COLUMN` 只能追加到表末尾，故物理列顺序与 `full/schema.sql` 不同 ——
  这是两库已知差异，见第六节表格与第七节第 7 条。）

**MySQL（在 MySQL 协议兼容服务端上实跑）**

- `full/`：建表 + `SHOW CREATE TABLE` 确认 `AUTO_INCREMENT`、`InnoDB`、
  `utf8mb4_general_ci`、`UNIQUE KEY` 均正确。
- 后端以 `DB_DRIVER=mysql` 启动，完成「登录 → 菜单 → 下单 → 加菜 → 催菜 →
  改单重算 → 状态流转 → 结账 → 报表」全链路。
- `scripts/sqlite2mysql.py` 搬运实测：与源库逐表一致，中文无乱码。
- ⚠️ **待补测**：近期新增的表加入后，尚未在 MySQL 上重跑一次全量建库。
  注意 `tb_user` 的部分唯一索引在 MySQL 下会降级为普通索引，
  唯一性改由应用层保证（见第五节说明）；`tb_role` 的 `idx_role_key` 已升级为
  两库统一的全局唯一索引，不受此问题影响。
