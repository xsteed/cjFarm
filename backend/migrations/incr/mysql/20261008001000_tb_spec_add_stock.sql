-- ============================================================================
-- 增量迁移 20261008001000：规格表补充「库存」列（MySQL）
-- 文件名：20261008001000_tb_spec_add_stock.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_stock)
-- 表：tb_spec    动作：alter（新增 1 列）
-- ----------------------------------------------------------------------------
-- 背景：自动售罄按「规格」计数 —— 下单/加菜扣减，取消未支付订单回补，
--       扣到 0 即售罄；-1 表示不限量（默认）。与菜品级人工售罄 tb_dish.sold_out 正交。
-- 适用：早期版本、tb_spec 表尚无 stock 列的库。
--
-- ⚠️ 幂等性说明：MySQL 不支持 ADD COLUMN IF NOT EXISTS，列已存在时会报
--      "Duplicate column name"（可忽略）。
-- 执行：mysql -u dining -p dining < 20261008001000_tb_spec_add_stock.sql
-- ============================================================================

SET NAMES utf8mb4;

ALTER TABLE tb_spec ADD COLUMN stock INT DEFAULT -1 COMMENT '-1 不限量，>=0 剩余库存';
