-- ============================================================================
-- 增量迁移 20260920022000：订单表补充「在线支付」列
-- 文件名：20260920022000_tb_order_add_pay_columns.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_pay_columns)
-- 表：tb_order    动作：alter（新增 4 列）
-- ----------------------------------------------------------------------------
-- 背景：订单表新增在线支付相关字段（第三方交易号 / 支付渠道 / 退款金额与时间）。
-- 适用：早期版本、tb_order 表尚无 transaction_id / pay_channel /
--       refund_amount / refund_time 的库。
--
-- ⚠️ 幂等性说明：SQLite 的 ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS。
--      若某列已存在，执行会报 "duplicate column name" 错误 —— 该报错可忽略；
--      请通过 PRAGMA table_info(tb_order) 确认后再决定是否执行本脚本。
-- 执行：sqlite3 dining.db < 20260920022000_tb_order_add_pay_columns.sql
-- ============================================================================

-- 第三方交易号（微信/支付宝返回）
ALTER TABLE tb_order ADD COLUMN transaction_id TEXT DEFAULT '';

-- 支付渠道：wxpay / alipay
ALTER TABLE tb_order ADD COLUMN pay_channel TEXT DEFAULT '';

-- 已退款金额（分）
ALTER TABLE tb_order ADD COLUMN refund_amount INTEGER DEFAULT 0;

-- 退款时间
ALTER TABLE tb_order ADD COLUMN refund_time TEXT;
