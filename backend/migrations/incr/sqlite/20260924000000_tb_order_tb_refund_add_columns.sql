-- ============================================================================
-- 增量迁移 20260924000000：订单表「结账前状态快照」+ 退款表「重复支付退回标记」列
-- 文件名：20260924000000_tb_order_tb_refund_add_columns.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_columns)
-- 表：tb_order / tb_refund    动作：alter（各新增 1 列）
-- ----------------------------------------------------------------------------
-- 背景：
--   · tb_order.pre_settle_status —— 结账时(状态被置 4 前)的订单状态快照，
--     撤销结算时恢复到结账前的真实状态。此前撤销结算硬编码回到 3(已上齐)，
--     订单若在 1/2 状态被提前结账，撤销后会凭空"跳级"到 3。
--   · tb_refund.is_duplicate —— 重复支付自动原路退回的退款单标记
--     (顾客在微信/支付宝各付了一笔，后到的一笔自动退回)。这类退款的金额
--     从未计入订单营收，因此不占订单退款额度、不扣订单实收，
--     与普通退款的统计口径(SumRefunded)严格区分。
-- 适用：早期版本、上述两表尚无对应列的 SQLite 库。
--
-- ⚠️ 幂等性说明：ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS，列已存在时会报
--      "duplicate column name"（可忽略）。默认值 0 即业务空值，无需回填。
-- 执行：sqlite3 dining.db < 20260924000000_tb_order_tb_refund_add_columns.sql
-- ============================================================================

-- 结账前状态快照：0=历史数据未记录(撤销结算时回退为 3)
ALTER TABLE tb_order ADD COLUMN pre_settle_status INTEGER DEFAULT 0;

-- 重复支付自动退回标记：0=普通退款 / 1=重复支付退回
ALTER TABLE tb_refund ADD COLUMN is_duplicate INTEGER DEFAULT 0;
