-- ============================================================================
-- 增量迁移 20261008151000：订单表增加会员归属列
-- 文件名：20261008151000_tb_order_add_customer_id.sql
-- 表：tb_order    动作：alter（新增 1 列 + 1 个索引）
-- ----------------------------------------------------------------------------
-- 背景：顾客会员体系（见 20261008150000_tb_customer_create.sql）。
--   会员在小程序下单时,订单需要与会员档案关联,才能提供「我的订单」(跨桌历史订单)。
-- 语义：
--   customer_id = 0 表示匿名单：所有历史订单、以及未登录顾客的新订单都是 0。
--   归属只认服务端从顾客令牌解析出的值,请求体里的任何字段都不可信(防止伪造归属)。
--   历史数据不回填 —— 匿名订单永远匿名。
--   索引 (customer_id, create_time) 服务「我的订单」按时间倒序分页。
-- 适用：tb_order 尚无 customer_id 列的 SQLite 库。
-- ⚠️ 幂等性：ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS,列已存在时会报
--      "duplicate column name"（可忽略）。
-- 执行：sqlite3 dining.db < 20261008151000_tb_order_add_customer_id.sql
-- ============================================================================

ALTER TABLE tb_order ADD COLUMN customer_id INTEGER DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_order_customer ON tb_order(customer_id, create_time);
