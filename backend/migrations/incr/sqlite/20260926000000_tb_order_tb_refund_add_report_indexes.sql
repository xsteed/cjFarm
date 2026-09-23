-- ============================================================================
-- 老库升级脚本(SQLite):报表查询索引补齐
-- 文件名:20260926000000_tb_order_tb_refund_add_report_indexes.sql
-- ----------------------------------------------------------------------------
-- 变更内容:
--   1. tb_order 增加 pay_time 索引(正常收款按「到账时间」归属)
--   2. tb_order 增加 credit_settle_time 索引(挂账核销按「核销时间」归属)
--   3. tb_refund 增加 update_time 索引(退款按「退款到账时间」归属并扣减)
--
-- 背景:报表的营业额口径按「钱到账的时间」做区间过滤(见 dao.RangeAmount),
-- 逐日/区间统计都会走 pay_time / credit_settle_time / update_time 这三个条件,
-- 缺索引时老库报表会退化成全表扫描。
--
-- 幂等性:SQLite 的 CREATE INDEX 支持 IF NOT EXISTS,可安全重复执行。
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_order_pay_time ON tb_order(pay_time);
CREATE INDEX IF NOT EXISTS idx_order_credit_settle_time ON tb_order(credit_settle_time);
CREATE INDEX IF NOT EXISTS idx_refund_update_time ON tb_refund(update_time);
