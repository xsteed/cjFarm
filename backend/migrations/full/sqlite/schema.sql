-- ============================================================================
-- 扫码点餐管理系统 — 全量建表脚本(SQLite)
-- 文件名:schema.sql —— 全量建表脚本(通用命名)
-- ----------------------------------------------------------------------------
-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的表结构定义生成,
-- 请勿手工修改;需要变更表结构时改 Go 定义后重新生成。
--
-- 适用场景:全新部署 / 从零建库,执行后得到与后端代码完全一致的库结构
--           (已包含历史上通过 ALTER TABLE 追加的所有列)。
-- 执行方式:sqlite3 dining.db < schema.sql
-- 幂等性  :全部使用 IF NOT EXISTS,可重复执行。
--
-- 金额约定:所有金额字段以「分」为单位存 INTEGER,API 层由 model.ToYuan 转元。
-- ============================================================================

PRAGMA foreign_keys = OFF;

CREATE TABLE IF NOT EXISTS tb_table (
        table_id    INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_category (
        category_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        category_name VARCHAR(64) NOT NULL,
        sort_order    INTEGER     DEFAULT 0,
        del_flag      VARCHAR(2)  DEFAULT '0',
        create_time   VARCHAR(32),
        update_time   VARCHAR(32)
    );
CREATE TABLE IF NOT EXISTS tb_dish (
        dish_id     INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_spec (
        spec_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        dish_id   INTEGER      NOT NULL,
        spec_name VARCHAR(64)  NOT NULL,
        price     INTEGER      DEFAULT 0
    );
CREATE TABLE IF NOT EXISTS tb_remark (
        remark_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        option_name VARCHAR(64) NOT NULL,
        sort_order  INTEGER     DEFAULT 0,
        del_flag    VARCHAR(2)  DEFAULT '0',
        create_time VARCHAR(32),
        update_time VARCHAR(32)
    );
CREATE TABLE IF NOT EXISTS tb_printer (
        printer_id   INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_print_log (
        print_id     INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_print_job (
        job_id       INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_oper_log (
        log_id        INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_config (
        cfg_key   VARCHAR(64)   PRIMARY KEY,
        cfg_value VARCHAR(4000) DEFAULT ''
    );
CREATE TABLE IF NOT EXISTS tb_order (
        order_id        INTEGER PRIMARY KEY AUTOINCREMENT,
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
        paid_amount     INTEGER      DEFAULT 0
    );
CREATE TABLE IF NOT EXISTS tb_order_item (
        item_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        order_id  INTEGER      NOT NULL,
        dish_id   INTEGER,
        dish_name VARCHAR(128),
        spec_id   INTEGER,
        spec_name VARCHAR(64),
        price     INTEGER      DEFAULT 0,
        quantity  INTEGER      DEFAULT 1,
        amount    INTEGER      DEFAULT 0,
        remark    VARCHAR(255) DEFAULT ''
    );
CREATE TABLE IF NOT EXISTS tb_order_urge (
        urge_id     INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_payment (
        payment_id       INTEGER PRIMARY KEY AUTOINCREMENT,
        order_no         VARCHAR(64)  NOT NULL,
        channel          VARCHAR(16)  NOT NULL,
        channel_trade_no VARCHAR(64)  DEFAULT '',
        amount           INTEGER      DEFAULT 0,
        status           INTEGER      DEFAULT 0,
        prepay_id        VARCHAR(128) DEFAULT '',
        notify_time      VARCHAR(32),
        create_time      VARCHAR(32),
        update_time      VARCHAR(32)
    );
CREATE TABLE IF NOT EXISTS tb_refund (
        refund_id         INTEGER PRIMARY KEY AUTOINCREMENT,
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
        update_time       VARCHAR(32)
    );
CREATE TABLE IF NOT EXISTS tb_role (
        role_id     INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_user (
        user_id         INTEGER PRIMARY KEY AUTOINCREMENT,
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
    );
CREATE TABLE IF NOT EXISTS tb_remember_token (
        token_id       INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id        INTEGER      NOT NULL,
        token          VARCHAR(64)  NOT NULL,
        expire_time    VARCHAR(32)  NOT NULL,
        create_time    VARCHAR(32),
        last_used_time VARCHAR(32)
    );
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_no ON tb_order(order_no);
CREATE INDEX IF NOT EXISTS idx_order_table ON tb_order(table_id);
CREATE INDEX IF NOT EXISTS idx_order_status ON tb_order(order_status);
CREATE INDEX IF NOT EXISTS idx_order_create_time ON tb_order(create_time);
CREATE INDEX IF NOT EXISTS idx_order_item_order ON tb_order_item(order_id);
CREATE INDEX IF NOT EXISTS idx_payment_order ON tb_payment(order_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_channel_trade ON tb_payment(channel, channel_trade_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_refund_no ON tb_refund(refund_no);
CREATE INDEX IF NOT EXISTS idx_refund_order ON tb_refund(order_id);
CREATE INDEX IF NOT EXISTS idx_urge_status ON tb_order_urge(status, create_time);
CREATE INDEX IF NOT EXISTS idx_urge_order ON tb_order_urge(order_id);
CREATE INDEX IF NOT EXISTS idx_print_log_order ON tb_print_log(order_id);
CREATE INDEX IF NOT EXISTS idx_print_log_time ON tb_print_log(create_time);
CREATE INDEX IF NOT EXISTS idx_print_log_status ON tb_print_log(status, create_time);
CREATE INDEX IF NOT EXISTS idx_print_job_pick ON tb_print_job(status, next_try_time, job_id);
CREATE INDEX IF NOT EXISTS idx_print_job_printer ON tb_print_job(printer_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_table_code ON tb_table(table_code) WHERE table_code!='';
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_username ON tb_user(username) WHERE del_flag='0';
CREATE INDEX IF NOT EXISTS idx_user_role ON tb_user(role_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_role_key ON tb_role(role_key) WHERE del_flag='0';
CREATE INDEX IF NOT EXISTS idx_operlog_time ON tb_oper_log(create_time);
CREATE INDEX IF NOT EXISTS idx_operlog_operator ON tb_oper_log(operator_id, create_time);
CREATE INDEX IF NOT EXISTS idx_operlog_target ON tb_oper_log(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_operlog_status ON tb_oper_log(status, create_time);
CREATE UNIQUE INDEX IF NOT EXISTS idx_remember_token ON tb_remember_token(token);
