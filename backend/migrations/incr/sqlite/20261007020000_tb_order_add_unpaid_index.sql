-- ============================================================================
-- 增量迁移 20261007020000：订单表补充「未结账单判定」组合索引
-- 文件名：20261007020000_tb_order_add_unpaid_index.sql
-- 表：tb_order    动作：create（新增 1 个索引）
-- ----------------------------------------------------------------------------
-- 背景：下单防重由「一桌一进行中单」改为「一桌最多一笔未结账单」，判定与看板
--       按桌分组查询走 table_id + pay_status + order_status 组合条件，缺索引时
--       老库会退化成全表扫描。
-- 幂等性：SQLite 的 CREATE INDEX 支持 IF NOT EXISTS，可安全重复执行。
-- 执行：sqlite3 dining.db < 20261007020000_tb_order_add_unpaid_index.sql
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_order_table_unpaid ON tb_order(table_id, pay_status, order_status);
