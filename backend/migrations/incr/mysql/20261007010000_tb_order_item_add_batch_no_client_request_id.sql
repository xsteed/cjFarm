-- ============================================================================
-- 增量迁移 20261007010000：订单明细补充「加菜批次」与「幂等键」列
-- 文件名：20261007010000_tb_order_item_add_batch_no_client_request_id.sql
-- 表：tb_order_item    动作：alter（新增 2 列）
-- ----------------------------------------------------------------------------
-- 背景：
--   1. batch_no          —— 菜品明细区分「首次下单 / 第 N 次加菜」；首次=1，每次加菜 +1。
--   2. client_request_id —— 下单/加菜幂等键，同一次提交的所有明细共享同一请求号，
--                           用于双击/超时重试去重（冗余存储于明细行，不另建表）。
-- 适用：早期版本、tb_order_item 尚无这两列的 MySQL 库。
-- ⚠️ 幂等性：ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS，列已存在时会报
--      "Duplicate column name"（可忽略）。
-- 执行：mysql -u dining -p dining < 20261007010000_tb_order_item_add_batch_no_client_request_id.sql
-- ============================================================================

ALTER TABLE tb_order_item ADD COLUMN batch_no INTEGER DEFAULT 1;
ALTER TABLE tb_order_item ADD COLUMN client_request_id VARCHAR(64) DEFAULT '';
