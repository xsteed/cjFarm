-- ============================================================================
-- 增量迁移 20261010000000：菜品表补充「招牌菜」标记（MySQL）
-- 文件名：20261010000000_tb_dish_add_signature.sql
--         命名规则 = 日期时间(YYYYMMDDHHMMSS) _ 表名 _ 动作(add_signature)
-- 表：tb_dish    动作：alter（新增 1 列）
-- ----------------------------------------------------------------------------
-- 背景：店长可在后台把菜品设为「招牌菜」，顾客端(H5/小程序)在菜品图片左上角
--       渲染「招牌」角标，帮助顾客快速识别镇店菜品。
-- 适用：早期版本、tb_dish 表尚无 signature 列的库。
--
-- ⚠️ 幂等性说明：MySQL 不支持 ADD COLUMN IF NOT EXISTS，列已存在时会报
--      "Duplicate column name"（可忽略）。
-- 执行：mysql -u dining -p dining < 20261010000000_tb_dish_add_signature.sql
-- ============================================================================

SET NAMES utf8mb4;

ALTER TABLE tb_dish ADD COLUMN signature INT DEFAULT 0 COMMENT '0 普通 1 招牌菜';
