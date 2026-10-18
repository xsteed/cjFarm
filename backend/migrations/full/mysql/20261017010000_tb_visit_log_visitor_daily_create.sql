-- 31 号:站点访问统计三张表(PV / UV / 新访客 / 渠道构成)。
--
-- 设计见 docs/design-docs/visit-analytics/spec.md,三张表的分工:
--   tb_visit_log   访问明细,「待办」性质 —— 日聚合落好后按 VISIT_RETENTION_DAYS
--                  (默认 90 天)清理,渠道 UV 的窗口查询只覆盖保留期内的明细;
--   tb_visitor     访客注册表,「台账」—— 永久保留,让「新访客」精确
--                  (first_visit_date 等于统计日即为当日新增);
--   tb_visit_daily 日聚合台账 —— 永久保留,报表主读;channel='all' 行为跨渠道
--                  去重后的全站合计,各单渠道行之和不等于它(同一访客可能
--                  当天两个渠道都来过)。
--
-- ip_hash / ua_hash 是 sha256(值 + 登录令牌密钥) 的前 32 位:库文件泄露也无法
-- 反推原始 IP/UA;多实例部署须共享同一密钥文件,各节点哈希才一致(UV 跨实例去重)。
--
-- 适用范围:仅用于存量库升级;新库请直接执行 migrations/full/mysql/schema.sql
-- (建表语句已包含这三张表)。后端启动时 store.migrate 也会建等价的表与索引。
--
-- 幂等性:建表为 IF NOT EXISTS;MySQL 的 CREATE INDEX 没有 IF NOT EXISTS 语法,
-- 重复执行会报 Duplicate key name,属可忽略的正常现象。
CREATE TABLE IF NOT EXISTS tb_visit_log (
	visit_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	visit_date  VARCHAR(10) NOT NULL,
	channel     VARCHAR(16) NOT NULL,
	visitor_key VARCHAR(80) NOT NULL,
	is_member   INT          DEFAULT 0,
	customer_id INT          DEFAULT 0,
	entry       VARCHAR(16)  DEFAULT '',
	ip_hash     VARCHAR(64)  DEFAULT '',
	ua_hash     VARCHAR(64)  DEFAULT '',
	create_time DATETIME     DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 访客注册表:主键即访客键,无需自增 ID(天然去重)。
CREATE TABLE IF NOT EXISTS tb_visitor (
	visitor_key      VARCHAR(80) PRIMARY KEY,
	first_visit_date VARCHAR(10) NOT NULL,
	last_visit_date  VARCHAR(10) NOT NULL,
	visit_count      INT         DEFAULT 1,
	is_member        INT         DEFAULT 0,
	customer_id      INT         DEFAULT 0,
	create_time      DATETIME    DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 日聚合台账:rollup 写绝对值(先 UPDATE 后 INSERT),可反复重算。
CREATE TABLE IF NOT EXISTS tb_visit_daily (
	id         INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	visit_date VARCHAR(10) NOT NULL,
	channel    VARCHAR(16) NOT NULL,
	pv         INT          DEFAULT 0,
	uv         INT          DEFAULT 0,
	new_uv     INT          DEFAULT 0,
	member_uv  INT          DEFAULT 0,
	create_time DATETIME    DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE INDEX idx_visitlog_date    ON tb_visit_log(visit_date);
CREATE INDEX idx_visitlog_visitor ON tb_visit_log(visit_date, visitor_key);
CREATE INDEX idx_visitlog_channel ON tb_visit_log(visit_date, channel, visitor_key);
CREATE INDEX idx_visitor_first     ON tb_visitor(first_visit_date);
CREATE UNIQUE INDEX idx_visit_daily_uk ON tb_visit_daily(visit_date, channel);
