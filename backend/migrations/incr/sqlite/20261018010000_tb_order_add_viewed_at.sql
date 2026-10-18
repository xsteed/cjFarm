-- 订单「商家已查看」时间落库（incr 33）
--
-- 背景：订单列表需要区分「顾客刚下、商家还没看过」的新单与已经处理过的单，
-- 让收银一眼看到哪些新单还没点开/操作过（此前只能靠 order_status=1 猜）。
--
-- 口径：
--   viewed_at —— 商家第一次打开订单详情，或对订单做过任意操作
--                （制作/上菜/收款/结账/取消/完成/改单/催办/补打）时写入；
--                NULL 表示新单，还没人看过；
--                只写首次时间（UPDATE 带 viewed_at IS NULL 守卫），后续查看不改写。
--
-- 回填：升级前的存量订单商家早已处理过，统一按「已查看」回填（取 update_time，
--       无则 create_time），避免升级后列表整屏挂「新」角标。
--
-- 适用范围：仅用于存量库升级。新库请直接执行 migrations/full/{mysql,sqlite}/schema.sql
-- （建表语句已包含该列）。后端启动时 store.migrate 也会补列并做同样的回填。
ALTER TABLE tb_order ADD COLUMN viewed_at VARCHAR(32) DEFAULT NULL;

UPDATE tb_order SET viewed_at = COALESCE(update_time, create_time) WHERE viewed_at IS NULL;
-- 连 create_time 都为空的历史脏行：用哨兵时间兜底，否则空串会被读侧当成 NULL。
UPDATE tb_order SET viewed_at = '1970-01-01 00:00:00' WHERE viewed_at IS NULL OR viewed_at = '';
