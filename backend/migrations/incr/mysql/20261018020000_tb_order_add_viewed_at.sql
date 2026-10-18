-- 订单「商家已操作」时间落库（incr 34）
--
-- 背景：订单列表需要区分「顾客刚下、商家还没操作过」的新单与已经处理过的单，
-- 让收银一眼看到哪些新单还没处理（仅打开详情查看不算操作）。
--
-- 口径：
--   viewed_at —— 商家对订单做过任意实际操作
--                （制作/上菜/收款/结账/取消/完成/改单/催办/补打/退款）时写入；
--                NULL 表示新单，还没操作过；
--                只写首次时间（UPDATE 带 viewed_at IS NULL 守卫），后续操作不改写。
--
-- 回填：升级前的存量订单商家早已处理过，统一按「已查看」回填（取 update_time，
--       无则 create_time），避免升级后列表整屏挂「新」角标。
--
-- 适用范围：仅用于存量库升级。新库请直接执行 migrations/full/{mysql,sqlite}/schema.sql
-- （建表语句已包含该列）。后端启动时 store.migrate 也会补列并做同样的回填。
ALTER TABLE tb_order ADD COLUMN viewed_at VARCHAR(32) DEFAULT NULL AFTER meal_end_time;

UPDATE tb_order SET viewed_at = COALESCE(update_time, create_time) WHERE viewed_at IS NULL;
-- 空串 / MySQL 历史零值（'0000-00-00 …'）都会被读侧折叠成 NULL，同样会挂「新」角标，
-- 统一用哨兵时间兜底。
UPDATE tb_order SET viewed_at = '1970-01-01 00:00:00'
	WHERE viewed_at IS NULL OR viewed_at = '' OR viewed_at LIKE '0000-00-00%';
