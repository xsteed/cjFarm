# 增量脚本如何验证

`incr/` 下的脚本按 **文件名时间前缀** 的字典序执行（见上级 README 第三节）。
本目录目前收录以下脚本：

| 序号 | 文件 | 作用 |
|---|---|---|
| 26 | `20261015000000_tb_order_add_meal_time_columns.sql` | 订单用餐起止时间落库（`meal_start_time` / `meal_end_time`）并回填历史行 |
| 27 | `20261015010000_tables_add_timestamp_columns.sql` | 11 张缺 `create_time` / `update_time` 的表补齐这两个列 |
| 28 | `20261015020000_times_*_autofill*.sql` | 时间列改由数据库自动维护：MySQL 换 `DATETIME` + `DEFAULT`/`ON UPDATE`；SQLite 建 22 个 `update_time` 触发器 |
| 29 | `20261016000000_tb_print_log_add_snapshot_columns.sql` | 打印台账补「票据标识 + 票面快照」三列（`label` / `content` / `paper_width`），老记录留空、读侧回落重算 |
| 30 | `20261017000000_tb_order_drop_legacy_time_columns.sql` | 删除 `tb_order` 遗留的 `begin_time` / `end_time` 两列（从未写入，已被 `meal_start_time` / `meal_end_time` 取代） |
| 31 | `20261017010000_tb_visit_log_visitor_daily_create.sql` | 站点访问统计三张表：`tb_visit_log` / `tb_visitor` / `tb_visit_daily` |
| 32 | `20261018000000_tb_customer_identity_create.sql` | 顾客会员身份表 `tb_customer_identity`（账号与登录方式解耦），并把存量 `tb_customer.openid` 搬迁为 `provider='wx_mp'` 的身份记录 |
| 33 | `20261018010000_tb_customer_drop_openid.sql` | 退役 `tb_customer.openid`：再搬一次（兼容跳过 32 号的库）→ 删唯一索引 → 删列。**删列后非微信登录才能建档** |
| 34 | `20261018020000_tb_order_add_viewed_at.sql` | 订单补「商家首次操作时间」`viewed_at`（NULL = 新单未操作过，列表据此打「新」角标；仅查看不算），存量订单回填为已操作 |

> 32/33 两号是配套的：32 建身份表并首次搬迁，33 删掉 `openid` 列。
> 只跑 32 不跑 33 也能用（P0 状态），但第二个登录方式仍会被 `openid` 的唯一索引挡住。
> 常规升级**不需要手工执行**：后端启动时的 `store.migrateCustomerOpenid` 做同等且幂等的迁移。

> 更早的 01~25 号脚本仅存在于 README 的历史演进记录里，本仓库实际未收录这些文件；
> 老库补列主要由后端启动时 `store.alterCols` 自动完成（等价且幂等），
> 本目录从 26 号起开始落人工可审计的 SQL 记录。

## 填写口径（全局约定）

| 字段 | 时机 | 由谁产生 |
|---|---|---|
| `create_time` | **仅 INSERT** | 数据库默认值（MySQL `DEFAULT CURRENT_TIMESTAMP` / SQLite `DEFAULT (datetime('now','localtime'))`）；应用层也会显式写一份，结果一致 |
| `update_time` | **仅 UPDATE** | 数据库自动推进（MySQL `ON UPDATE CURRENT_TIMESTAMP` / SQLite `trg_<表>_touch` 触发器）；INSERT 时留 NULL |

「创建后从未改过」与「刚创建」因此可区分（前者 `update_time` 为 NULL）。
触发器与 `ON UPDATE` 都不覆盖应用显式写入的值，运维手工 SQL 不写 `update_time` 时
数据库会补上 —— 这是加列即可依赖的兜底能力。

例外：`tb_config` 的写入走 REPLACE（整行替换），写 `create_time` 会把首次落库时间冲掉，
因此只在种子语句里写一次 `create_time`，之后改配置只推进 `update_time`。

## MySQL 侧执行前必读（28 号脚本）

1. 脚本会先把空串与 `'0000-00-00'` 历史零值清洗成 NULL —— 严格模式下 `MODIFY COLUMN`
   遇到这类非法值会直接失败；清洗只把「没有值」规范化，不改动真实时间。
2. `CURRENT_TIMESTAMP` 取**会话时区**，执行前请确认 `time_zone` 与部署时区一致
   （如 `SET time_zone='+08:00';`），否则会出现「应用写本地时间、默认值写 UTC」两套时间。
3. 大表（`tb_order` / `tb_oper_log`）的 `MODIFY` 会重建表，请在业务低峰执行。
4. SQLite 侧不需要换类型（保持 `VARCHAR(32)`），只建触发器 —— 老库无法通过 ALTER
   补列默认值，`create_time` 继续由应用层写入。

## 幂等性

ALTER TABLE ADD COLUMN 在两种数据库上都不支持 `IF NOT EXISTS`：列已存在时会报
`duplicate column name`（SQLite）/ `Duplicate column name`（MySQL），属**可忽略的正常现象**，
与启动补列被跳过是同一回事。回填 SQL 一律带 `IS NULL` 守卫，重复执行零变更。

同理，`drop_*` 系列（30 号删 `tb_order.begin_time` / `end_time`）两种数据库也都没有
`DROP COLUMN IF EXISTS`，**重复执行会报 `no such column`（SQLite）/
`check that column/key exists`（MySQL），同样可忽略**。执行前自查：
SQLite `PRAGMA table_info(tb_order);` / MySQL `SHOW COLUMNS FROM tb_order;`。
