-- create_time / update_time 补齐、转 DATETIME 并移到表尾（incr 27）
--
-- 本脚本合并了这两列的补列、清洗转型与置尾全部变更（其余业务时间列保持 VARCHAR(32) 不变）。
-- 目标（与重新生成的 migrations/full/{mysql,sqlite}/schema.sql 完全一致）：
--   每张表的最后两列都是 create_time / update_time，类型 DATETIME，
--   create_time 插入时由数据库填当前时间，update_time 行被更新时自动推进，
--   应用层、运维手工 SQL、外部导入三方都不必再关心这两个字段。
--
-- 处理策略：
--   缺列的表     —— ADD COLUMN（天然落在表尾，直接建成 DATETIME）；
--   只缺 update_time 的表 —— ADD update_time（落表尾）+ MODIFY create_time（转型并挪到它前面）；
--   两列都有的表 —— MODIFY（转型 + AFTER 置尾一步到位）。
--
-- 执行前请注意：
--   1) 第一节先清洗空串 / '0000-00-00' 历史零值（严格模式下 MODIFY 会因这类非法值直接失败），
--      清洗即把「没有值」规范化成 NULL，不改动真实时间；
--   2) MySQL 的 CURRENT_TIMESTAMP 取**会话时区**，执行前确认 time_zone 与应用部署时区一致
--      （如 SET time_zone='+08:00';），否则会出现「应用写本地时间、默认值写 UTC」两套时间；
--   3) MODIFY 会重建表（同表多条已合并为一条 ALTER，整表只重建一次），
--      大表（tb_order / tb_user / tb_role / tb_dish）请在业务低峰执行。
--
-- 适用范围：仅用于存量库升级，须在 20261015000000（订单用餐时间）之后执行；
-- 新库请直接执行 migrations/full/{mysql,sqlite}/schema.sql（建表语句已包含这些列）。
-- 幂等性：MODIFY 与清洗可重复执行；ADD COLUMN 在列已存在时会报 Duplicate column，
-- 请勿对已补过列的库重复执行本节。

-- 一、清洗非法值（空串 / 历史零值 → NULL，仅限本脚本要 MODIFY 的既有列）
UPDATE tb_table SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_table SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_category SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_category SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_dish SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_dish SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_remark SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_remark SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_printer SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_printer SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_print_log SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_print_job SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_print_agent SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_print_agent SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_oper_log SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_order SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_order SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_order_urge SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_payment SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_payment SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_refund SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_refund SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_role SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_role SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_user SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_user SET update_time=NULL WHERE COALESCE(update_time,'')='' OR update_time LIKE '0000-00-00%';
UPDATE tb_customer SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_remember_token SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';
UPDATE tb_image SET create_time=NULL WHERE COALESCE(create_time,'')='' OR create_time LIKE '0000-00-00%';

-- 二、两个时间都缺的表：ADD 直接建成 DATETIME，追加在表尾
ALTER TABLE tb_spec
	ADD COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_config
	ADD COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_day_seq
	ADD COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_order_item
	ADD COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;

-- 三、只缺 update_time 的表：ADD update_time 落表尾，create_time 转型并挪到它前面
ALTER TABLE tb_print_log
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE tb_print_job
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER done_time;
ALTER TABLE tb_oper_log
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE tb_order_urge
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER handle_by;
ALTER TABLE tb_customer
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER remark;
ALTER TABLE tb_remember_token
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER ua;
ALTER TABLE tb_image
	ADD COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP;

-- 四、两列均已存在的表：转型 + AFTER 置尾（同表合并为一条 ALTER，只重建一次）
ALTER TABLE tb_table
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER table_code,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP AFTER create_time;
ALTER TABLE tb_category
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_dish
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER remark,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP AFTER create_time;
ALTER TABLE tb_remark
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_printer
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_print_agent
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_payment
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP;
ALTER TABLE tb_refund
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER is_duplicate,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP AFTER create_time;
ALTER TABLE tb_role
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER remark,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP AFTER create_time;
ALTER TABLE tb_user
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER remark,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP AFTER create_time;
ALTER TABLE tb_order
	MODIFY COLUMN create_time DATETIME NULL DEFAULT CURRENT_TIMESTAMP AFTER meal_end_time,
	MODIFY COLUMN update_time DATETIME NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP AFTER create_time;
