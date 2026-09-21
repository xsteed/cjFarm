-- ============================================================================
-- 增量迁移 20260920021000：新增支付 / 退款流水表
-- 文件名：20260920021000_tb_payment_refund_create.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(create)
-- 表：tb_payment、tb_refund    动作：create（新建表 + 索引）
-- ----------------------------------------------------------------------------
-- 背景：为在线支付（微信 / 支付宝）能力新增 tb_payment（支付流水）与
--       tb_refund（退款流水）两张表及相关索引。
-- 适用：已运行过早期版本、尚无这两张表的库。
-- 幂等：本脚本使用 IF NOT EXISTS，可安全重复执行。
-- 执行：sqlite3 dining.db < 20260920021000_tb_payment_refund_create.sql
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_payment (
    payment_id       INTEGER PRIMARY KEY AUTOINCREMENT,
    order_no         TEXT NOT NULL,
    channel          TEXT NOT NULL,         -- wxpay / alipay
    channel_trade_no TEXT DEFAULT '',       -- 渠道交易号
    amount           INTEGER DEFAULT 0,     -- 金额（分）
    status           INTEGER DEFAULT 0,     -- 0 待支付 / 1 成功 / 2 失败
    prepay_id        TEXT DEFAULT '',
    notify_time      TEXT,
    create_time      TEXT,
    update_time      TEXT
);

CREATE TABLE IF NOT EXISTS tb_refund (
    refund_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id          INTEGER NOT NULL,
    order_no          TEXT NOT NULL,
    payment_id        INTEGER NOT NULL,
    refund_no         TEXT NOT NULL,        -- 退款单号（唯一）
    channel           TEXT NOT NULL,
    channel_refund_no TEXT DEFAULT '',
    amount            INTEGER DEFAULT 0,    -- 退款金额（分）
    status            INTEGER DEFAULT 0,    -- 0 处理中 / 1 成功 / 2 失败
    reason            TEXT DEFAULT '',
    operator          TEXT DEFAULT '',
    fail_reason       TEXT DEFAULT '',
    create_time       TEXT,
    update_time       TEXT
);

CREATE INDEX        IF NOT EXISTS idx_payment_order         ON tb_payment(order_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_channel_trade ON tb_payment(channel, channel_trade_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_refund_no             ON tb_refund(refund_no);
CREATE INDEX        IF NOT EXISTS idx_refund_order          ON tb_refund(order_id);
