-- ============================================================================
-- 增量迁移 20260920021000：新增支付 / 退款流水表（MySQL）
-- 文件名：20260920021000_tb_payment_refund_create.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(create)
-- 表：tb_payment / tb_refund    动作：create（新建表 + 索引）
-- ----------------------------------------------------------------------------
-- 背景：为在线支付（微信 / 支付宝）能力新增 tb_payment（支付流水）与
--       tb_refund（退款流水）两张表及相关索引。
-- 适用：已运行过早期版本、尚无这两张表的 MySQL 库。
-- 幂等：建表使用 IF NOT EXISTS；建索引重复执行会报 'Duplicate key name'，可忽略。
-- 执行：mysql -u dining -p dining < 20260920021000_tb_payment_refund_create.sql
-- ============================================================================

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS tb_payment (
    payment_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    order_no         VARCHAR(64) NOT NULL,
    channel          VARCHAR(16) NOT NULL,      -- wxpay / alipay
    channel_trade_no VARCHAR(64) DEFAULT '',    -- 渠道交易号
    amount           INT DEFAULT 0,             -- 金额（分）
    status           INT DEFAULT 0,             -- 0 待支付 / 1 成功 / 2 失败
    prepay_id        VARCHAR(128) DEFAULT '',
    notify_time      VARCHAR(32),
    create_time      VARCHAR(32),
    update_time      VARCHAR(32)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS tb_refund (
    refund_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    order_id          INT NOT NULL,
    order_no          VARCHAR(64) NOT NULL,
    payment_id        INT NOT NULL,
    refund_no         VARCHAR(64) NOT NULL,     -- 退款单号（唯一）
    channel           VARCHAR(16) NOT NULL,
    channel_refund_no VARCHAR(64) DEFAULT '',
    amount            INT DEFAULT 0,            -- 退款金额（分）
    status            INT DEFAULT 0,            -- 0 处理中 / 1 成功 / 2 失败
    reason            VARCHAR(255) DEFAULT '',
    operator          VARCHAR(64)  DEFAULT '',
    fail_reason       VARCHAR(255) DEFAULT '',
    create_time       VARCHAR(32),
    update_time       VARCHAR(32)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE INDEX        idx_payment_order         ON tb_payment(order_no);
CREATE UNIQUE INDEX idx_payment_channel_trade ON tb_payment(channel, channel_trade_no);
CREATE UNIQUE INDEX idx_refund_no             ON tb_refund(refund_no);
CREATE INDEX        idx_refund_order          ON tb_refund(order_id);
