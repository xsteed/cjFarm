-- ============================================================================
-- 扫码点餐管理系统 — 全量建表脚本(MySQL 8.0 / 5.7,MariaDB 10.3+)
-- 文件名:schema.sql —— 全量建表脚本(通用命名)
-- ----------------------------------------------------------------------------
-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的表结构定义生成,
-- 请勿手工修改;需要变更表结构时改 Go 定义后重新生成。
--
-- 前置步骤(只需执行一次):
--   CREATE DATABASE IF NOT EXISTS dining
--     DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
--   CREATE USER IF NOT EXISTS 'dining'@'%' IDENTIFIED BY '改成你的强密码';
--   GRANT ALL PRIVILEGES ON dining.* TO 'dining'@'%';
--   FLUSH PRIVILEGES;
--
-- 执行方式:mysql -u dining -p dining < schema.sql
--
-- 注意事项:
--   1. MySQL 不支持 CREATE INDEX IF NOT EXISTS;重复执行时建表语句会被
--      IF NOT EXISTS 跳过,但建索引语句会报 'Duplicate key name',
--      该错误可安全忽略(可用 `mysql --force` 继续执行)。
--   2. 表引擎 InnoDB、字符集 utf8mb4,需在 MySQL 5.7+ 上执行。
--   3. 与 SQLite 版的唯一语义差异:MySQL 会强制校验 VARCHAR 长度,
--      SQLite 不校验,导入超长数据时 MySQL 会报 'Data too long'。
--
-- 金额约定:所有金额字段以「分」为单位存 INT,API 层由 po.ToYuan 转元。
-- ============================================================================

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS tb_table (
        table_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        table_no    VARCHAR(32)  NOT NULL,
        table_name  VARCHAR(64)  NOT NULL,
        capacity    INTEGER      DEFAULT 0,
        status      INTEGER      DEFAULT 0,
        sort_order  INTEGER      DEFAULT 0,
        del_flag    VARCHAR(2)   DEFAULT '0',
        create_by   VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32),
        update_by   VARCHAR(64)  DEFAULT '',
        update_time VARCHAR(32),
        remark      VARCHAR(500),
        table_code  VARCHAR(16)  DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_category (
        category_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        category_name VARCHAR(64) NOT NULL,
        sort_order    INTEGER     DEFAULT 0,
        del_flag      VARCHAR(2)  DEFAULT '0',
        create_time   VARCHAR(32),
        update_time   VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_dish (
        dish_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        category_id INTEGER      NOT NULL,
        dish_name   VARCHAR(128) NOT NULL,
        dish_image  VARCHAR(255) DEFAULT '',
        description VARCHAR(500) DEFAULT '',
        status      INTEGER      DEFAULT 1,
        sort_order  INTEGER      DEFAULT 0,
        del_flag    VARCHAR(2)   DEFAULT '0',
        create_by   VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32),
        update_by   VARCHAR(64)  DEFAULT '',
        update_time VARCHAR(32),
        remark      VARCHAR(500)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_spec (
        spec_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        dish_id   INTEGER      NOT NULL,
        spec_name VARCHAR(64)  NOT NULL,
        price     INTEGER      DEFAULT 0
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_remark (
        remark_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        option_name VARCHAR(64) NOT NULL,
        sort_order  INTEGER     DEFAULT 0,
        del_flag    VARCHAR(2)  DEFAULT '0',
        create_time VARCHAR(32),
        update_time VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_printer (
        printer_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        printer_name VARCHAR(64) NOT NULL,
        printer_type INTEGER     DEFAULT 1,
        provider     VARCHAR(16) DEFAULT 'tcp',
        ip           VARCHAR(64) DEFAULT '',
        port         INTEGER     DEFAULT 9100,
        feie_sn      VARCHAR(64) DEFAULT '',
        paper_width  INTEGER     DEFAULT 48,
        copies       INTEGER     DEFAULT 1,
        category_ids VARCHAR(255) DEFAULT '',
        status       INTEGER     DEFAULT 1,
        del_flag     VARCHAR(2)  DEFAULT '0',
        create_time  VARCHAR(32),
        update_time  VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_print_log (
        print_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_id     INTEGER      DEFAULT 0,
        order_no     VARCHAR(64)  DEFAULT '',
        table_no     VARCHAR(32)  DEFAULT '',
        table_name   VARCHAR(64)  DEFAULT '',
        printer_id   INTEGER      DEFAULT 0,
        printer_name VARCHAR(64)  DEFAULT '',
        printer_type INTEGER      DEFAULT 1,
        provider     VARCHAR(16)  DEFAULT 'tcp',
        doc_type     VARCHAR(16)  DEFAULT '',
        copies       INTEGER      DEFAULT 1,
        status       INTEGER      DEFAULT 0,
        remote_id    VARCHAR(128) DEFAULT '',
        detail       VARCHAR(500) DEFAULT '',
        trigger_by   VARCHAR(16)  DEFAULT '',
        operator     VARCHAR(64)  DEFAULT '',
        cost_ms      INTEGER      DEFAULT 0,
        create_time  VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_print_job (
        job_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        printer_id   INTEGER       DEFAULT 0,
        printer_name VARCHAR(64)   DEFAULT '',
        printer_type INTEGER       DEFAULT 1,
        ip           VARCHAR(64)   DEFAULT '',
        port         INTEGER       DEFAULT 9100,
        doc_type     VARCHAR(16)   DEFAULT '',
        order_id     INTEGER       DEFAULT 0,
        order_no     VARCHAR(64)   DEFAULT '',
        table_no     VARCHAR(32)   DEFAULT '',
        copies       INTEGER       DEFAULT 1,
        print_log_id INTEGER       DEFAULT 0,
        delivery_id  VARCHAR(36)   DEFAULT '',
        payload      VARCHAR(4000) DEFAULT '',
        status       INTEGER       DEFAULT 0,
        attempts     INTEGER       DEFAULT 0,
        last_error   VARCHAR(500)  DEFAULT '',
        claimed_by   VARCHAR(64)   DEFAULT '',
        claim_time   VARCHAR(32),
        next_try_time VARCHAR(32),
        trigger_by   VARCHAR(16)   DEFAULT '',
        operator     VARCHAR(64)   DEFAULT '',
        create_time  VARCHAR(32),
        done_time    VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_print_agent (
        agent_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        agent_name  VARCHAR(64)  DEFAULT '',
        token_hash  VARCHAR(64)  DEFAULT '',
        token_hint  VARCHAR(8)   DEFAULT '',
        printer_ids VARCHAR(255) DEFAULT '',
        status      INTEGER      DEFAULT 1,
        last_seen   VARCHAR(32),
        last_report VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32),
        update_time VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_oper_log (
        log_id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        module        VARCHAR(32)   DEFAULT '',
        business_type VARCHAR(16)   DEFAULT 'other',
        action        VARCHAR(64)   DEFAULT '',
        method        VARCHAR(255)  DEFAULT '',
        request_url   VARCHAR(255)  DEFAULT '',
        operator_id   INTEGER       DEFAULT 0,
        operator      VARCHAR(64)   DEFAULT '',
        operator_role VARCHAR(64)   DEFAULT '',
        oper_ip       VARCHAR(64)   DEFAULT '',
        target_type   VARCHAR(32)   DEFAULT '',
        target_id     VARCHAR(64)   DEFAULT '',
        oper_param    VARCHAR(2000) DEFAULT '',
        detail        VARCHAR(500)  DEFAULT '',
        status        INTEGER       DEFAULT 0,
        error_msg     VARCHAR(500)  DEFAULT '',
        cost_ms       INTEGER       DEFAULT 0,
        create_time   VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_config (
        cfg_key   VARCHAR(64)   PRIMARY KEY,
        cfg_value VARCHAR(4000) DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_order (
        order_id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_no        VARCHAR(64)  NOT NULL,
        table_id        INTEGER,
        table_no        VARCHAR(32),
        table_name      VARCHAR(64),
        person_count    INTEGER      DEFAULT 1,
        order_status    INTEGER      DEFAULT 1,
        dish_amount     INTEGER      DEFAULT 0,
        seat_fee        INTEGER      DEFAULT 0,
        discount_amount INTEGER      DEFAULT 0,
        total_amount    INTEGER      DEFAULT 0,
        pay_status      INTEGER      DEFAULT 0,
        pay_type        VARCHAR(16),
        pay_time        VARCHAR(32),
        transaction_id  VARCHAR(64)  DEFAULT '',
        pay_channel     VARCHAR(16)  DEFAULT '',
        refund_amount   INTEGER      DEFAULT 0,
        refund_time     VARCHAR(32),
        finish_time     VARCHAR(32),
        order_remark    VARCHAR(500) DEFAULT '',
        cancel_reason   VARCHAR(255) DEFAULT '',
        begin_time      VARCHAR(32),
        end_time        VARCHAR(32),
        create_by       VARCHAR(64)  DEFAULT '',
        create_time     VARCHAR(32),
        update_by       VARCHAR(64)  DEFAULT '',
        update_time     VARCHAR(32),
        remark          VARCHAR(500),
        settle_type     VARCHAR(16)  DEFAULT 'normal',
        settle_time     VARCHAR(32),
        settle_operator VARCHAR(64)  DEFAULT '',
        settle_remark   VARCHAR(255) DEFAULT '',
        credit_status   INTEGER      DEFAULT 0,
        credit_amount   INTEGER      DEFAULT 0,
        credit_settle_time VARCHAR(32),
        credit_settle_by   VARCHAR(64) DEFAULT '',
        paid_amount     INTEGER      DEFAULT 0,
        -- 结账时(状态被置 4 前)的订单状态快照:撤销结算时恢复到结账前的真实状态,
        -- 而不是无条件回到 3(订单可能在 1/2 状态被提前结账,撤销后应回到 1/2 继续制作流程)。
        -- 0 表示历史数据未记录(撤销时回退为 3)。
        pre_settle_status INTEGER     DEFAULT 0
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_order_item (
        item_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_id  INTEGER      NOT NULL,
        dish_id   INTEGER,
        dish_name VARCHAR(128),
        spec_id   INTEGER,
        spec_name VARCHAR(64),
        price     INTEGER      DEFAULT 0,
        quantity  INTEGER      DEFAULT 1,
        amount    INTEGER      DEFAULT 0,
        remark    VARCHAR(255) DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_order_urge (
        urge_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_id    INTEGER      NOT NULL,
        order_no    VARCHAR(64)  NOT NULL,
        table_id    INTEGER      NOT NULL,
        table_no    VARCHAR(32)  DEFAULT '',
        table_name  VARCHAR(64)  DEFAULT '',
        urge_type   VARCHAR(16)  DEFAULT 'urge',
        status      INTEGER      DEFAULT 0,
        remark      VARCHAR(255) DEFAULT '',
        create_time VARCHAR(32),
        handle_time VARCHAR(32),
        handle_by   VARCHAR(64)  DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_payment (
        payment_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_no         VARCHAR(64)  NOT NULL,
        channel          VARCHAR(16)  NOT NULL,
        channel_trade_no VARCHAR(64)  DEFAULT '',
        amount           INTEGER      DEFAULT 0,
        status           INTEGER      DEFAULT 0,
        prepay_id        VARCHAR(128) DEFAULT '',
        notify_time      VARCHAR(32),
        create_time      VARCHAR(32),
        update_time      VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_refund (
        refund_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_id          INTEGER      NOT NULL,
        order_no          VARCHAR(64)  NOT NULL,
        payment_id        INTEGER      NOT NULL,
        refund_no         VARCHAR(64)  NOT NULL,
        channel           VARCHAR(16)  NOT NULL,
        channel_refund_no VARCHAR(64)  DEFAULT '',
        amount            INTEGER      DEFAULT 0,
        status            INTEGER      DEFAULT 0,
        reason            VARCHAR(255) DEFAULT '',
        operator          VARCHAR(64)  DEFAULT '',
        fail_reason       VARCHAR(255) DEFAULT '',
        create_time       VARCHAR(32),
        update_time       VARCHAR(32),
        -- 1 = 重复支付自动退回(顾客在两个渠道各付了一笔,后到的一笔原路退回)。
        -- 这类退款的金额从未计入订单营收(paid_amount),因此不计入订单退款额度、
        -- 也不扣减订单实收 —— 与普通退款(SumRefunded/applyRefundSuccess)严格区分。
        is_duplicate      INTEGER      DEFAULT 0
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_role (
        role_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        role_key    VARCHAR(32)   NOT NULL,
        role_name   VARCHAR(64)   NOT NULL,
        perms       VARCHAR(4000) DEFAULT '',
        data_scope  VARCHAR(16)   DEFAULT 'all',
        is_builtin  INTEGER       DEFAULT 0,
        sort_order  INTEGER       DEFAULT 0,
        status      INTEGER       DEFAULT 1,
        del_flag    VARCHAR(2)    DEFAULT '0',
        create_time VARCHAR(32),
        update_time VARCHAR(32),
        remark      VARCHAR(500)  DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_user (
        user_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        username        VARCHAR(64)  NOT NULL,
        password_hash   VARCHAR(255) DEFAULT '',
        real_name       VARCHAR(64)  DEFAULT '',
        role_id         INTEGER      NOT NULL DEFAULT 0,
        phone           VARCHAR(32)  DEFAULT '',
        status          INTEGER      DEFAULT 1,
        last_login_time VARCHAR(32),
        last_login_ip   VARCHAR(64)  DEFAULT '',
        login_count     INTEGER      DEFAULT 0,
        pwd_update_time VARCHAR(32),
        token_version   INTEGER      DEFAULT 0,
        del_flag        VARCHAR(2)   DEFAULT '0',
        create_by       VARCHAR(64)  DEFAULT '',
        create_time     VARCHAR(32),
        update_by       VARCHAR(64)  DEFAULT '',
        update_time     VARCHAR(32),
        remark          VARCHAR(500) DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_remember_token (
        token_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        user_id        INTEGER      NOT NULL,
        token          VARCHAR(64)  NOT NULL,
        token_prefix   VARCHAR(8)   DEFAULT '',
        expire_time    VARCHAR(32)  NOT NULL,
        create_time    VARCHAR(32),
        last_used_time VARCHAR(32),
        ua             VARCHAR(255) DEFAULT ''
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_image (
        img_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        img_name     VARCHAR(128) NOT NULL,
        content_type VARCHAR(32)  DEFAULT '',
        img_data     LONGBLOB,
        file_size    INTEGER      DEFAULT 0,
        create_time  VARCHAR(32)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE UNIQUE INDEX idx_order_no ON tb_order(order_no);
CREATE INDEX idx_order_table ON tb_order(table_id);
CREATE INDEX idx_order_status ON tb_order(order_status);
CREATE INDEX idx_order_create_time ON tb_order(create_time);
CREATE INDEX idx_order_pay_time ON tb_order(pay_time);
CREATE INDEX idx_order_credit_settle_time ON tb_order(credit_settle_time);
CREATE INDEX idx_order_item_order ON tb_order_item(order_id);
CREATE INDEX idx_payment_order ON tb_payment(order_no);
CREATE UNIQUE INDEX idx_payment_channel_trade ON tb_payment(channel, channel_trade_no);
CREATE UNIQUE INDEX idx_refund_no ON tb_refund(refund_no);
CREATE INDEX idx_refund_order ON tb_refund(order_id);
CREATE INDEX idx_refund_update_time ON tb_refund(update_time);
CREATE INDEX idx_urge_status ON tb_order_urge(status, create_time);
CREATE INDEX idx_urge_order ON tb_order_urge(order_id);
CREATE INDEX idx_print_log_order ON tb_print_log(order_id);
CREATE INDEX idx_print_log_time ON tb_print_log(create_time);
CREATE INDEX idx_print_log_status ON tb_print_log(status, create_time);
CREATE INDEX idx_print_job_pick ON tb_print_job(status, next_try_time, job_id);
CREATE INDEX idx_print_job_printer ON tb_print_job(printer_id, status);
CREATE INDEX idx_print_agent_token ON tb_print_agent(token_hash, status);
CREATE INDEX idx_table_code ON tb_table(table_code);
CREATE INDEX idx_user_username ON tb_user(username);
CREATE INDEX idx_user_role ON tb_user(role_id);
CREATE INDEX idx_role_key ON tb_role(role_key);
CREATE INDEX idx_operlog_time ON tb_oper_log(create_time);
CREATE INDEX idx_operlog_operator ON tb_oper_log(operator_id, create_time);
CREATE INDEX idx_operlog_target ON tb_oper_log(target_type, target_id);
CREATE INDEX idx_operlog_status ON tb_oper_log(status, create_time);
CREATE UNIQUE INDEX idx_remember_token ON tb_remember_token(token);
CREATE UNIQUE INDEX idx_image_name ON tb_image(img_name);
