-- ============================================================================
-- 增量迁移 20260920023000：订单表补充「免单 / 挂账(记账)」列 + 历史数据回填（MySQL）
-- 文件名：20260920023000_tb_order_add_settle_credit_columns.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_settle_credit_columns)
-- 表：tb_order    动作：alter（新增 9 列）+ backfill（历史数据回填）
-- ----------------------------------------------------------------------------
-- 背景：为结算能力新增：
--   · settle_*  结算方式（normal 正常收款 / free 免单 / credit 挂账）
--   · credit_*  挂账状态（0 非挂账 / 1 待收款 / 2 已结清）
--   · paid_amount 实收金额（分）
-- 适用：早期版本、tb_order 表尚无上述列的 MySQL 库。
--
-- ⚠️ 幂等性说明：MySQL 不支持 ADD COLUMN IF NOT EXISTS，列已存在时会报
--      "Duplicate column name"（可忽略）。回填语句（UPDATE）可安全重复执行。
-- 执行：mysql -u dining -p dining < 20260920023000_tb_order_add_settle_credit_columns.sql
-- ============================================================================

SET NAMES utf8mb4;

-- ---------- 结算方式 ----------
ALTER TABLE tb_order ADD COLUMN settle_type     VARCHAR(16) DEFAULT 'normal';   -- normal / free / credit
ALTER TABLE tb_order ADD COLUMN settle_time     VARCHAR(32);                    -- 结算时间
ALTER TABLE tb_order ADD COLUMN settle_operator VARCHAR(64) DEFAULT '';         -- 结算操作人
ALTER TABLE tb_order ADD COLUMN settle_remark   VARCHAR(255) DEFAULT '';        -- 结算备注

-- ---------- 挂账（记账） ----------
ALTER TABLE tb_order ADD COLUMN credit_status      INT DEFAULT 0;               -- 0 非挂账 / 1 待收款 / 2 已结清
ALTER TABLE tb_order ADD COLUMN credit_amount      INT DEFAULT 0;               -- 挂账金额（分）
ALTER TABLE tb_order ADD COLUMN credit_settle_time VARCHAR(32);                 -- 挂账结清时间
ALTER TABLE tb_order ADD COLUMN credit_settle_by   VARCHAR(64) DEFAULT '';      -- 挂账结清人

-- ---------- 实收金额 ----------
ALTER TABLE tb_order ADD COLUMN paid_amount INT DEFAULT 0;                      -- 实收金额（分）

-- ============================================================================
-- 历史数据回填（可重复执行）
-- ============================================================================

-- 1) 老订单没有实收金额：按「已支付且非挂账，即全额实收（扣退款）」补齐，保证报表口径连续。
UPDATE tb_order
SET paid_amount = total_amount - COALESCE(refund_amount, 0)
WHERE pay_status = 1
  AND (paid_amount IS NULL OR paid_amount = 0)
  AND (settle_type IS NULL OR settle_type = 'normal');

-- 2) 空结算方式统一初始化为 normal。
UPDATE tb_order
SET settle_type = 'normal'
WHERE settle_type IS NULL OR settle_type = '';
