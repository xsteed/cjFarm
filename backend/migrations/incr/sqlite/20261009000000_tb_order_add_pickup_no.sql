-- ============================================================================
-- 增量迁移 20261009000000：订单营业日与当日取餐号
-- 文件名：20261009000000_tb_order_add_pickup_no.sql
-- 表：tb_order（新增 2 列 + 1 索引）/ tb_day_seq（新建表）    动作：alter + create
-- ----------------------------------------------------------------------------
-- 背景：
--   1. 内部单号 order_no 是不可枚举的 20 位随机串（D + 秒级时间 + 35bit 随机数），
--      因为顾客端 /customer/order/:orderNo 免登录即可凭单号查单，一旦换成自增或
--      雪花这类线性编号，等于开放对全量订单的遍历枚举 —— 这个性质必须保留。
--   2. 但 20 位随机串无法口头报读，顾客也没法向服务员报号、厨房更没法叫号。
--      故另设一套「当天从 1 递增」的短号，与订单号互不干涉、各司其职。
-- 设计：
--   tb_order.biz_date     —— 营业日 'YYYY-MM-DD'，取餐号的分区键。
--   tb_order.pickup_no    —— 当日取餐号，从 1 递增，供顾客报号与厨房叫号。
--   tb_day_seq            —— 按营业日维护 next_val 的计数器表；取号在下单事务内
--                            由该行的 UPDATE 行锁串行化（见 dao.NextPickupNoTx），
--                            事务回滚则号随之回滚，不会出现「号被占用却没订单」。
--   idx_order_pickup      —— (biz_date, pickup_no) 唯一索引，作为兜底防线。
--                            biz_date 缺省 NULL 而非空串：两库对含 NULL 的组合都不
--                            判重复，手工补录的历史行（不带这两列）不会被误伤。
-- 历史数据：本脚本不回填。早期订单 pickup_no 保持 0，
--           展示时由 po.DisplayNo 退回「从 order_no 派生的尾 6 位短号」。
-- 适用：早期版本、尚无取餐号列的 SQLite 库。
-- ⚠️ 幂等性：ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS，列已存在时会报
--      "duplicate column name"（可忽略）；建表与 CREATE INDEX 带 IF NOT EXISTS 可重复执行。
-- 执行：sqlite3 dining.db < 20261009000000_tb_order_add_pickup_no.sql
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_day_seq (
    biz_date VARCHAR(10) PRIMARY KEY,
    next_val INTEGER     DEFAULT 0
);

ALTER TABLE tb_order ADD COLUMN biz_date  VARCHAR(10) DEFAULT NULL;
ALTER TABLE tb_order ADD COLUMN pickup_no INTEGER     DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_pickup ON tb_order(biz_date, pickup_no);
