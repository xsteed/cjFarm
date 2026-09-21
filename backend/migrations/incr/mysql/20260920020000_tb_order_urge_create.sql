-- ============================================================================
-- 增量迁移 20260920020000：新增催菜表（MySQL）
-- 文件名：20260920020000_tb_order_urge_create.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(create)
-- 表：tb_order_urge    动作：create（新建表 + 索引）
-- ----------------------------------------------------------------------------
-- 背景：为顾客端「催菜」功能新增 tb_order_urge 表（含看板红角标 / 订单列表
--       标签 / 详情横幅），记录催菜与加菜请求及其处理状态。
-- 适用：已运行过早期版本、尚无 tb_order_urge 表的 MySQL 库。
-- 幂等：建表使用 IF NOT EXISTS，可重复执行；
--       建索引无 IF NOT EXISTS 语法，重复执行会报 'Duplicate key name'，可忽略。
-- 执行：mysql -u dining -p dining < 20260920020000_tb_order_urge_create.sql
-- ============================================================================

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS tb_order_urge (
    urge_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    order_id    INT NOT NULL,
    order_no    VARCHAR(64)  NOT NULL,
    table_id    INT NOT NULL,
    table_no    VARCHAR(32)  DEFAULT '',
    table_name  VARCHAR(64)  DEFAULT '',
    urge_type   VARCHAR(16)  DEFAULT 'urge',   -- urge 催菜 / add 加菜
    status      INT          DEFAULT 0,        -- 0 待处理 / 1 已处理
    remark      VARCHAR(255) DEFAULT '',
    create_time VARCHAR(32),
    handle_time VARCHAR(32),
    handle_by   VARCHAR(64)  DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE INDEX idx_urge_status ON tb_order_urge(status, create_time);
CREATE INDEX idx_urge_order  ON tb_order_urge(order_id);
