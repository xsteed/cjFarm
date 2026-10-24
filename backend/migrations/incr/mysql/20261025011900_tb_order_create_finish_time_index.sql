-- 报表按「完成时间」归属的查询补索引（2026-10-25）：tb_order 建 idx_order_finish_time。
--
-- 背景：报表页的「已完成订单数」（dao.CountFinishedIn）与「用户用餐时长」
-- （dao.DiningDurationRows）都按 tb_order.finish_time 的区间归属，而 schema 里只有
-- create_time / pay_time / credit_settle_time 三个时间索引，这两个查询此前只能全表
-- 扫订单主表 —— 订单量涨上去之后它就是整页报表最慢的一块。
--
-- 为什么是 (order_status, finish_time) 复合而不是单列：两个查询的过滤条件都是
-- 「order_status = 4」+「finish_time 区间」，等值列必须排在范围列之前，BTree 才能
-- 先收敛到 status=4 再沿 finish_time 做范围扫描；写成单列、或把 finish_time 放前面，
-- 都吃不到这层收敛。
--
-- 幂等与重复执行：
--   - MySQL 不支持 CREATE INDEX IF NOT EXISTS，重复执行会报 Duplicate key name，
--     属正常现象、可忽略（见 migrations/README.md 第二节）。
--   - 本脚本与「后端启动时按 indexDefs 幂等补建索引」等价，执行其一即可；
--     两者都跑也不会出错（启动时按索引名跳过已存在的索引）。
--   - 大表（tb_order）建索引会重建索引，生产库请在低峰执行。

CREATE INDEX idx_order_finish_time ON tb_order(order_status, finish_time);
