-- ============================================================================
-- 点餐管理系统 — 全量建表脚本(SQLite)
-- 文件名:schema.sql —— 全量建表脚本(通用命名)
-- ----------------------------------------------------------------------------
-- 本文件由 `go run ./cmd/gensql` 从 backend/infrastructure/store 的表结构定义生成,
-- 请勿手工修改;需要变更表结构时改 Go 定义后重新生成。
--
-- 适用场景:全新部署 / 从零建库,执行后得到与后端代码完全一致的库结构
--           (已包含历史上通过 ALTER TABLE 追加的所有列)。
-- 执行方式:sqlite3 data/dining.db < schema.sql
-- 幂等性  :全部使用 IF NOT EXISTS,可重复执行。
--
-- 金额约定:所有金额字段以「分」为单位存 INTEGER,API 层由 po.ToYuan 转元。
-- ============================================================================

PRAGMA foreign_keys = OFF;

CREATE TABLE IF NOT EXISTS tb_table (
        table_id    INTEGER PRIMARY KEY AUTOINCREMENT,
        table_no    VARCHAR(32)  NOT NULL,
        table_name  VARCHAR(64)  NOT NULL,
        capacity    INTEGER      DEFAULT 0,
        status      INTEGER      DEFAULT 0,
        lock_status INTEGER      DEFAULT 0,
        lock_remark VARCHAR(255) DEFAULT '',
        sort_order  INTEGER      DEFAULT 0,
        del_flag    VARCHAR(2)   DEFAULT '0',
        create_by   VARCHAR(64)  DEFAULT '',
        update_by   VARCHAR(64)  DEFAULT '',
        remark      VARCHAR(500),
        table_code  VARCHAR(16)  DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_category (
        category_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        category_name VARCHAR(64) NOT NULL,
        sort_order    INTEGER     DEFAULT 0,
        del_flag      VARCHAR(2)  DEFAULT '0',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_dish (
        dish_id     INTEGER PRIMARY KEY AUTOINCREMENT,
        category_id INTEGER      NOT NULL,
        dish_name   VARCHAR(128) NOT NULL,
        dish_image  VARCHAR(255) DEFAULT '',
        description VARCHAR(500) DEFAULT '',
        status      INTEGER      DEFAULT 1,
        sold_out    INTEGER      DEFAULT 0,
        signature   INTEGER      DEFAULT 0,
        sort_order  INTEGER      DEFAULT 0,
        del_flag    VARCHAR(2)   DEFAULT '0',
        create_by   VARCHAR(64)  DEFAULT '',
        update_by   VARCHAR(64)  DEFAULT '',
        remark      VARCHAR(500),
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_spec (
        spec_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        dish_id   INTEGER      NOT NULL,
        spec_name VARCHAR(64)  NOT NULL,
        price     INTEGER      DEFAULT 0,
        stock     INTEGER      DEFAULT -1,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_remark (
        remark_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        option_name VARCHAR(64) NOT NULL,
        sort_order  INTEGER     DEFAULT 0,
        del_flag    VARCHAR(2)  DEFAULT '0',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        label        VARCHAR(64)  DEFAULT '',
        detail       VARCHAR(500) DEFAULT '',
        content      TEXT,
        paper_width  INTEGER      DEFAULT 48,
        trigger_by   VARCHAR(16)  DEFAULT '',
        operator     VARCHAR(64)  DEFAULT '',
        cost_ms      INTEGER      DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        claim_time VARCHAR(32) DEFAULT NULL,
        next_try_time VARCHAR(32) DEFAULT NULL,
        trigger_by   VARCHAR(16)   DEFAULT '',
        operator     VARCHAR(64)   DEFAULT '',
        done_time VARCHAR(32) DEFAULT NULL,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_print_agent (
        agent_id    INTEGER PRIMARY KEY AUTOINCREMENT,
        agent_name  VARCHAR(64)  DEFAULT '',
        token_hash  VARCHAR(64)  DEFAULT '',
        token_hint  VARCHAR(8)   DEFAULT '',
        printer_ids VARCHAR(255) DEFAULT '',
        status      INTEGER      DEFAULT 1,
        last_seen VARCHAR(32) DEFAULT NULL,
        last_report VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_config (
        cfg_key   VARCHAR(64)   PRIMARY KEY,
        cfg_value VARCHAR(4000) DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_day_seq (
        biz_date VARCHAR(10) PRIMARY KEY,
        next_val INTEGER      DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        pay_time VARCHAR(32) DEFAULT NULL,
        transaction_id  VARCHAR(64)  DEFAULT '',
        pay_channel     VARCHAR(16)  DEFAULT '',
        refund_amount   INTEGER      DEFAULT 0,
        refund_time VARCHAR(32) DEFAULT NULL,
        finish_time VARCHAR(32) DEFAULT NULL,
        order_remark    VARCHAR(500) DEFAULT '',
        cancel_reason   VARCHAR(255) DEFAULT '',
        create_by       VARCHAR(64)  DEFAULT '',
        update_by       VARCHAR(64)  DEFAULT '',
        remark          VARCHAR(500),
        settle_type     VARCHAR(16)  DEFAULT 'normal',
        settle_time VARCHAR(32) DEFAULT NULL,
        settle_operator VARCHAR(64)  DEFAULT '',
        settle_remark   VARCHAR(255) DEFAULT '',
        credit_status   INTEGER      DEFAULT 0,
        credit_amount   INTEGER      DEFAULT 0,
        credit_settle_time VARCHAR(32) DEFAULT NULL,
        credit_settle_by   VARCHAR(64) DEFAULT '',
        paid_amount     INTEGER      DEFAULT 0,
        -- 赠送金额(分):赠送菜品按原价计的金额合计,不计入应收金额(让利口径)。
        gift_amount     INTEGER      DEFAULT 0,
        -- 下单会员:0 = 匿名(历史单与未登录单);会员体系见 tb_customer。
        -- 请求体不带该值,一律以顾客令牌为准(防止伪造归属)。
        customer_id     INTEGER      DEFAULT 0,
        -- 结账时(状态被置 4 前)的订单状态快照:撤销结算时恢复到结账前的真实状态,
        -- 而不是无条件回到 3(订单可能在 1/2 状态被提前结账,撤销后应回到 1/2 继续制作流程)。
        -- 0 表示历史数据未记录(撤销时回退为 3)。
        pre_settle_status INTEGER     DEFAULT 0,
        -- 营业日(YYYY-MM-DD)与当日取餐号:专供顾客口头报号 / 厨房叫号,每天从 1 递增,
        -- 与不可枚举的内部单号 order_no 各司其职(见 tb_day_seq 的注释)。
        -- biz_date 缺省 NULL 而非空串的用意:让「非下单写入的行」不参与下面
        -- (biz_date, pickup_no) 的唯一判定 —— 两种数据库对含 NULL 的组合都不判重复。
        biz_date   VARCHAR(10) DEFAULT NULL,
        pickup_no  INTEGER     DEFAULT 0,
        -- 用餐起止时间(落库值):
        --   meal_start_time —— 下单时写入(用餐开始);
        --   meal_end_time   —— 订单走到终态(4 已完成 / 5 已取消)时写入(用餐结束)。
        -- 默认 NULL 而非空串:区分「还没结束」与「时间未知」,回填脚本与报警 SQL 一眼明了。
        meal_start_time VARCHAR(32) DEFAULT NULL,
        meal_end_time VARCHAR(32) DEFAULT NULL,
        -- 商家首次实际操作该订单的时间:NULL = 还没操作过(仅打开详情查看不算)。
        -- 列表据此给「已下单」的未操作订单打「新」角标,任意操作后写入,只写一次。
        viewed_at VARCHAR(32) DEFAULT NULL,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        remark    VARCHAR(255) DEFAULT '',
        -- 加菜批次:1=首次下单,每次加菜 +1,用于菜品明细里区分「首次下单/第N次加菜」。
        batch_no  INTEGER      DEFAULT 1,
        -- 幂等键:同一次下单/加菜提交的所有明细共享同一 client_request_id,用于去重(适当冗余)。
        client_request_id VARCHAR(64) DEFAULT '',
        -- 是否赠送:0 正常计费 1 赠送(商家改单标记,amount 记 0,原价计入订单 gift_amount)。
        is_gift   INTEGER      DEFAULT 0,
        gift_reason VARCHAR(255) DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        handle_time VARCHAR(32) DEFAULT NULL,
        handle_by   VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_payment (
        payment_id       INTEGER PRIMARY KEY AUTOINCREMENT,
        order_no         VARCHAR(64)  NOT NULL,
        channel          VARCHAR(16)  NOT NULL,
        channel_trade_no VARCHAR(64)  DEFAULT '',
        amount           INTEGER      DEFAULT 0,
        status           INTEGER      DEFAULT 0,
        prepay_id        VARCHAR(128) DEFAULT '',
        notify_time VARCHAR(32) DEFAULT NULL,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        -- 1 = 重复支付自动退回(顾客在两个渠道各付了一笔,后到的一笔原路退回)。
        -- 这类退款的金额从未计入订单营收(paid_amount),因此不计入订单退款额度、
        -- 也不扣减订单实收 —— 与普通退款(SumRefunded/applyRefundSuccess)严格区分。
        is_duplicate      INTEGER      DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
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
        remark      VARCHAR(500)  DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_user (
        user_id         INTEGER PRIMARY KEY AUTOINCREMENT,
        username        VARCHAR(64)  NOT NULL,
        password_hash   VARCHAR(255) DEFAULT '',
        real_name       VARCHAR(64)  DEFAULT '',
        role_id         INTEGER      NOT NULL DEFAULT 0,
        phone           VARCHAR(32)  DEFAULT '',
        status          INTEGER      DEFAULT 1,
        last_login_time VARCHAR(32) DEFAULT NULL,
        last_login_ip   VARCHAR(64)  DEFAULT '',
        login_count     INTEGER      DEFAULT 0,
        pwd_update_time VARCHAR(32) DEFAULT NULL,
        token_version   INTEGER      DEFAULT 0,
        del_flag        VARCHAR(2)   DEFAULT '0',
        create_by       VARCHAR(64)  DEFAULT '',
        update_by       VARCHAR(64)  DEFAULT '',
        remark          VARCHAR(500) DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_customer (
        customer_id      INTEGER PRIMARY KEY AUTOINCREMENT,
        nickname         VARCHAR(64)  DEFAULT '',
        avatar_url       VARCHAR(255) DEFAULT '',
        status           INTEGER      DEFAULT 1,
        order_count      INTEGER      DEFAULT 0,
        last_order_time VARCHAR(32) DEFAULT NULL,
        last_login_time VARCHAR(32) DEFAULT NULL,
        remark           VARCHAR(255) DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_customer_identity (
        identity_id      INTEGER PRIMARY KEY AUTOINCREMENT,
        customer_id      INTEGER      NOT NULL,
        provider         VARCHAR(32)  NOT NULL,
        external_id      VARCHAR(128) NOT NULL,
        credential       VARCHAR(255) DEFAULT '',
        unionid          VARCHAR(64)  DEFAULT NULL,
        last_login_time VARCHAR(32) DEFAULT NULL,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_wallet (
        customer_id    INTEGER PRIMARY KEY,
        balance        INTEGER      DEFAULT 0,
        balance_gift   INTEGER      DEFAULT 0,
        total_recharge INTEGER      DEFAULT 0,
        total_consume  INTEGER      DEFAULT 0,
        total_gift     INTEGER      DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_wallet_txn (
        txn_id         INTEGER PRIMARY KEY AUTOINCREMENT,
        customer_id    INTEGER      NOT NULL,
        biz_type       VARCHAR(16)  NOT NULL,
        amount         INTEGER      NOT NULL,
        balance_after  INTEGER      NOT NULL,
        gift_amount    INTEGER      DEFAULT 0,
        gift_after     INTEGER      DEFAULT 0,
        order_no       VARCHAR(32)  DEFAULT '',
        payment_no     VARCHAR(64)  DEFAULT '',
        reason         VARCHAR(255) DEFAULT '',
        operator       VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_wallet_agreement (
        customer_id    INTEGER PRIMARY KEY,
        rules_ver      VARCHAR(16)  NOT NULL,
        agreed_time VARCHAR(32) DEFAULT NULL,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_recharge (
        recharge_id      INTEGER PRIMARY KEY AUTOINCREMENT,
        recharge_no      VARCHAR(32)  NOT NULL,
        customer_id      INTEGER      NOT NULL,
        amount           INTEGER      NOT NULL,
        gift_amount      INTEGER      DEFAULT 0,
        status           INTEGER      DEFAULT 0,
        channel          VARCHAR(16)  DEFAULT '',
        channel_trade_no VARCHAR(64)  DEFAULT '',
        pay_time        VARCHAR(32) DEFAULT NULL,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_remember_token (
        token_id       INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id        INTEGER      NOT NULL,
        token          VARCHAR(64)  NOT NULL,
        token_prefix   VARCHAR(8)   DEFAULT '',
        expire_time VARCHAR(32) DEFAULT NULL,
        last_used_time VARCHAR(32) DEFAULT NULL,
        ua             VARCHAR(255) DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_image (
        img_id       INTEGER PRIMARY KEY AUTOINCREMENT,
        img_name     VARCHAR(128) NOT NULL,
        content_type VARCHAR(32)  DEFAULT '',
        img_data     BLOB,
        file_size    INTEGER      DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time VARCHAR(32) DEFAULT NULL
    );
CREATE TABLE IF NOT EXISTS tb_visit_log (
        visit_id    INTEGER PRIMARY KEY AUTOINCREMENT,
        visit_date  VARCHAR(10)  NOT NULL,
        channel     VARCHAR(16)  NOT NULL,
        visitor_key VARCHAR(80)  NOT NULL,
        is_member   INTEGER      DEFAULT 0,
        customer_id INTEGER      DEFAULT 0,
        entry       VARCHAR(16)  DEFAULT '',
        ip_hash     VARCHAR(64)  DEFAULT '',
        ua_hash     VARCHAR(64)  DEFAULT '',
        -- 终端与来源信息(2026-10-18 增补):IP 明文用于排查与展示(明细 90 天清理,
        -- 与操作日志 oper_ip 明文同一取舍);城市来自 ip2region 离线库
        -- (data/ip2region.xdb,缺文件时为空);OS/浏览器/设备类型为归一后的展示值
        -- (iOS/微信/手机…),机型安卓 UA 可解出型号,iOS 的 UA 不含机型存空,
        -- 小程序端上直传最准。
        ip          VARCHAR(45)  DEFAULT '',
        city        VARCHAR(64)  DEFAULT '',
        os          VARCHAR(16)  DEFAULT '',
        browser     VARCHAR(16)  DEFAULT '',
        device      VARCHAR(8)   DEFAULT '',
        model       VARCHAR(64)  DEFAULT '',
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime'))
    );
CREATE TABLE IF NOT EXISTS tb_visitor (
        visitor_key      VARCHAR(80) PRIMARY KEY,
        first_visit_date VARCHAR(10) NOT NULL,
        last_visit_date  VARCHAR(10) NOT NULL,
        visit_count      INTEGER     DEFAULT 1,
        is_member        INTEGER     DEFAULT 0,
        customer_id      INTEGER     DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime'))
    );
CREATE TABLE IF NOT EXISTS tb_visit_daily (
        id         INTEGER PRIMARY KEY AUTOINCREMENT,
        visit_date VARCHAR(10) NOT NULL,
        channel    VARCHAR(16) NOT NULL,
        pv         INTEGER     DEFAULT 0,
        uv         INTEGER     DEFAULT 0,
        new_uv     INTEGER     DEFAULT 0,
        member_uv  INTEGER     DEFAULT 0,
        create_time VARCHAR(32) DEFAULT (datetime('now','localtime'))
    );
CREATE TRIGGER IF NOT EXISTS trg_tb_table_touch
        AFTER UPDATE ON tb_table FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_table SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_category_touch
        AFTER UPDATE ON tb_category FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_category SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_dish_touch
        AFTER UPDATE ON tb_dish FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_dish SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_spec_touch
        AFTER UPDATE ON tb_spec FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_spec SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_remark_touch
        AFTER UPDATE ON tb_remark FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_remark SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_printer_touch
        AFTER UPDATE ON tb_printer FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_printer SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_print_log_touch
        AFTER UPDATE ON tb_print_log FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_print_log SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_print_job_touch
        AFTER UPDATE ON tb_print_job FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_print_job SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_print_agent_touch
        AFTER UPDATE ON tb_print_agent FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_print_agent SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_oper_log_touch
        AFTER UPDATE ON tb_oper_log FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_oper_log SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_config_touch
        AFTER UPDATE ON tb_config FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_config SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_day_seq_touch
        AFTER UPDATE ON tb_day_seq FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_day_seq SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_order_touch
        AFTER UPDATE ON tb_order FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_order SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_order_item_touch
        AFTER UPDATE ON tb_order_item FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_order_item SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_order_urge_touch
        AFTER UPDATE ON tb_order_urge FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_order_urge SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_payment_touch
        AFTER UPDATE ON tb_payment FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_payment SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_refund_touch
        AFTER UPDATE ON tb_refund FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_refund SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_role_touch
        AFTER UPDATE ON tb_role FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_role SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_user_touch
        AFTER UPDATE ON tb_user FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_user SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_customer_touch
        AFTER UPDATE ON tb_customer FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_customer SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_customer_identity_touch
        AFTER UPDATE ON tb_customer_identity FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_customer_identity SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_wallet_touch
        AFTER UPDATE ON tb_wallet FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_wallet SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_wallet_txn_touch
        AFTER UPDATE ON tb_wallet_txn FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_wallet_txn SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_wallet_agreement_touch
        AFTER UPDATE ON tb_wallet_agreement FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_wallet_agreement SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_recharge_touch
        AFTER UPDATE ON tb_recharge FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_recharge SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_remember_token_touch
        AFTER UPDATE ON tb_remember_token FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_remember_token SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE TRIGGER IF NOT EXISTS trg_tb_image_touch
        AFTER UPDATE ON tb_image FOR EACH ROW
        WHEN NEW.update_time IS OLD.update_time
        BEGIN
            UPDATE tb_image SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
        END;
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_no ON tb_order(order_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_pickup ON tb_order(biz_date, pickup_no);
CREATE INDEX IF NOT EXISTS idx_order_table ON tb_order(table_id);
CREATE INDEX IF NOT EXISTS idx_order_status ON tb_order(order_status);
CREATE INDEX IF NOT EXISTS idx_order_table_unpaid ON tb_order(table_id, pay_status, order_status);
CREATE INDEX IF NOT EXISTS idx_order_create_time ON tb_order(create_time);
CREATE INDEX IF NOT EXISTS idx_order_pay_time ON tb_order(pay_time);
CREATE INDEX IF NOT EXISTS idx_order_credit_settle_time ON tb_order(credit_settle_time);
CREATE INDEX IF NOT EXISTS idx_order_customer ON tb_order(customer_id, create_time);
CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_provider_external ON tb_customer_identity(provider, external_id);
CREATE INDEX IF NOT EXISTS idx_identity_customer ON tb_customer_identity(customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_recharge_no ON tb_recharge(recharge_no);
CREATE INDEX IF NOT EXISTS idx_wallet_txn_customer ON tb_wallet_txn(customer_id, txn_id);
CREATE INDEX IF NOT EXISTS idx_identity_unionid ON tb_customer_identity(unionid);
CREATE INDEX IF NOT EXISTS idx_order_item_order ON tb_order_item(order_id);
CREATE INDEX IF NOT EXISTS idx_payment_order ON tb_payment(order_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_channel_trade ON tb_payment(channel, channel_trade_no);
CREATE UNIQUE INDEX IF NOT EXISTS idx_refund_no ON tb_refund(refund_no);
CREATE INDEX IF NOT EXISTS idx_refund_order ON tb_refund(order_id);
CREATE INDEX IF NOT EXISTS idx_refund_update_time ON tb_refund(update_time);
CREATE INDEX IF NOT EXISTS idx_urge_status ON tb_order_urge(status, create_time);
CREATE INDEX IF NOT EXISTS idx_urge_order ON tb_order_urge(order_id);
CREATE INDEX IF NOT EXISTS idx_print_log_order ON tb_print_log(order_id);
CREATE INDEX IF NOT EXISTS idx_print_log_time ON tb_print_log(create_time);
CREATE INDEX IF NOT EXISTS idx_print_log_status ON tb_print_log(status, create_time);
CREATE INDEX IF NOT EXISTS idx_print_job_pick ON tb_print_job(status, next_try_time, job_id);
CREATE INDEX IF NOT EXISTS idx_print_job_printer ON tb_print_job(printer_id, status);
CREATE INDEX IF NOT EXISTS idx_print_agent_token ON tb_print_agent(token_hash, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_table_code ON tb_table(table_code) WHERE table_code!='';
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_username ON tb_user(username) WHERE del_flag='0';
CREATE INDEX IF NOT EXISTS idx_user_role ON tb_user(role_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_role_key ON tb_role(role_key);
CREATE INDEX IF NOT EXISTS idx_operlog_time ON tb_oper_log(create_time);
CREATE INDEX IF NOT EXISTS idx_operlog_operator ON tb_oper_log(operator_id, create_time);
CREATE INDEX IF NOT EXISTS idx_operlog_target ON tb_oper_log(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_operlog_status ON tb_oper_log(status, create_time);
CREATE UNIQUE INDEX IF NOT EXISTS idx_remember_token ON tb_remember_token(token);
CREATE UNIQUE INDEX IF NOT EXISTS idx_image_name ON tb_image(img_name);
CREATE INDEX IF NOT EXISTS idx_visitlog_date ON tb_visit_log(visit_date);
CREATE INDEX IF NOT EXISTS idx_visitlog_visitor ON tb_visit_log(visit_date, visitor_key);
CREATE INDEX IF NOT EXISTS idx_visitlog_channel ON tb_visit_log(visit_date, channel, visitor_key);
CREATE INDEX IF NOT EXISTS idx_visitor_first ON tb_visitor(first_visit_date);
CREATE UNIQUE INDEX IF NOT EXISTS idx_visit_daily_uk ON tb_visit_daily(visit_date, channel);
