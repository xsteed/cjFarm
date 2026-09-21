-- ============================================================================
-- 增量迁移 20260920020000：新增催菜表
-- 文件名：20260920020000_tb_order_urge_create.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(create)
-- 表：tb_order_urge    动作：create（新建表 + 索引）
-- ----------------------------------------------------------------------------
-- 背景：为顾客端「催菜」功能新增 tb_order_urge 表（含看板红角标 / 订单列表
--       标签 / 详情横幅），记录催菜与加菜请求及其处理状态。
-- 适用：已运行过早期版本、尚无 tb_order_urge 表的库。
-- 幂等：本脚本使用 IF NOT EXISTS，可安全重复执行。
-- 执行：sqlite3 dining.db < 20260920020000_tb_order_urge_create.sql
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_order_urge (
    urge_id     INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL,
    order_no    TEXT NOT NULL,
    table_id    INTEGER NOT NULL,
    table_no    TEXT DEFAULT '',
    table_name  TEXT DEFAULT '',
    urge_type   TEXT DEFAULT 'urge',        -- urge 催菜 / add 加菜
    status      INTEGER DEFAULT 0,          -- 0 待处理 / 1 已处理
    remark      TEXT DEFAULT '',
    create_time TEXT,
    handle_time TEXT,
    handle_by   TEXT DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_urge_status ON tb_order_urge(status, create_time);
CREATE INDEX IF NOT EXISTS idx_urge_order  ON tb_order_urge(order_id);
