# 数据库迁移脚本（backend/migrations）

本目录集中管理点餐管理系统的 **建库与升级脚本**，位于后端工程内，
与 `internal/store/schema.go`、`seed.go` 保持一一对应，并**按数据库产品分为两套**：
SQLite（默认、单机）与 MySQL（多实例 / 高并发部署）。

## 一、目录结构与命名规则

```
backend/migrations/
├── README.md                                              # 本说明
├── full/                                                  # 全量脚本：全新部署，一次建好最终结构 + 存量数据
│   ├── sqlite/
│   │   ├── schema.sql                                     #   建表 DDL（22 表 + 34 索引）
│   │   └── seed.sql                                       #   种子 / 存量数据
│   └── mysql/
│       ├── schema.sql                                     #   同上（InnoDB / utf8mb4 / AUTO_INCREMENT）
│       └── seed.sql
└── incr/                                                  # 增量脚本：老库按文件名时间顺序升级（两库各 25 个，文件名一致）
    ├── sqlite/
    │   ├── 20260920020000_tb_order_urge_create.sql                    # 01 催菜/加菜记录表
    │   ├── 20260920021000_tb_payment_refund_create.sql                # 02 支付/退款流水表
    │   ├── 20260920022000_tb_order_add_pay_columns.sql                # 03 订单支付列
    │   ├── 20260920023000_tb_order_add_settle_credit_columns.sql      # 04 结算/挂账列
    │   ├── 20260921000000_tb_user_role_create.sql                     # 05 员工与角色表（多员工 + 权限）
    │   ├── 20260922000000_tb_oper_log_create.sql                      # 06 操作日志表（审计留痕）
    │   ├── 20260922010000_tb_remember_token_create.sql                # 07 记住我令牌表（7/30 天免登录）
    │   ├── 20260922020000_tb_remember_token_add_ua.sql                # 08 令牌补环境指纹列 ua
    │   ├── 20260923000000_tb_print_job_create.sql                     # 09 本地打印代理任务队列（云部署 + 门店 9100 网络机）
    │   ├── 20260924000000_tb_order_tb_refund_add_columns.sql          # 10 结账前状态快照 + 重复支付退回标记
    │   ├── 20260924000000_tb_print_agent_create.sql                   # 11 本地打印代理身份表
    │   ├── 20260924010000_tb_table_add_table_code.sql                 # 12 桌台点餐稳定码列（补与运行时迁移同义的历史脚本）
    │   ├── 20260925000000_tb_remember_token_add_token_prefix.sql      # 13 令牌补设备识别前缀列 token_prefix
    │   ├── 20260926000000_tb_order_tb_refund_add_report_indexes.sql   # 14 报表查询索引补齐（订单/退款按时间）
    │   ├── 20261007010000_tb_order_item_add_batch_no_client_request_id.sql # 15 明细补「加菜批次」与「幂等键」列
    │   ├── 20261007020000_tb_order_add_unpaid_index.sql               # 16 订单补「未结账单判定」组合索引
    │   ├── 20261008000000_tb_dish_add_sold_out.sql                    # 17 菜品补「售罄」标记
    │   ├── 20261008001000_tb_spec_add_stock.sql                       # 18 规格补「库存」列
    │   ├── 20261008120000_tb_order_item_add_gift_columns.sql          # 19 订单明细赠送（送菜）标记列
    │   ├── 20261008150000_tb_customer_create.sql                      # 20 小程序顾客会员档案表
    │   ├── 20261008151000_tb_order_add_customer_id.sql                # 21 订单补会员归属列
    │   ├── 20261009000000_tb_order_add_pickup_no.sql                  # 22 订单营业日与当日取餐号
    │   ├── 20261009010000_tb_table_add_lock_status.sql                # 23 桌台补「人工可用性状态」列
    │   ├── 20261010000000_tb_dish_add_signature.sql                   # 24 菜品补「招牌菜」标记
    │   └── 20261013000000_tb_config_fix_shop_name_dot.sql             # 25 店铺名统一为「品牌 · 标语」
    └── mysql/                                             # 与 sqlite/ 同名同序，内容按 MySQL 语法适配
        └──（同上 25 个文件）
```

> 下文「归属脚本」中的 incr 编号即上表 01~25 的序号。

> 另有 `tb_print_log`（打印日志）与 `tb_printer` 的飞鹅云字段，因早于 `incr/` 机制建立，
> 老库升级由 `store.alterCols` 在启动时自动补列/建表，`full/` 脚本已包含最终结构
> （`tb_print_log` 的「票据标识 + 票面快照」三列另见 incr 29，`alterCols` 与脚本等价幂等）。

**命名规则**

| 目录 | 规则 | 示例 |
|---|---|---|
| `full/` | **通用命名**：`schema.sql` 为建表脚本；`seed.sql` 为种子 / 存量数据 | `schema.sql`、`seed.sql`（SQLite 库为 `data/dining.db`，MySQL 库为 `dining`） |
| `incr/` | **`日期时间_表名_动作.sql`**，日期时间为 `YYYYMMDDHHMMSS` | `20260920022000_tb_order_add_pay_columns.sql` |

- **第一层按数据库产品分目录**（`sqlite/`、`mysql/`），两套文件名完全一致，
  便于对照排查；切换数据库时执行对应目录下的脚本即可。
- 时间前缀保证**字典序 = 执行顺序**，新增脚本只需取「晚于现有最大时间」的时间戳。
- 常用动作词：`create`（建表/建索引）、`add_xxx_columns`（加列）、`drop_xxx`（删列/删表）、
  `backfill`（历史数据回填）、`fix_xxx`（数据修正）。
- 一次变更涉及多张表时，表名部分用下划线连接（如 `tb_payment_refund_create`）。
- 本仓库从 **26 号（`20261015000000`）起** 才真正落 `incr/` 文件；此前 01~25 号只在
  下表的历史演进清单里出现，实际升级一直依赖后端启动时 `store.alterCols` 自动补列。
  已收录脚本（26 起）的说明见 [`incr/README.md`](./incr/README.md)。

> **脚本是生成的，不是手写的**：`full/` 下 4 个文件由
> `go run ./cmd/gensql` 从 `internal/store` 的表结构与种子数据定义自动生成
> （单一事实来源，避免两库脚本各自漂移）。改结构流程见第六节。
> `incr/` 下的脚本为手工编写的历史演进记录。

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

按文件名时间顺序依次执行。

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
> `create` 系列为纯建表（`IF NOT EXISTS`），可安全重复执行。
>
> ⚠️ `drop_*` 系列（如 30 号删 `tb_order.begin_time` / `end_time`）同样无
> `IF EXISTS` 语法，重复执行报 `no such column` / `check that column/key exists`，
> 属正常现象、可忽略（详见 `incr/README.md` 的幂等性说明）。

## 四、表清单（共 22 张）

| # | 表名 | 说明 | 种子数据 | 归属脚本 |
|---|---|---|---|---|
| 1 | `tb_table` | 桌台 | 8 桌 | full |
| 2 | `tb_category` | 菜品分类 | 6 类 | full |
| 3 | `tb_dish` | 菜品 | 21 道 | full + incr 17/24 |
| 4 | `tb_spec` | 菜品规格 | 32 条 | full + incr 18 |
| 5 | `tb_remark` | 备注选项 | 6 项 | full |
| 6 | `tb_printer` | 打印机（ESC/POS 网络 + 飞鹅云） | 2 台（停用） | full |
| 7 | `tb_config` | 系统配置（键值对） | 39 项 | full |
| 8 | `tb_order` | 订单主表（42 列） | 运行时产生 | full + incr 03/04/10/14/16/21/22/26（`meal_start_time`/`meal_end_time`）/30（删遗留 `begin_time`/`end_time`） |
| 9 | `tb_order_item` | 订单明细 | 运行时产生 | full + incr 15/19 |
| 10 | `tb_order_urge` | 催菜/加菜记录 | 运行时产生 | incr 01 |
| 11 | `tb_payment` | 支付流水（在线支付） | 运行时产生 | incr 02/10/14 |
| 12 | `tb_refund` | 退款流水 | 运行时产生 | incr 02/10/14 |
| 13 | `tb_print_log` | 打印日志（成功/失败留痕、票据标识与票面快照、支持补打与预览回放） | 运行时产生 | full + incr 29 |
| 14 | `tb_role` | 角色（权限点集合） | 4 个内置角色 | incr 05 |
| 15 | `tb_user` | 员工账号 | 首次启动引导超管 | incr 05 |
| 16 | `tb_oper_log` | 操作日志（审计留痕：谁在何时改了什么） | 运行时产生 | incr 06 |
| 17 | `tb_remember_token` | 记住我令牌（7/30 天免登录，过期由后端查库裁决） | 运行时产生 | full + incr 07/08/13 |
| 18 | `tb_print_job` | 本地打印代理任务队列（云后端入队，门店代理取单后直发 9100） | 运行时产生 | incr 09 |
| 19 | `tb_print_agent` | 本地打印代理注册与令牌状态 | 运行时产生 | full + incr 11 |
| 20 | `tb_image` | 图片内容表（菜品图 / 收款码 / 上传图二进制） | 启动时写入出厂图 | full |
| 21 | `tb_day_seq` | 按营业日维护「当日取餐号」的计数器 | 运行时产生 | full + incr 22 |
| 22 | `tb_customer` | 小程序会员（openid 建档，用于「我的订单」与会员归属） | 运行时产生 | full + incr 20/21 |

> `tb_print_job` 与 `tb_print_log` 的分工：前者是「待办」（送达即结案，可定期清理），
> 后者是「台账」（给商家查「这单打了没」，长期保留），两者由 `print_log_id` 关联。
> 不用 `agent` 通道时该表始终为空，行为与升级前完全一致。
> 部署与排障见 [`../../docs/print-agent.md`](../../docs/print-agent.md)。

> `tb_user` **不写种子数据**：由启动引导 `store.EnsureAdminUser()` 按
> `ADMIN_USER` / `ADMIN_PASS`（默认 `admin`/`admin123`）生成首个超级管理员。
> `tb_role` 的 4 个内置角色（admin / manager / cashier / staff）每次启动由
> `store.SyncBuiltinRoles()` 幂等校准；其中 `admin` 的权限**强制恢复为全量**，防止误改锁死。

## 五、索引清单（共 34 个）

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
| `idx_order_customer` | tb_order | 普通（2 列） | 会员「我的订单」按时间倒序分页 |
| `idx_customer_openid` | tb_customer | UNIQUE | 会员按 openid 精确查档；同时兜底「并发首登建档」的幂等性（MySQL 下降级为普通索引，唯一性由应用层保证） |
| `idx_order_item_order` | tb_order_item | 普通 | 明细按订单 |
| `idx_payment_order` | tb_payment | 普通 | 流水按订单号 |
| `idx_payment_channel_trade` | tb_payment | UNIQUE（2 列） | 渠道交易号防重 |
| `idx_refund_no` | tb_refund | UNIQUE | 退款单号唯一 |
| `idx_refund_order` | tb_refund | 普通 | 退款按订单 |
| `idx_refund_update_time` | tb_refund | 普通 | 报表按退款到账时间 |
| `idx_urge_status` | tb_order_urge | 普通（2 列） | 看板待处理催菜 |
| `idx_urge_order` | tb_order_urge | 普通 | 催菜按订单 |
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

> **部分唯一索引的差异**：`idx_table_code` / `idx_user_username`
> 在 SQLite 下是**部分唯一索引**（带 `WHERE` 条件，分别只约束非空桌台码、
> `del_flag='0'` 的未删除行）；MySQL 不支持部分索引，会降级为普通索引。
> 为此代码在应用层补了存在性校验 —— `store.NewUniqueTableCode()`、
> `store.EnsureUsernameAvailable()` / `EnsureRoleKeyAvailable()`，
> 保证两种后端下的唯一性语义一致（员工/角色软删除后同名可复用）。
>
> `idx_role_key` **不再**是部分唯一索引：历史上 MySQL 降级为普通索引后，
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
# 1) 修改 internal/store 里的表结构 / 种子数据定义
# 2) 重新生成两库的 full 脚本
go run ./cmd/gensql
# 3) 手工补一个 incr/ 脚本（sqlite + mysql 各一份），并跑测试
go test ./...
```

`go run ./cmd/gensql -check` 可用于 CI：只校验磁盘脚本是否与 Go 定义一致，不写盘。
`internal/store/sqlgen_test.go` 也会在 `go test` 时自动做这项一致性校验
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
   | `update_time` | `DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP` | `AFTER UPDATE` 触发器（共 22 个） |
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
   落库时间），详见 [`incr/README.md`](./incr/README.md)。
   用餐时长对应的两个落库列是 `tb_order` 的 `meal_start_time` / `meal_end_time`
   （下单写起点、走到终态即 4 已完成 / 5 已取消写终点；撤销结算时终点清空）。
   老库由 `store.alterCols` 自动补列 + `backfillMealTimes()` 按 `create_time` /
   `finish_time` 幂等回填，读侧 `COALESCE` 回落旧列，升级前后统计口径一致。
3. **图片/收款码**：图片内容存于 `tb_image` 表；出厂图片内嵌在后端二进制中、
   首次启动自动写入数据库，`seed.sql` 不含图片数据；老版本升级时旧 `uploads` 目录中的
   存量图片由后端启动时自动导入。业务表仍保存 `/uploads/<文件名>` 虚拟路径（该前缀由
   后端 `store.UploadURLPrefix` 统一定义，启动时会把老库里的 `/picture/` 前缀自动改写）。
4. **软删除**：业务表用 `del_flag`（`'0'` 正常 / `'1'` 删除），查询需带 `del_flag='0'`。
5. **full 与 incr 的关系**：`full/schema.sql` 始终体现**最终结构**（已内联历史上所有
   `ALTER TABLE` 追加的列）；`incr/` 则保留**逐步演进过程**，供老库按序升级。
6. **主密钥别丢**：`tb_config` 中的敏感项是 AES-256-GCM 密文，主密钥在
   `backend/data/master.key`。搬迁数据库时必须把它一并带走，否则密文无法解密。

### MySQL 大图写入注意

`tb_image` 单条 `INSERT` 可达 64MB，MySQL 5.7 服务端默认 `max_allowed_packet=4MB` 会拒绝，
需提前调大，例如：

```sql
SET GLOBAL max_allowed_packet=128M;
```

客户端侧后端默认 DSN 已带 `maxAllowedPacket=134217728`，无需额外配置。

## 八、校验记录

**SQLite（3.53）**

- `full/`：建库结果为 **22 表 / 34 索引**；种子数据 config 39、role 4、table 8、
  category 6、dish 21、spec 32、remark 6、printer 2（合计 118 行，`seed.sql` 不含图片数据）；
  `tb_order` 42 列齐全；重复执行数量不变（建表 `IF NOT EXISTS`、种子 `INSERT OR IGNORE`）。
- `incr/`：在 24 列的旧结构 `tb_order` 上依次执行 01~04 后补齐至 42 列；历史回填正确
  （已支付订单 `settle_type='normal'`、`paid_amount=total_amount-refund_amount`）；
  05（`tb_user_role_create`）在旧库上建出员工/角色两表与 3 个索引。
- **复核方式**：`go run ./cmd/gensql -check` 通过（磁盘脚本与 Go 定义一致）；
  22 表 / 34 索引 / 118 行种子由 `schemaTemplate` 与 `SeedCounts()` 自动核对，
  `go test ./...` 中 `sqlgen_test.go` 会同步做这项一致性校验。
  > 计数以 `gensql -check` 的输出为准；上面的数字只是复核时的快照，加表加列后不必回来改这里。

**MySQL（在 MySQL 协议兼容服务端上实跑）**

- `full/`：建表 + `SHOW CREATE TABLE` 确认 `AUTO_INCREMENT`、`InnoDB`、
  `utf8mb4_general_ci`、`UNIQUE KEY` 均正确。
- `incr/`：在早期结构上依次执行 → `tb_order` 补齐至 37 列；
  回填数值正确（`10000` / `0` / `6000`，即已支付扣退款后的实收金额）。
- 后端以 `DB_DRIVER=mysql` 启动，完成「登录 → 菜单 → 下单 → 加菜 → 催菜 →
  改单重算 → 状态流转 → 结账 → 报表」全链路。
- `scripts/sqlite2mysql.py` 搬运实测：与源库逐表一致，中文无乱码。
- ⚠️ **待补测**：员工/角色两表、打印日志/代理表与图片表加入后（现为 22 表 / 34 索引），尚未在 MySQL 上
  重跑一次全量建库。注意 `tb_user` 的部分唯一索引在 MySQL 下会降级为普通索引，
  唯一性改由应用层保证（见第五节说明）；`tb_role` 的 `idx_role_key` 已升级为
  两库统一的全局唯一索引，不受此问题影响。
