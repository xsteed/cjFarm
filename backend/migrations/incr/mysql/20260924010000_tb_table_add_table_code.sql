-- ============================================================================
-- 增量迁移 20260924010000：桌台表补充「点餐稳定码」列（MySQL）
-- 文件名：20260924010000_tb_table_add_table_code.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_table_code)
-- 表：tb_table    动作：alter（新增 1 列）+ backfill（历史数据回填）
-- ----------------------------------------------------------------------------
-- 背景：二维码内容从自增 ID 改为每桌生成一次、永不变更的 8 位随机稳定码
--       （剔除 0/O/1/I/L 等易混字符），防止自增 ID 被枚举遍历。
-- 适用：早期版本、tb_table 尚无 table_code 列的 MySQL 库。
--       （此前该列仅靠启动时运行时迁移兜底，补此脚本与其余加列惯例一致。）
--
-- ⚠️ 幂等性说明：MySQL 不支持 ADD COLUMN IF NOT EXISTS（MariaDB 支持）。
--      若列已存在会报 "Duplicate column name"，该报错可忽略。
-- 执行：mysql -u dining -p dining < 20260924010000_tb_table_add_table_code.sql
-- ============================================================================

SET NAMES utf8mb4;

ALTER TABLE tb_table ADD COLUMN table_code VARCHAR(16) DEFAULT '';

-- 历史桌台在启动时由 backfillTableCodes() 补发随机码（需 Go 运行时），
-- 纯 SQL 侧无法生成随机码，这里不写回填语句。
