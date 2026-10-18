-- ============================================================================
-- ç¹é¤ç®¡çç³»ç» â å¨éå»ºè¡¨èæ¬(MySQL 8.0 / 5.7,MariaDB 10.3+)
-- æä»¶å:schema.sql ââ å¨éå»ºè¡¨èæ¬(éç¨å½å)
-- ----------------------------------------------------------------------------
-- æ¬æä»¶ç± `go run ./cmd/gensql` ä» backend/infrastructure/store çè¡¨ç»æå®ä¹çæ,
-- è¯·å¿æå·¥ä¿®æ¹;éè¦åæ´è¡¨ç»ææ¶æ¹ Go å®ä¹åéæ°çæã
--
-- åç½®æ­¥éª¤(åªéæ§è¡ä¸æ¬¡):
--   CREATE DATABASE IF NOT EXISTS dining
--     DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
--   CREATE USER IF NOT EXISTS 'dining'@'%' IDENTIFIED BY 'æ¹æä½ çå¼ºå¯ç ';
--   GRANT ALL PRIVILEGES ON dining.* TO 'dining'@'%';
--   FLUSH PRIVILEGES;
--
-- æ§è¡æ¹å¼:mysql -u dining -p dining < schema.sql
--
-- æ³¨æäºé¡¹:
--   1. MySQL ä¸æ¯æ CREATE INDEX IF NOT EXISTS;éå¤æ§è¡æ¶å»ºè¡¨è¯­å¥ä¼è¢«
--      IF NOT EXISTS è·³è¿,ä½å»ºç´¢å¼è¯­å¥ä¼æ¥ 'Duplicate key name',
--      è¯¥éè¯¯å¯å®å¨å¿½ç¥(å¯ç¨ `mysql --force` ç»§ç»­æ§è¡)ã
--   2. è¡¨å¼æ InnoDBãå­ç¬¦é utf8mb4,éå¨ MySQL 5.7+ ä¸æ§è¡ã
--   3. ä¸ SQLite ççå¯ä¸è¯­ä¹å·®å¼:MySQL ä¼å¼ºå¶æ ¡éª VARCHAR é¿åº¦,
--      SQLite ä¸æ ¡éª,å¯¼å¥è¶é¿æ°æ®æ¶ MySQL ä¼æ¥ 'Data too long'ã
--
-- éé¢çº¦å®:ææéé¢å­æ®µä»¥ãåãä¸ºåä½å­ INT,API å±ç± po.ToYuan è½¬åã
-- ============================================================================

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS tb_table (
        table_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
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
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_category (
        category_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        category_name VARCHAR(64) NOT NULL,
        sort_order    INTEGER     DEFAULT 0,
        del_flag      VARCHAR(2)  DEFAULT '0',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_dish (
        dish_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
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
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_spec (
        spec_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        dish_id   INTEGER      NOT NULL,
        spec_name VARCHAR(64)  NOT NULL,
        price     INTEGER      DEFAULT 0,
        stock     INTEGER      DEFAULT -1,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_remark (
        remark_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        option_name VARCHAR(64) NOT NULL,
        sort_order  INTEGER     DEFAULT 0,
        del_flag    VARCHAR(2)  DEFAULT '0',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        label        VARCHAR(64)  DEFAULT '',
        detail       VARCHAR(500) DEFAULT '',
        content      LONGTEXT,
        paper_width  INTEGER      DEFAULT 48,
        trigger_by   VARCHAR(16)  DEFAULT '',
        operator     VARCHAR(64)  DEFAULT '',
        cost_ms      INTEGER      DEFAULT 0,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        claim_time VARCHAR(32) DEFAULT NULL,
        next_try_time VARCHAR(32) DEFAULT NULL,
        trigger_by   VARCHAR(16)   DEFAULT '',
        operator     VARCHAR(64)   DEFAULT '',
        done_time VARCHAR(32) DEFAULT NULL,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_print_agent (
        agent_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        agent_name  VARCHAR(64)  DEFAULT '',
        token_hash  VARCHAR(64)  DEFAULT '',
        token_hint  VARCHAR(8)   DEFAULT '',
        printer_ids VARCHAR(255) DEFAULT '',
        status      INTEGER      DEFAULT 1,
        last_seen VARCHAR(32) DEFAULT NULL,
        last_report VARCHAR(64)  DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_config (
        cfg_key   VARCHAR(64)   PRIMARY KEY,
        cfg_value VARCHAR(4000) DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_day_seq (
        biz_date VARCHAR(10) PRIMARY KEY,
        next_val INTEGER      DEFAULT 0,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        -- èµ ééé¢(å):èµ éèåæåä»·è®¡çéé¢åè®¡,ä¸è®¡å¥åºæ¶éé¢(è®©å©å£å¾)ã
        gift_amount     INTEGER      DEFAULT 0,
        -- ä¸åä¼å:0 = å¿å(åå²åä¸æªç»å½å);ä¼åä½ç³»è§ tb_customerã
        -- è¯·æ±ä½ä¸å¸¦è¯¥å¼,ä¸å¾ä»¥é¡¾å®¢ä»¤çä¸ºå(é²æ­¢ä¼ªé å½å±)ã
        customer_id     INTEGER      DEFAULT 0,
        -- ç»è´¦æ¶(ç¶æè¢«ç½® 4 å)çè®¢åç¶æå¿«ç§:æ¤éç»ç®æ¶æ¢å¤å°ç»è´¦åççå®ç¶æ,
        -- èä¸æ¯æ æ¡ä»¶åå° 3(è®¢åå¯è½å¨ 1/2 ç¶æè¢«æåç»è´¦,æ¤éååºåå° 1/2 ç»§ç»­å¶ä½æµç¨)ã
        -- 0 è¡¨ç¤ºåå²æ°æ®æªè®°å½(æ¤éæ¶åéä¸º 3)ã
        pre_settle_status INTEGER     DEFAULT 0,
        -- è¥ä¸æ¥(YYYY-MM-DD)ä¸å½æ¥åé¤å·:ä¸ä¾é¡¾å®¢å£å¤´æ¥å· / å¨æ¿å«å·,æ¯å¤©ä» 1 éå¢,
        -- ä¸ä¸å¯æä¸¾çåé¨åå· order_no åå¸å¶è(è§ tb_day_seq çæ³¨é)ã
        -- biz_date ç¼ºç NULL èéç©ºä¸²çç¨æ:è®©ãéä¸ååå¥çè¡ãä¸åä¸ä¸é¢
        -- (biz_date, pickup_no) çå¯ä¸å¤å® ââ ä¸¤ç§æ°æ®åºå¯¹å« NULL çç»åé½ä¸å¤éå¤ã
        biz_date   VARCHAR(10) DEFAULT NULL,
        pickup_no  INTEGER     DEFAULT 0,
        -- ç¨é¤èµ·æ­¢æ¶é´(è½åºå¼):
        --   meal_start_time ââ ä¸åæ¶åå¥(ç¨é¤å¼å§);
        --   meal_end_time   ââ è®¢åèµ°å°ç»æ(4 å·²å®æ / 5 å·²åæ¶)æ¶åå¥(ç¨é¤ç»æ)ã
        -- é»è®¤ NULL èéç©ºä¸²:åºåãè¿æ²¡ç»æãä¸ãæ¶é´æªç¥ã,åå¡«èæ¬ä¸æ¥è­¦ SQL ä¸ç¼æäºã
        meal_start_time VARCHAR(32) DEFAULT NULL,
        meal_end_time VARCHAR(32) DEFAULT NULL,
        -- 商家首次查看/操作该订单的时间:NULL = 还没人看过，列表据此打「新」角标。
        viewed_at VARCHAR(32) DEFAULT NULL,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        remark    VARCHAR(255) DEFAULT '',
        -- å èæ¹æ¬¡:1=é¦æ¬¡ä¸å,æ¯æ¬¡å è +1,ç¨äºèåæç»éåºåãé¦æ¬¡ä¸å/ç¬¬Næ¬¡å èãã
        batch_no  INTEGER      DEFAULT 1,
        -- å¹ç­é®:åä¸æ¬¡ä¸å/å èæäº¤çæææç»å±äº«åä¸ client_request_id,ç¨äºå»é(éå½åä½)ã
        client_request_id VARCHAR(64) DEFAULT '',
        -- æ¯å¦èµ é:0 æ­£å¸¸è®¡è´¹ 1 èµ é(åå®¶æ¹åæ è®°,amount è®° 0,åä»·è®¡å¥è®¢å gift_amount)ã
        is_gift   INTEGER      DEFAULT 0,
        gift_reason VARCHAR(255) DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        handle_time VARCHAR(32) DEFAULT NULL,
        handle_by   VARCHAR(64)  DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_payment (
        payment_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        order_no         VARCHAR(64)  NOT NULL,
        channel          VARCHAR(16)  NOT NULL,
        channel_trade_no VARCHAR(64)  DEFAULT '',
        amount           INTEGER      DEFAULT 0,
        status           INTEGER      DEFAULT 0,
        prepay_id        VARCHAR(128) DEFAULT '',
        notify_time VARCHAR(32) DEFAULT NULL,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        -- 1 = éå¤æ¯ä»èªå¨éå(é¡¾å®¢å¨ä¸¤ä¸ªæ¸ éåä»äºä¸ç¬,åå°çä¸ç¬åè·¯éå)ã
        -- è¿ç±»éæ¬¾çéé¢ä»æªè®¡å¥è®¢åè¥æ¶(paid_amount),å æ­¤ä¸è®¡å¥è®¢åéæ¬¾é¢åº¦ã
        -- ä¹ä¸æ£åè®¢åå®æ¶ ââ ä¸æ®ééæ¬¾(SumRefunded/applyRefundSuccess)ä¸¥æ ¼åºåã
        is_duplicate      INTEGER      DEFAULT 0,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
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
        remark      VARCHAR(500)  DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_user (
        user_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
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
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_customer (
        customer_id      INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        openid           VARCHAR(64)  NOT NULL,
        nickname         VARCHAR(64)  DEFAULT '',
        avatar_url       VARCHAR(255) DEFAULT '',
        status           INTEGER      DEFAULT 1,
        order_count      INTEGER      DEFAULT 0,
        last_order_time VARCHAR(32) DEFAULT NULL,
        last_login_time VARCHAR(32) DEFAULT NULL,
        remark           VARCHAR(255) DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_customer_identity (
        identity_id      INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        customer_id      INTEGER      NOT NULL,
        provider         VARCHAR(32)  NOT NULL,
        external_id      VARCHAR(128) NOT NULL,
        credential       VARCHAR(255) DEFAULT '',
        unionid          VARCHAR(64)  DEFAULT NULL,
        last_login_time DATETIME DEFAULT NULL,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_remember_token (
        token_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        user_id        INTEGER      NOT NULL,
        token          VARCHAR(64)  NOT NULL,
        token_prefix   VARCHAR(8)   DEFAULT '',
        expire_time VARCHAR(32) DEFAULT NULL,
        last_used_time VARCHAR(32) DEFAULT NULL,
        ua             VARCHAR(255) DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_image (
        img_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        img_name     VARCHAR(128) NOT NULL,
        content_type VARCHAR(32)  DEFAULT '',
        img_data     LONGBLOB,
        file_size    INTEGER      DEFAULT 0,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_visit_log (
        visit_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        visit_date  VARCHAR(10)  NOT NULL,
        channel     VARCHAR(16)  NOT NULL,
        visitor_key VARCHAR(80)  NOT NULL,
        is_member   INTEGER      DEFAULT 0,
        customer_id INTEGER      DEFAULT 0,
        entry       VARCHAR(16)  DEFAULT '',
        ip_hash     VARCHAR(64)  DEFAULT '',
        ua_hash     VARCHAR(64)  DEFAULT '',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_visitor (
        visitor_key      VARCHAR(80) PRIMARY KEY,
        first_visit_date VARCHAR(10) NOT NULL,
        last_visit_date  VARCHAR(10) NOT NULL,
        visit_count      INTEGER     DEFAULT 1,
        is_member        INTEGER     DEFAULT 0,
        customer_id      INTEGER     DEFAULT 0,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE TABLE IF NOT EXISTS tb_visit_daily (
        id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
        visit_date VARCHAR(10) NOT NULL,
        channel    VARCHAR(16) NOT NULL,
        pv         INTEGER     DEFAULT 0,
        uv         INTEGER     DEFAULT 0,
        new_uv     INTEGER     DEFAULT 0,
        member_uv  INTEGER     DEFAULT 0,
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
CREATE UNIQUE INDEX idx_order_no ON tb_order(order_no);
CREATE UNIQUE INDEX idx_order_pickup ON tb_order(biz_date, pickup_no);
CREATE INDEX idx_order_table ON tb_order(table_id);
CREATE INDEX idx_order_status ON tb_order(order_status);
CREATE INDEX idx_order_table_unpaid ON tb_order(table_id, pay_status, order_status);
CREATE INDEX idx_order_create_time ON tb_order(create_time);
CREATE INDEX idx_order_pay_time ON tb_order(pay_time);
CREATE INDEX idx_order_credit_settle_time ON tb_order(credit_settle_time);
CREATE INDEX idx_order_customer ON tb_order(customer_id, create_time);
CREATE UNIQUE INDEX idx_customer_openid ON tb_customer(openid);
CREATE UNIQUE INDEX idx_identity_provider_external ON tb_customer_identity(provider, external_id);
CREATE INDEX idx_identity_customer ON tb_customer_identity(customer_id);
CREATE INDEX idx_identity_unionid ON tb_customer_identity(unionid);
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
CREATE UNIQUE INDEX idx_role_key ON tb_role(role_key);
CREATE INDEX idx_operlog_time ON tb_oper_log(create_time);
CREATE INDEX idx_operlog_operator ON tb_oper_log(operator_id, create_time);
CREATE INDEX idx_operlog_target ON tb_oper_log(target_type, target_id);
CREATE INDEX idx_operlog_status ON tb_oper_log(status, create_time);
CREATE UNIQUE INDEX idx_remember_token ON tb_remember_token(token);
CREATE UNIQUE INDEX idx_image_name ON tb_image(img_name);
CREATE INDEX idx_visitlog_date ON tb_visit_log(visit_date);
CREATE INDEX idx_visitlog_visitor ON tb_visit_log(visit_date, visitor_key);
CREATE INDEX idx_visitlog_channel ON tb_visit_log(visit_date, channel, visitor_key);
CREATE INDEX idx_visitor_first ON tb_visitor(first_visit_date);
CREATE UNIQUE INDEX idx_visit_daily_uk ON tb_visit_daily(visit_date, channel);
