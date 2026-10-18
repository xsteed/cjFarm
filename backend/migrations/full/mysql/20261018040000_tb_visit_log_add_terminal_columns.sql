-- 36 号:访问明细补「终端与来源」六列(说明见 sqlite 同名脚本)。
--
-- 幂等性:列已存在时报 Duplicate column name,属可忽略的正常现象。
-- 单条 ALTER 内多个 ADD COLUMN 各自带 AFTER 以维持列顺序(与 full 建表语句一致),
-- 同时避免部分客户端对多条语句拆分不当导致的语法报错。
ALTER TABLE tb_visit_log
    ADD COLUMN ip      VARCHAR(45) DEFAULT '' AFTER ua_hash,
    ADD COLUMN city    VARCHAR(64) DEFAULT '' AFTER ip,
    ADD COLUMN os      VARCHAR(16) DEFAULT '' AFTER city,
    ADD COLUMN browser VARCHAR(16) DEFAULT '' AFTER os,
    ADD COLUMN device  VARCHAR(8)  DEFAULT '' AFTER browser,
    ADD COLUMN model   VARCHAR(64) DEFAULT '' AFTER device;
