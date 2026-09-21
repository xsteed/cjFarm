-- ============================================================================
-- 增量迁移 20260920022000：订单表补充「在线支付」列（MySQL）
-- 文件名：20260920022000_tb_order_add_pay_columns.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_pay_columns)
-- 表：tb_order    动作：alter（新增 4 列）
-- ----------------------------------------------------------------------------
-- 背景：订单表新增在线支付相关字段（第三方交易号 / 支付渠道 / 退款金额与时间）。
-- 适用：早期版本、tb_order 表尚无 transaction_id / pay_channel /
--       refund_amount / refund_time 的 MySQL 库。
--
-- ⚠️ 幂等性说明：MySQL 不支持 ADD COLUMN IF NOT EXISTS（MariaDB 支持）。
--      若某列已存在会报 "Duplicate column name"，该报错可忽略；
--      执行前可用 SHOW COLUMNS FROM tb_order; 自查缺哪些列。
-- 执行：mysql -u dining -p dining < 20260920022000_tb_order_add_pay_columns.sql
-- ============================================================================

SET NAMES utf8mb4;

-- 第三方交易号（微信/支付宝返回）
ALTER TABLE tb_order ADD COLUMN transaction_id VARCHAR(64) DEFAULT '';

-- 支付渠道：wxpay / alipay / offline / 空
ALTER TABLE tb_order ADD COLUMN pay_channel VARCHAR(16) DEFAULT '';

-- 已退款金额（分）
ALTER TABLE tb_order ADD COLUMN refund_amount INT DEFAULT 0;

-- 退款时间
ALTER TABLE tb_order ADD COLUMN refund_time VARCHAR(32);
