-- 30 号:清理 tb_order 遗留的 begin_time / end_time 两列。
--
-- 这两列是早期「用餐起止」的尝试,后来被 meal_start_time / meal_end_time 取代
-- (26 号脚本)。业务代码从未写入(全库搜索确认只有读取透传),长期为 NULL,
-- 留着只会误导排障(详见 tb_order 表时间字段整理记录)。
--
-- SQLite 3.35+ 支持 ALTER TABLE DROP COLUMN;项目内置 modernc.org/sqlite v1.34.1
-- (SQLite 3.46)满足。两列均无索引/触发器引用,可直接删。
ALTER TABLE tb_order DROP COLUMN begin_time;
ALTER TABLE tb_order DROP COLUMN end_time;
