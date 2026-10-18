-- 32 号:访问明细补「终端与来源」六列(说明见 sqlite 同名脚本)。
--
-- 幂等性:ADD COLUMN 列已存在时报 Duplicate column name,属可忽略的正常现象。
ALTER TABLE tb_visit_log ADD COLUMN ip VARCHAR(45) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN city VARCHAR(64) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN os VARCHAR(16) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN browser VARCHAR(16) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN device VARCHAR(8) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN model VARCHAR(64) DEFAULT '';
