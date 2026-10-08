-- ============================================================================
-- 增量迁移 20261008120000：订单赠送（送菜）标记列
-- 文件名：20261008120000_tb_order_item_add_gift_columns.sql
-- 表：tb_order_item / tb_order    动作：alter（新增 3 列）
-- ----------------------------------------------------------------------------
-- 背景：
--   1. tb_order_item.is_gift       —— 是否赠送：0 正常计费 1 赠送（商家改单标记）。
--   2. tb_order_item.gift_reason   —— 赠送原因（可选）。
--   3. tb_order.gift_amount        —— 订单赠送金额合计（分）：赠送菜品按原价计，
--                                     不计入应收金额，用于让利口径统计与小票展示。
-- 语义：赠送菜品 amount 记 0（不进入菜品金额与合计），原价累计到订单 gift_amount。
-- 适用：早期版本、尚无这三列的 SQLite 库。
-- ⚠️ 幂等性：ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS，列已存在时会报
--      "duplicate column name"（可忽略）。
-- 执行：sqlite3 dining.db < 20261008120000_tb_order_item_add_gift_columns.sql
-- ============================================================================

ALTER TABLE tb_order ADD COLUMN gift_amount INTEGER DEFAULT 0;
ALTER TABLE tb_order_item ADD COLUMN is_gift INTEGER DEFAULT 0;
ALTER TABLE tb_order_item ADD COLUMN gift_reason TEXT DEFAULT '';
