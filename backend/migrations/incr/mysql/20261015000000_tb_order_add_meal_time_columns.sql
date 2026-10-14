-- 订单用餐起止时间落库（incr 26）
--
-- 背景：桌台页「已用餐」与报表「用餐时长分析」此前都是由 create_time / finish_time
-- 现算出来的——看得见，却没沉淀下来，既无法回溯「这桌具体坐了多久」，长期趋势也只能
-- 每次全量扫描订单。
--
-- 口径：
--   meal_start_time —— 下单那一刻写入（用餐开始），等同于当时的 create_time；
--   meal_end_time   —— 订单走到终态（4 已完成 / 5 已取消）时写入（用餐结束）；
--   撤销结算时 meal_end_time 与 finish_time 一并清空（订单回到进行中）。
--
-- 历史行由本脚本按 create_time / finish_time 回填；读侧 COALESCE 回落旧列，
-- 因此升级前后统计口径完全一致。类型为 NULL-able，历史未知保持 NULL 而非空串。
--
-- 适用范围：仅用于存量库升级。新库请直接执行 migrations/full/{mysql,sqlite}/schema.sql
-- （建表语句已包含这两列）。MySQL 的 ADD COLUMN 不支持 IF NOT EXISTS，
-- 在已有该列的库上重复执行会报 Duplicate column 中断。
ALTER TABLE tb_order ADD COLUMN meal_start_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_order ADD COLUMN meal_end_time VARCHAR(32) DEFAULT NULL;

-- 回填已完成的订单：起点取下单时间，终点取完成时间（有的话）。
UPDATE tb_order SET meal_start_time = create_time
	WHERE meal_start_time IS NULL AND COALESCE(create_time,'') <> '';
UPDATE tb_order SET meal_end_time = finish_time
	WHERE meal_end_time IS NULL AND order_status IN (4,5) AND COALESCE(finish_time,'') <> '';
