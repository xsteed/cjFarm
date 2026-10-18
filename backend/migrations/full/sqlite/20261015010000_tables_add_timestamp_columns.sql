-- 通用记录时间补齐（incr 27）
--
-- 背景：早期只给「主数据表」（桌台/菜品/订单/员工/角色…）留了 create_time/update_time，
-- 其余表按需加甚至没加。排查问题时无从判断「这行什么时候进来的、最后被谁动过」。
--
-- 约定（与既有主数据表一致）：
--   create_time —— INSERT 时写入（`store.Now()`），此后不再改动；
--   update_time —— INSERT 时一并写入（`store.Now()`），每次 UPDATE 刷新；
--   历史行保持 NULL —— 老数据的真实时间已无从得知，硬凑比留空误导性更强。
--
-- 例外说明：
--   - tb_config 既是配置也是主数据，但写入走 REPLACE（整行替换），因此只有首次种子
--     写入 create_time、之后每次改配置只推进 update_time，避免「改一下配置就变成刚创建」。
--   - tb_image 的不变量是「写入即不可变」（无 UPDATE 路径），故只加 update_time 兜底。
--   - tb_day_seq 是取餐号计数器：当天首条 INSERT 记 create_time，每次自增刷 update_time。

-- 两个时间都缺的表
ALTER TABLE tb_spec ADD COLUMN create_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_spec ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_config ADD COLUMN create_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_config ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_day_seq ADD COLUMN create_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_day_seq ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_order_item ADD COLUMN create_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_order_item ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;

-- 只缺 update_time 的表（create_time 已在 full 脚本中）
ALTER TABLE tb_print_log ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_print_job ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_oper_log ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_order_urge ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_customer ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_remember_token ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
ALTER TABLE tb_image ADD COLUMN update_time VARCHAR(32) DEFAULT NULL;
