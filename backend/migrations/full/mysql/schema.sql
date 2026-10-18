-- ============================================================================
-- 点餐管理系统 — 全量建表脚本(MySQL 8.0 / 5.7,MariaDB 10.3+)
-- 文件名:schema.sql —— 全量建表脚本(通用命名)
-- ----------------------------------------------------------------------------
-- 本文件由 `go run ./cmd/gensql` 从 backend/infrastructure/store 的表结构定义生成,
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
--   4. 字段注释:每张表与每一列都带 COMMENT(定义见
--      infrastructure/store/schema_comment.go);SQLite 无注释语法,
--      字段说明以建表脚本内 -- 注释与 migrations/README.md 为准。
--
-- 金额约定:所有金额字段以「分」为单位存 INT,API 层由 po.ToYuan 转元。
-- ============================================================================

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS tb_table (
        table_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '桌台ID(自增主键)',
        table_no    VARCHAR(32)  NOT NULL COMMENT '桌号,如 A1',
        table_name  VARCHAR(64)  NOT NULL COMMENT '桌台名称',
        capacity    INTEGER      DEFAULT 0 COMMENT '座位数',
        status      INTEGER      DEFAULT 0 COMMENT '占用状态 0空闲 1占用(订单自动流转,勿手工置位)',
        lock_status INTEGER      DEFAULT 0 COMMENT '人工可用性 0正常 1停用 2预约(与status正交)',
        lock_remark VARCHAR(255) DEFAULT '' COMMENT '停用原因或预约人/时段',
        sort_order  INTEGER      DEFAULT 0 COMMENT '排序值(小在前)',
        del_flag    VARCHAR(2)   DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        create_by   VARCHAR(64)  DEFAULT '' COMMENT '创建人',
        update_by   VARCHAR(64)  DEFAULT '' COMMENT '更新人',
        remark      VARCHAR(500) COMMENT '备注',
        table_code  VARCHAR(16)  DEFAULT '' COMMENT '桌台码(二维码内容,生成后不变,防自增ID被枚举)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='桌台';
CREATE TABLE IF NOT EXISTS tb_category (
        category_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '分类ID(自增主键)',
        category_name VARCHAR(64) NOT NULL COMMENT '分类名称',
        sort_order    INTEGER     DEFAULT 0 COMMENT '排序值(小在前)',
        del_flag      VARCHAR(2)  DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='菜品分类';
CREATE TABLE IF NOT EXISTS tb_dish (
        dish_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '菜品ID(自增主键)',
        category_id INTEGER      NOT NULL COMMENT '所属分类ID',
        dish_name   VARCHAR(128) NOT NULL COMMENT '菜品名称',
        dish_image  VARCHAR(255) DEFAULT '' COMMENT '图片路径(/uploads/ 下,内容存 tb_image)',
        description VARCHAR(500) DEFAULT '' COMMENT '菜品描述',
        status      INTEGER      DEFAULT 1 COMMENT '上架状态 1上架 0下架',
        sold_out    INTEGER      DEFAULT 0 COMMENT '售罄标记 1售罄 0正常(人工标记)',
        signature   INTEGER      DEFAULT 0 COMMENT '招牌菜标记 1招牌 0普通',
        sort_order  INTEGER      DEFAULT 0 COMMENT '排序值(小在前)',
        del_flag    VARCHAR(2)   DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        create_by   VARCHAR(64)  DEFAULT '' COMMENT '创建人',
        update_by   VARCHAR(64)  DEFAULT '' COMMENT '更新人',
        remark      VARCHAR(500) COMMENT '备注',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='菜品';
CREATE TABLE IF NOT EXISTS tb_spec (
        spec_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '规格ID(自增主键)',
        dish_id   INTEGER      NOT NULL COMMENT '所属菜品ID',
        spec_name VARCHAR(64)  NOT NULL COMMENT '规格名称',
        price     INTEGER      DEFAULT 0 COMMENT '售价(分)',
        stock     INTEGER      DEFAULT -1 COMMENT '库存 -1不限量 其余为剩余份数(触发自动售罄)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='菜品规格';
CREATE TABLE IF NOT EXISTS tb_remark (
        remark_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '备注ID(自增主键)',
        option_name VARCHAR(64) NOT NULL COMMENT '备注常用语文案',
        sort_order  INTEGER     DEFAULT 0 COMMENT '排序值(小在前)',
        del_flag    VARCHAR(2)  DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='备注常用语';
CREATE TABLE IF NOT EXISTS tb_printer (
        printer_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '打印机ID(自增主键)',
        printer_name VARCHAR(64) NOT NULL COMMENT '打印机名称',
        printer_type INTEGER     DEFAULT 1 COMMENT '票种 1厨房单 2顾客小票',
        provider     VARCHAR(16) DEFAULT 'tcp' COMMENT '接入方式 tcp直连 feie飞鹅云 agent本地代理',
        ip           VARCHAR(64) DEFAULT '' COMMENT '打印机IP(agent通道为门店内网地址)',
        port         INTEGER     DEFAULT 9100 COMMENT '打印机端口(默认9100)',
        feie_sn      VARCHAR(64) DEFAULT '' COMMENT '飞鹅云终端SN(provider=feie时必填)',
        paper_width  INTEGER     DEFAULT 48 COMMENT '纸宽 32=58mm 48=80mm',
        copies       INTEGER     DEFAULT 1 COMMENT '打印份数(1-5)',
        category_ids VARCHAR(255) DEFAULT '' COMMENT '按分类分单的CSV分类ID(空=收全部菜品)',
        status       INTEGER     DEFAULT 1 COMMENT '启用状态 1启用 0停用',
        del_flag     VARCHAR(2)  DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='打印机';
CREATE TABLE IF NOT EXISTS tb_print_log (
        print_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '日志ID(自增主键)',
        order_id     INTEGER      DEFAULT 0 COMMENT '关联订单ID',
        order_no     VARCHAR(64)  DEFAULT '' COMMENT '关联订单号',
        table_no     VARCHAR(32)  DEFAULT '' COMMENT '桌号快照',
        table_name   VARCHAR(64)  DEFAULT '' COMMENT '桌名快照',
        printer_id   INTEGER      DEFAULT 0 COMMENT '打印机ID',
        printer_name VARCHAR(64)  DEFAULT '' COMMENT '打印机名称快照',
        printer_type INTEGER      DEFAULT 1 COMMENT '票种 1厨房单 2顾客小票',
        provider     VARCHAR(16)  DEFAULT 'tcp' COMMENT '发送通道 tcp/feie/agent',
        doc_type     VARCHAR(16)  DEFAULT '' COMMENT '票据类型 kitchen厨房单 guest顾客小票 guest_append加菜单',
        copies       INTEGER      DEFAULT 1 COMMENT '打印份数',
        status       INTEGER      DEFAULT 0 COMMENT '结果 0失败 1已送出 2已入队(仅agent通道)',
        remote_id    VARCHAR(128) DEFAULT '' COMMENT '飞鹅云订单号(追问真实出纸状态用)',
        label        VARCHAR(64)  DEFAULT '' COMMENT '票据标识,如【厨房单】1/4(拆单场景区分丢的是哪张)',
        detail       VARCHAR(500) DEFAULT '' COMMENT '发送详情/失败原因(代理通道会整段改写)',
        content      LONGTEXT COMMENT '票面快照(JSON文本行,预览与补打回放用)',
        paper_width  INTEGER      DEFAULT 48 COMMENT '快照纸宽 32=58mm 48=80mm',
        trigger_by   VARCHAR(16)  DEFAULT '' COMMENT '触发场景 order下单 append加菜 settle结账 test测试 reprint补打',
        operator     VARCHAR(64)  DEFAULT '' COMMENT '操作人',
        cost_ms      INTEGER      DEFAULT 0 COMMENT '发送耗时(毫秒)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='打印台账(每次向打印机发单的留痕)';
CREATE TABLE IF NOT EXISTS tb_print_job (
        job_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '任务ID(自增主键)',
        printer_id   INTEGER       DEFAULT 0 COMMENT '目标打印机ID',
        printer_name VARCHAR(64)   DEFAULT '' COMMENT '打印机名称快照',
        printer_type INTEGER       DEFAULT 1 COMMENT '票种 1厨房单 2顾客小票',
        ip           VARCHAR(64)   DEFAULT '' COMMENT '打印机门店内网IP(代理直发用)',
        port         INTEGER       DEFAULT 9100 COMMENT '打印机端口',
        doc_type     VARCHAR(16)   DEFAULT '' COMMENT '票据类型(同 tb_print_log.doc_type)',
        order_id     INTEGER       DEFAULT 0 COMMENT '关联订单ID',
        order_no     VARCHAR(64)   DEFAULT '' COMMENT '关联订单号',
        table_no     VARCHAR(32)   DEFAULT '' COMMENT '桌号快照',
        copies       INTEGER       DEFAULT 1 COMMENT '打印份数',
        print_log_id INTEGER       DEFAULT 0 COMMENT '关联打印台账ID',
        delivery_id  VARCHAR(36)   DEFAULT '' COMMENT '投递批次ID(代理回执对账用)',
        payload      VARCHAR(4000) DEFAULT '' COMMENT '票据等宽文本行(换行分隔,取单时才转ESC/POS)',
        status       INTEGER       DEFAULT 0 COMMENT '0待取单 1已取单(租约) 2已送出 3已放弃',
        attempts     INTEGER       DEFAULT 0 COMMENT '已尝试次数(上限3)',
        last_error   VARCHAR(500)  DEFAULT '' COMMENT '最近一次失败原因',
        claimed_by   VARCHAR(64)   DEFAULT '' COMMENT '取单代理标识',
        claim_time VARCHAR(32) DEFAULT NULL COMMENT '取单时间(租约起点)',
        next_try_time VARCHAR(32) DEFAULT NULL COMMENT '下次可取单时间',
        trigger_by   VARCHAR(16)   DEFAULT '' COMMENT '触发场景(同 tb_print_log.trigger_by)',
        operator     VARCHAR(64)   DEFAULT '' COMMENT '操作人',
        done_time VARCHAR(32) DEFAULT NULL COMMENT '送达时间',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='本地打印代理任务队列';
CREATE TABLE IF NOT EXISTS tb_print_agent (
        agent_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '代理ID(自增主键)',
        agent_name  VARCHAR(64)  DEFAULT '' COMMENT '代理名称',
        token_hash  VARCHAR(64)  DEFAULT '' COMMENT '令牌SHA-256(不存明文)',
        token_hint  VARCHAR(8)   DEFAULT '' COMMENT '令牌前8位(识别用)',
        printer_ids VARCHAR(255) DEFAULT '' COMMENT '授权打印机ID(CSV)',
        status      INTEGER      DEFAULT 1 COMMENT '1启用 0停用',
        last_seen VARCHAR(32) DEFAULT NULL COMMENT '最近心跳时间',
        last_report VARCHAR(64)  DEFAULT '' COMMENT '最近上报状态摘要',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='本地打印代理身份';
CREATE TABLE IF NOT EXISTS tb_oper_log (
        log_id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '日志ID(自增主键)',
        module        VARCHAR(32)   DEFAULT '' COMMENT '模块中文名,如 订单管理',
        business_type VARCHAR(16)   DEFAULT 'other' COMMENT '操作类型 insert/update/delete/login/grant/print/other',
        action        VARCHAR(64)   DEFAULT '' COMMENT '动作中文名,如 修改菜品',
        method        VARCHAR(255)  DEFAULT '' COMMENT 'HTTP方法',
        request_url   VARCHAR(255)  DEFAULT '' COMMENT '请求URL',
        operator_id   INTEGER       DEFAULT 0 COMMENT '操作人ID',
        operator      VARCHAR(64)   DEFAULT '' COMMENT '操作人姓名快照(改名不回溯)',
        operator_role VARCHAR(64)   DEFAULT '' COMMENT '操作人角色快照',
        oper_ip       VARCHAR(64)   DEFAULT '' COMMENT '操作IP',
        target_type   VARCHAR(32)   DEFAULT '' COMMENT '对象类型',
        target_id     VARCHAR(64)   DEFAULT '' COMMENT '对象ID',
        oper_param    VARCHAR(2000) DEFAULT '' COMMENT '请求参数(脱敏后)',
        detail        VARCHAR(500)  DEFAULT '' COMMENT '结果详情',
        status        INTEGER       DEFAULT 0 COMMENT '结果 0失败 1成功',
        error_msg     VARCHAR(500)  DEFAULT '' COMMENT '失败原因',
        cost_ms       INTEGER       DEFAULT 0 COMMENT '耗时(毫秒)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='操作日志(审计留痕)';
CREATE TABLE IF NOT EXISTS tb_config (
        cfg_key   VARCHAR(64)   PRIMARY KEY COMMENT '配置键(主键)',
        cfg_value VARCHAR(4000) DEFAULT '' COMMENT '配置值(敏感项AES-256-GCM密文)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='系统配置(键值对)';
CREATE TABLE IF NOT EXISTS tb_day_seq (
        biz_date VARCHAR(10) PRIMARY KEY COMMENT '营业日YYYY-MM-DD(主键)',
        next_val INTEGER      DEFAULT 0 COMMENT '当日下一个取餐号',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='营业日取餐号计数器';
CREATE TABLE IF NOT EXISTS tb_order (
        order_id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '订单ID(自增主键)',
        order_no        VARCHAR(64)  NOT NULL COMMENT '订单号(D开头随机串,不可枚举)',
        table_id        INTEGER COMMENT '桌台ID',
        table_no        VARCHAR(32) COMMENT '桌号快照',
        table_name      VARCHAR(64) COMMENT '桌名快照',
        person_count    INTEGER      DEFAULT 1 COMMENT '用餐人数',
        order_status    INTEGER      DEFAULT 1 COMMENT '状态 1已下单 2制作中 3已上齐 4已完成 5已取消',
        dish_amount     INTEGER      DEFAULT 0 COMMENT '菜品金额(分)',
        seat_fee        INTEGER      DEFAULT 0 COMMENT '座位费(分)',
        discount_amount INTEGER      DEFAULT 0 COMMENT '优惠金额(分)',
        total_amount    INTEGER      DEFAULT 0 COMMENT '应收总额(分)',
        pay_status      INTEGER      DEFAULT 0 COMMENT '支付状态 0未支付 1已支付',
        pay_type        VARCHAR(16) COMMENT '支付方式(预留)',
        pay_time VARCHAR(32) DEFAULT NULL COMMENT '支付时间',
        transaction_id  VARCHAR(64)  DEFAULT '' COMMENT '第三方渠道交易号',
        pay_channel     VARCHAR(16)  DEFAULT '' COMMENT '支付渠道 wxpay/alipay/offline(码牌)/空',
        refund_amount   INTEGER      DEFAULT 0 COMMENT '累计退款金额(分)',
        refund_time VARCHAR(32) DEFAULT NULL COMMENT '最近退款时间',
        finish_time VARCHAR(32) DEFAULT NULL COMMENT '完成/取消时间',
        order_remark    VARCHAR(500) DEFAULT '' COMMENT '顾客订单备注',
        cancel_reason   VARCHAR(255) DEFAULT '' COMMENT '取消原因',
        create_by       VARCHAR(64)  DEFAULT '' COMMENT '创建人',
        update_by       VARCHAR(64)  DEFAULT '' COMMENT '更新人',
        remark          VARCHAR(500) COMMENT '商家备注',
        settle_type     VARCHAR(16)  DEFAULT 'normal' COMMENT '结算方式 normal正常 free免单 credit挂账',
        settle_time VARCHAR(32) DEFAULT NULL COMMENT '结算时间',
        settle_operator VARCHAR(64)  DEFAULT '' COMMENT '结算操作人',
        settle_remark   VARCHAR(255) DEFAULT '' COMMENT '结算备注',
        credit_status   INTEGER      DEFAULT 0 COMMENT '挂账状态 0非挂账 1待收款 2已结清',
        credit_amount   INTEGER      DEFAULT 0 COMMENT '挂账金额(分)',
        credit_settle_time VARCHAR(32) DEFAULT NULL COMMENT '挂账核销时间',
        credit_settle_by   VARCHAR(64) DEFAULT '' COMMENT '挂账核销人',
        paid_amount     INTEGER      DEFAULT 0 COMMENT '实收金额(分,免单=0)',
        -- 赠送金额(分):赠送菜品按原价计的金额合计,不计入应收金额(让利口径)。
        gift_amount     INTEGER      DEFAULT 0 COMMENT '赠送金额(分,让利口径不计应收)',
        -- 下单会员:0 = 匿名(历史单与未登录单);会员体系见 tb_customer。
        -- 请求体不带该值,一律以顾客令牌为准(防止伪造归属)。
        customer_id     INTEGER      DEFAULT 0 COMMENT '下单会员ID(0=匿名;以顾客令牌为准防伪造)',
        -- 结账时(状态被置 4 前)的订单状态快照:撤销结算时恢复到结账前的真实状态,
        -- 而不是无条件回到 3(订单可能在 1/2 状态被提前结账,撤销后应回到 1/2 继续制作流程)。
        -- 0 表示历史数据未记录(撤销时回退为 3)。
        pre_settle_status INTEGER     DEFAULT 0 COMMENT '结账前状态快照(0=未记录,撤销结算时恢复用)',
        -- 营业日(YYYY-MM-DD)与当日取餐号:专供顾客口头报号 / 厨房叫号,每天从 1 递增,
        -- 与不可枚举的内部单号 order_no 各司其职(见 tb_day_seq 的注释)。
        -- biz_date 缺省 NULL 而非空串的用意:让「非下单写入的行」不参与下面
        -- (biz_date, pickup_no) 的唯一判定 —— 两种数据库对含 NULL 的组合都不判重复。
        biz_date   VARCHAR(10) DEFAULT NULL COMMENT '营业日YYYY-MM-DD(NULL不参与取餐号唯一判定)',
        pickup_no  INTEGER     DEFAULT 0 COMMENT '当日取餐号(顾客报号/厨房叫号)',
        -- 用餐起止时间(落库值):
        --   meal_start_time —— 下单时写入(用餐开始);
        --   meal_end_time   —— 订单走到终态(4 已完成 / 5 已取消)时写入(用餐结束)。
        -- 默认 NULL 而非空串:区分「还没结束」与「时间未知」,回填脚本与报警 SQL 一眼明了。
        meal_start_time VARCHAR(32) DEFAULT NULL COMMENT '用餐开始时间(下单时写入)',
        meal_end_time VARCHAR(32) DEFAULT NULL COMMENT '用餐结束时间(订单到终态时写入)',
        -- 商家首次实际操作该订单的时间:NULL = 还没操作过(仅打开详情查看不算)。
        -- 列表据此给「已下单」的未操作订单打「新」角标,任意操作后写入,只写一次。
        viewed_at VARCHAR(32) DEFAULT NULL COMMENT '商家首次操作时间(NULL=新单,只写一次)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='订单主表';
CREATE TABLE IF NOT EXISTS tb_order_item (
        item_id   INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '明细ID(自增主键)',
        order_id  INTEGER      NOT NULL COMMENT '所属订单ID',
        dish_id   INTEGER COMMENT '菜品ID',
        dish_name VARCHAR(128) COMMENT '菜名快照(菜品后改不回溯)',
        spec_id   INTEGER COMMENT '规格ID',
        spec_name VARCHAR(64) COMMENT '规格名快照',
        price     INTEGER      DEFAULT 0 COMMENT '下单时单价(分)',
        quantity  INTEGER      DEFAULT 1 COMMENT '数量',
        amount    INTEGER      DEFAULT 0 COMMENT '小计(分)',
        remark    VARCHAR(255) DEFAULT '' COMMENT '单品备注',
        -- 加菜批次:1=首次下单,每次加菜 +1,用于菜品明细里区分「首次下单/第N次加菜」。
        batch_no  INTEGER      DEFAULT 1 COMMENT '加菜批次(1=首次下单,每次加菜+1)',
        -- 幂等键:同一次下单/加菜提交的所有明细共享同一 client_request_id,用于去重(适当冗余)。
        client_request_id VARCHAR(64) DEFAULT '' COMMENT '幂等键(同次下单/加菜提交共享,去重用)',
        -- 是否赠送:0 正常计费 1 赠送(商家改单标记,amount 记 0,原价计入订单 gift_amount)。
        is_gift   INTEGER      DEFAULT 0 COMMENT '赠送标记 1赠送(amount记0,原价计入gift_amount)',
        gift_reason VARCHAR(255) DEFAULT '' COMMENT '赠送原因',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='订单明细';
CREATE TABLE IF NOT EXISTS tb_order_urge (
        urge_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '记录ID(自增主键)',
        order_id    INTEGER      NOT NULL COMMENT '关联订单ID',
        order_no    VARCHAR(64)  NOT NULL COMMENT '关联订单号',
        table_id    INTEGER      NOT NULL COMMENT '桌台ID',
        table_no    VARCHAR(32)  DEFAULT '' COMMENT '桌号快照',
        table_name  VARCHAR(64)  DEFAULT '' COMMENT '桌名快照',
        urge_type   VARCHAR(16)  DEFAULT 'urge' COMMENT '类型 urge=催菜',
        status      INTEGER      DEFAULT 0 COMMENT '处理状态 0待处理 1已处理',
        remark      VARCHAR(255) DEFAULT '' COMMENT '处理备注',
        handle_time VARCHAR(32) DEFAULT NULL COMMENT '处理时间',
        handle_by   VARCHAR(64)  DEFAULT '' COMMENT '处理人',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='催菜记录';
CREATE TABLE IF NOT EXISTS tb_payment (
        payment_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '支付单ID(自增主键)',
        order_no         VARCHAR(64)  NOT NULL COMMENT '关联订单号',
        channel          VARCHAR(16)  NOT NULL COMMENT '支付渠道 wxpay/alipay',
        channel_trade_no VARCHAR(64)  DEFAULT '' COMMENT '渠道交易号',
        amount           INTEGER      DEFAULT 0 COMMENT '支付金额(分)',
        status           INTEGER      DEFAULT 0 COMMENT '0待支付 1已支付 2已关闭 3退款中 4已退款 5退款失败',
        prepay_id        VARCHAR(128) DEFAULT '' COMMENT '微信预支付ID',
        notify_time VARCHAR(32) DEFAULT NULL COMMENT '回调通知时间',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='支付流水';
CREATE TABLE IF NOT EXISTS tb_refund (
        refund_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '退款单ID(自增主键)',
        order_id          INTEGER      NOT NULL COMMENT '关联订单ID',
        order_no          VARCHAR(64)  NOT NULL COMMENT '关联订单号',
        payment_id        INTEGER      NOT NULL COMMENT '关联支付单ID',
        refund_no         VARCHAR(64)  NOT NULL COMMENT '退款单号',
        channel           VARCHAR(16)  NOT NULL COMMENT '支付渠道',
        channel_refund_no VARCHAR(64)  DEFAULT '' COMMENT '渠道退款单号',
        amount            INTEGER      DEFAULT 0 COMMENT '退款金额(分)',
        status            INTEGER      DEFAULT 0 COMMENT '0处理中 1成功 2失败',
        reason            VARCHAR(255) DEFAULT '' COMMENT '退款原因',
        operator          VARCHAR(64)  DEFAULT '' COMMENT '操作人',
        fail_reason       VARCHAR(255) DEFAULT '' COMMENT '失败原因',
        -- 1 = 重复支付自动退回(顾客在两个渠道各付了一笔,后到的一笔原路退回)。
        -- 这类退款的金额从未计入订单营收(paid_amount),因此不计入订单退款额度、
        -- 也不扣减订单实收 —— 与普通退款(SumRefunded/applyRefundSuccess)严格区分。
        is_duplicate      INTEGER      DEFAULT 0 COMMENT '1=重复支付自动退回(不计营收不占退款额度)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='退款流水';
CREATE TABLE IF NOT EXISTS tb_role (
        role_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '角色ID(自增主键)',
        role_key    VARCHAR(32)   NOT NULL COMMENT '角色标识(全局唯一,含已删除行)',
        role_name   VARCHAR(64)   NOT NULL COMMENT '角色名称',
        perms       VARCHAR(4000) DEFAULT '' COMMENT '权限点CSV(权限矩阵见 permission.go)',
        data_scope  VARCHAR(16)   DEFAULT 'all' COMMENT '数据范围(当前仅all)',
        is_builtin  INTEGER       DEFAULT 0 COMMENT '内置角色标记(启动时校准)',
        sort_order  INTEGER       DEFAULT 0 COMMENT '排序值(小在前)',
        status      INTEGER       DEFAULT 1 COMMENT '1启用 0停用',
        del_flag    VARCHAR(2)    DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        remark      VARCHAR(500)  DEFAULT '' COMMENT '备注',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='角色(权限点集合)';
CREATE TABLE IF NOT EXISTS tb_user (
        user_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '员工ID(自增主键)',
        username        VARCHAR(64)  NOT NULL COMMENT '登录名(未删除行内唯一)',
        password_hash   VARCHAR(255) DEFAULT '' COMMENT '密码bcrypt哈希',
        real_name       VARCHAR(64)  DEFAULT '' COMMENT '姓名(订单留痕展示用)',
        role_id         INTEGER      NOT NULL DEFAULT 0 COMMENT '角色ID',
        phone           VARCHAR(32)  DEFAULT '' COMMENT '手机号',
        status          INTEGER      DEFAULT 1 COMMENT '1启用 0停用(停用即失去访问权)',
        last_login_time VARCHAR(32) DEFAULT NULL COMMENT '最近登录时间',
        last_login_ip   VARCHAR(64)  DEFAULT '' COMMENT '最近登录IP',
        login_count     INTEGER      DEFAULT 0 COMMENT '登录次数',
        pwd_update_time VARCHAR(32) DEFAULT NULL COMMENT '密码修改时间',
        token_version   INTEGER      DEFAULT 0 COMMENT '令牌版本(改密/强制下线+1使旧令牌失效)',
        del_flag        VARCHAR(2)   DEFAULT '0' COMMENT '软删除标记 0正常 1已删除',
        create_by       VARCHAR(64)  DEFAULT '' COMMENT '创建人',
        update_by       VARCHAR(64)  DEFAULT '' COMMENT '更新人',
        remark          VARCHAR(500) DEFAULT '' COMMENT '备注',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='员工账号';
CREATE TABLE IF NOT EXISTS tb_customer (
        customer_id      INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '会员ID(自增主键)',
        nickname         VARCHAR(64)  DEFAULT '' COMMENT '昵称(空时前端显示微信用户)',
        avatar_url       VARCHAR(255) DEFAULT '' COMMENT '头像路径',
        status           INTEGER      DEFAULT 1 COMMENT '1正常 0停用',
        order_count      INTEGER      DEFAULT 0 COMMENT '累计订单数',
        last_order_time VARCHAR(32) DEFAULT NULL COMMENT '最近下单时间',
        last_login_time VARCHAR(32) DEFAULT NULL COMMENT '最近登录时间',
        remark           VARCHAR(255) DEFAULT '' COMMENT '备注',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='会员档案(账号与登录方式解耦)';
CREATE TABLE IF NOT EXISTS tb_customer_identity (
        identity_id      INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '身份ID(自增主键)',
        customer_id      INTEGER      NOT NULL COMMENT '会员ID',
        provider         VARCHAR(32)  NOT NULL COMMENT '登录方式 wx_mp/wx_oa/wx_app/phone/password/apple',
        external_id      VARCHAR(128) NOT NULL COMMENT '该方式下的用户标识(如openid),(provider,external_id)唯一',
        credential       VARCHAR(255) DEFAULT '' COMMENT '登录凭据(预留)',
        unionid          VARCHAR(64)  DEFAULT NULL COMMENT '微信unionid(多端归并,非唯一)',
        last_login_time DATETIME DEFAULT NULL COMMENT '最近登录时间',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='会员登录方式';
CREATE TABLE IF NOT EXISTS tb_wallet (
        customer_id    INTEGER PRIMARY KEY COMMENT '会员ID(主键,1:1)',
        balance        INTEGER      DEFAULT 0 COMMENT '余额(分,只由事务内DAO增减,守卫不为负)',
        total_recharge INTEGER      DEFAULT 0 COMMENT '累计充值(分)',
        total_consume  INTEGER      DEFAULT 0 COMMENT '累计消费(分)',
        total_gift     INTEGER      DEFAULT 0 COMMENT '累计赠送(分)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='会员储值账户';
CREATE TABLE IF NOT EXISTS tb_wallet_txn (
        txn_id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '流水ID(自增主键)',
        customer_id    INTEGER      NOT NULL COMMENT '会员ID',
        biz_type       VARCHAR(16)  NOT NULL COMMENT '业务类型 recharge/consume/refund/gift/adjust',
        amount         INTEGER      NOT NULL COMMENT '变动金额(分,带符号,正入负出)',
        balance_after  INTEGER      NOT NULL COMMENT '变动后余额(分,逐行累加必须等于当前余额)',
        order_no       VARCHAR(32)  DEFAULT '' COMMENT '关联订单号',
        payment_no     VARCHAR(64)  DEFAULT '' COMMENT '关联支付单号',
        reason         VARCHAR(255) DEFAULT '' COMMENT '变动原因',
        operator       VARCHAR(64)  DEFAULT '' COMMENT '后台操作人(顾客自助操作为空)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='储值流水(只增不改)';
CREATE TABLE IF NOT EXISTS tb_recharge (
        recharge_id      INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '充值单ID(自增主键)',
        recharge_no      VARCHAR(32)  NOT NULL COMMENT '充值单号(R开头,与订单D开头靠前缀分流回调)',
        customer_id      INTEGER      NOT NULL COMMENT '会员ID',
        amount           INTEGER      NOT NULL COMMENT '充值金额(分)',
        status           INTEGER      DEFAULT 0 COMMENT '0待支付 1已入账(回调条件更新做幂等)',
        channel          VARCHAR(16)  DEFAULT '' COMMENT '支付渠道',
        channel_trade_no VARCHAR(64)  DEFAULT '' COMMENT '渠道交易号',
        pay_time        VARCHAR(32) DEFAULT NULL COMMENT '支付时间',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='储值充值单';
CREATE TABLE IF NOT EXISTS tb_remember_token (
        token_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '令牌ID(自增主键)',
        user_id        INTEGER      NOT NULL COMMENT '员工ID',
        token          VARCHAR(64)  NOT NULL COMMENT '令牌SHA-256(库内不落明文,查库裁决过期)',
        token_prefix   VARCHAR(8)   DEFAULT '' COMMENT '令牌前8位(设备管理页识别)',
        expire_time VARCHAR(32) DEFAULT NULL COMMENT '过期时间(7/30天免登录)',
        last_used_time VARCHAR(32) DEFAULT NULL COMMENT '最近使用时间',
        ua             VARCHAR(255) DEFAULT '' COMMENT '环境指纹(User-Agent)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='记住登录令牌';
CREATE TABLE IF NOT EXISTS tb_image (
        img_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '图片ID(自增主键)',
        img_name     VARCHAR(128) NOT NULL COMMENT '文件名(唯一,业务表以 /uploads/ 路径引用)',
        content_type VARCHAR(32)  DEFAULT '' COMMENT 'MIME类型',
        img_data     LONGBLOB COMMENT '图片二进制(单张上限64MB)',
        file_size    INTEGER      DEFAULT 0 COMMENT '字节数',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='图片内容(菜品图/收款码随库走)';
CREATE TABLE IF NOT EXISTS tb_visit_log (
        visit_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '明细ID(自增主键)',
        visit_date  VARCHAR(10)  NOT NULL COMMENT '访问日期YYYY-MM-DD',
        channel     VARCHAR(16)  NOT NULL COMMENT '渠道 h5/miniprogram/admin',
        visitor_key VARCHAR(80)  NOT NULL COMMENT '访客去重键 m:会员 c:前端ID a:匿名哈希',
        is_member   INTEGER      DEFAULT 0 COMMENT '是否会员 1是 0否',
        customer_id INTEGER      DEFAULT 0 COMMENT '会员ID',
        entry       VARCHAR(16)  DEFAULT '' COMMENT '入口标识(H5为桌台码,小程序一期留空)',
        ip_hash     VARCHAR(64)  DEFAULT '' COMMENT 'IP哈希前32位(UV去重用,不可反推)',
        ua_hash     VARCHAR(64)  DEFAULT '' COMMENT 'UA哈希前32位(UV去重用,不可反推)',
        -- 终端与来源信息(2026-10-18 增补):IP 明文用于排查与展示(明细 90 天清理,
        -- 与操作日志 oper_ip 明文同一取舍);城市来自 ip2region 离线库
        -- (data/ip2region.xdb,缺文件时为空);OS/浏览器/设备类型为归一后的展示值
        -- (iOS/微信/手机…),机型安卓 UA 可解出型号,iOS 的 UA 不含机型存空,
        -- 小程序端上直传最准。
        ip          VARCHAR(45)  DEFAULT '' COMMENT '明文IP(排查展示用,随明细90天清理)',
        city        VARCHAR(64)  DEFAULT '' COMMENT '城市(ip2region离线库解析,缺文件为空)',
        os          VARCHAR(16)  DEFAULT '' COMMENT '操作系统(归一展示值,小程序直传最准)',
        browser     VARCHAR(16)  DEFAULT '' COMMENT '浏览器(归一展示值)',
        device      VARCHAR(8)   DEFAULT '' COMMENT '设备类型(手机/平板/PC)',
        model       VARCHAR(64)  DEFAULT '' COMMENT '机型(安卓UA可解出型号,iOS为空)',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='访问明细(待办性质,按保留期清理)';
CREATE TABLE IF NOT EXISTS tb_visitor (
        visitor_key      VARCHAR(80) PRIMARY KEY COMMENT '访客键(主键,天然去重)',
        first_visit_date VARCHAR(10) NOT NULL COMMENT '首访日期(等于统计日即当日新访客)',
        last_visit_date  VARCHAR(10) NOT NULL COMMENT '最近访问日期',
        visit_count      INTEGER     DEFAULT 1 COMMENT '累计访问次数',
        is_member        INTEGER     DEFAULT 0 COMMENT '是否会员 1是 0否',
        customer_id      INTEGER     DEFAULT 0 COMMENT '会员ID',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='访客注册表(新访客精确口径)';
CREATE TABLE IF NOT EXISTS tb_visit_daily (
        id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '行ID(自增主键)',
        visit_date VARCHAR(10) NOT NULL COMMENT '统计日期YYYY-MM-DD',
        channel    VARCHAR(16) NOT NULL COMMENT '渠道(all=跨渠道去重的全站合计行)',
        pv         INTEGER     DEFAULT 0 COMMENT '浏览量(PV)',
        uv         INTEGER     DEFAULT 0 COMMENT '独立访客数(UV)',
        new_uv     INTEGER     DEFAULT 0 COMMENT '新访客数',
        member_uv  INTEGER     DEFAULT 0 COMMENT '会员访客数',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='访问日聚合台账(报表主读)';
CREATE UNIQUE INDEX idx_order_no ON tb_order(order_no);
CREATE UNIQUE INDEX idx_order_pickup ON tb_order(biz_date, pickup_no);
CREATE INDEX idx_order_table ON tb_order(table_id);
CREATE INDEX idx_order_status ON tb_order(order_status);
CREATE INDEX idx_order_table_unpaid ON tb_order(table_id, pay_status, order_status);
CREATE INDEX idx_order_create_time ON tb_order(create_time);
CREATE INDEX idx_order_pay_time ON tb_order(pay_time);
CREATE INDEX idx_order_credit_settle_time ON tb_order(credit_settle_time);
CREATE INDEX idx_order_customer ON tb_order(customer_id, create_time);
CREATE UNIQUE INDEX idx_identity_provider_external ON tb_customer_identity(provider, external_id);
CREATE INDEX idx_identity_customer ON tb_customer_identity(customer_id);
CREATE UNIQUE INDEX idx_recharge_no ON tb_recharge(recharge_no);
CREATE INDEX idx_wallet_txn_customer ON tb_wallet_txn(customer_id, txn_id);
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
