-- ============================================================================
-- 增量迁移 20261009010000：桌台表补充「人工可用性状态」列（SQLite）
-- 文件名：20261009010000_tb_table_add_lock_status.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_lock_status)
-- 表：tb_table    动作：alter（新增 2 列）
-- ----------------------------------------------------------------------------
-- 背景：除订单自动流转的占用状态（status）外，还需要人工把某张桌台设为
--       「停用（不可用）」或「预约」，避免被其他客人扫码占用：
--         lock_status  0 正常 / 1 停用(不可用) / 2 预约
--         lock_remark  停用原因或预约人/时段备注
--       两列与 status 正交：订单完成/取消只改 status，不会冲掉人工状态。
-- 适用：早期版本、tb_table 尚无 lock_status 列的库。
--
-- ⚠️ 幂等性说明：SQLite 不支持 ADD COLUMN IF NOT EXISTS，列已存在时会报
--      "duplicate column name"（可忽略）。
-- 执行：sqlite3 data/dining.db < 20261009010000_tb_table_add_lock_status.sql
-- ============================================================================

ALTER TABLE tb_table ADD COLUMN lock_status INTEGER      DEFAULT 0;   -- 0 正常 1 停用 2 预约
ALTER TABLE tb_table ADD COLUMN lock_remark VARCHAR(255) DEFAULT '';
