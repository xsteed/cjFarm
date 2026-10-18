-- 储值合规改造（2026-10-19）：本金 / 赠金分桶 + 《储值规则》同意留痕 + 充值单冻结赠送额
--
-- 变更内容：
--   tb_wallet.balance_gift          赠金桶（分）
--   tb_wallet_txn.gift_amount       本笔变动中属于赠金的部分（带符号）
--   tb_wallet_txn.gift_after        变动后赠金余额（分）
--   tb_recharge.gift_amount         建单时冻结的赠送额（分）
--   tb_wallet_agreement（新表）     《储值规则》同意留痕（会员 + 版本 + 时间）
--
-- 为什么必须分桶：混在一个数字里，退余额时无法回答「这笔钱该不该退给顾客」，
-- 只能一刀切全退 —— 于是「充 100 送 10 → 用赠金买单 → 退款拿回本金」可以无限套现。
-- 详见 docs/design-docs/customer-wallet/spec.md。
--
-- 老库数据口径（不回填，原因写在这里以免日后被当成遗漏）：
--   1. 存量余额全部落在本金桶，赠金从 0 起算。分桶上线前没有赠送功能，
--      不存在「需要拆开却拆不开」的数据；这也与「历史消费全额可退」的既有语义一致。
--   2. 历史流水的 gift_amount / gift_after 补 0，等价于「历史消费不含赠金」，
--      退款时可退本金上限 = 该单消费总额，行为与升级前完全一致。
--   3. 历史充值单 gift_amount 补 0（当时没有赠送活动）。
--   4. 老会员没有规则同意记录，**不做批量回填**：回填等于替顾客签字，
--      反而让「他充值时看到的是哪一版」这条留痕失去意义。
--      预期行为是老会员首次充值时被要求确认一次。
--
-- 幂等与重复执行：
--   - ALTER TABLE ADD COLUMN 两库都不支持 IF NOT EXISTS，列已存在时报
--     Duplicate column name，属正常现象、可忽略（见 migrations/README.md 第三节）。
--   - CREATE TABLE 带 IF NOT EXISTS，可安全重复执行。
--   - 本脚本与后端启动时 store.alterCols 的自动补列等价，执行其一即可，
--     两者都跑也不会出错（自动补列会跳过已存在的列）。
--
-- ⚠️ MySQL 加列必须带 AFTER（migrations/README.md 第七节第 7 条）：
--    否则新列会追加到表末尾，与 store.alterCols 补列后的列顺序对不上。
--    AFTER 取值以 full/mysql/schema.sql 中该列的实际位置为准。

ALTER TABLE tb_wallet ADD COLUMN balance_gift INTEGER DEFAULT 0 COMMENT '赠金余额(分,充值赠送/活动赠送,只可消费不可退现)' AFTER balance;
ALTER TABLE tb_wallet_txn ADD COLUMN gift_amount INTEGER DEFAULT 0 COMMENT '本笔变动中赠金部分(分,带符号;退款据此算可退本金)' AFTER balance_after;
ALTER TABLE tb_wallet_txn ADD COLUMN gift_after INTEGER DEFAULT 0 COMMENT '变动后赠金余额(分)' AFTER gift_amount;
ALTER TABLE tb_recharge ADD COLUMN gift_amount INTEGER DEFAULT 0 COMMENT '本笔赠送额(分,建单时冻结,入账进赠金桶)' AFTER amount;

CREATE TABLE IF NOT EXISTS tb_wallet_agreement (
        customer_id    INTEGER PRIMARY KEY COMMENT '会员ID(主键,1:1)',
        rules_ver      VARCHAR(16)  NOT NULL COMMENT '同意的规则版本号',
        agreed_time DATETIME DEFAULT NULL COMMENT '同意时间',
        create_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间(数据库自动填)',
        update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='储值规则同意留痕(合规:发卡须明示章程)';
