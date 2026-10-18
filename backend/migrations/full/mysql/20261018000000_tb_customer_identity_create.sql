-- 32 号:顾客会员身份表(账号 / 登录方式解耦),把 openid 从 tb_customer 拆出来。
--
-- 设计见 docs/design-docs/customer-member-identity/spec.md:
--   tb_customer          只存会员档案与账号状态,与「怎么登录」无关;
--   tb_customer_identity 存「哪种登录方式 + 该方式下的用户标识」,
--                        (provider, external_id) 唯一 = 一个登录凭据只能绑一个会员。
--
-- 本期(P0)只写 provider='wx_mp',存量 openid 由下面的搬迁语句导入;
-- 其余 provider(wx_oa / wx_app / phone / password / apple)已登记枚举,
-- 待 App / 网站接入时再实现,表结构无需再改。
--
-- ⚠️ P0 不动 tb_customer.openid:该列 NOT NULL + 唯一索引继续保留、继续写入。
--    删列与放开可空是 P2 的前置动作(接第二个 provider 之前必须做),见 spec §7.2。
--
-- 适用范围:仅用于存量库升级;新库请直接执行 migrations/full/mysql/schema.sql
-- (建表语句已包含该表)。后端启动时 store.migrate 也会建等价的表与索引。
--
-- 幂等性:建表为 IF NOT EXISTS,搬迁用 NOT EXISTS 守卫(不依赖索引),重复执行零变更。
--         MySQL 的 CREATE INDEX 没有 IF NOT EXISTS 语法,重复执行会报
--         Duplicate key name,属可忽略的正常现象(见 migrations/README.md)。
CREATE TABLE IF NOT EXISTS tb_customer_identity (
	identity_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	customer_id     INT          NOT NULL,
	provider        VARCHAR(32)  NOT NULL,
	external_id     VARCHAR(128) NOT NULL,
	credential      VARCHAR(255) DEFAULT '',
	unionid         VARCHAR(64)  DEFAULT NULL,
	last_login_time DATETIME     DEFAULT NULL,
	create_time     DATETIME     DEFAULT CURRENT_TIMESTAMP,
	update_time     DATETIME     DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 存量搬迁:把每位现有会员的 openid 转成一条 wx_mp 身份记录。
-- 用 NOT EXISTS 而不是 INSERT IGNORE:后者依赖唯一索引已建好,
-- 若索引尚未创建,重复执行会插入重复行。这里的守卫与索引顺序无关。
INSERT INTO tb_customer_identity(customer_id, provider, external_id, last_login_time, create_time)
SELECT c.customer_id, 'wx_mp', c.openid, c.last_login_time, c.create_time
FROM tb_customer c
WHERE COALESCE(c.openid, '') <> ''
  AND NOT EXISTS (
	SELECT 1 FROM tb_customer_identity i
	WHERE i.provider = 'wx_mp' AND i.external_id = c.openid
  );

-- 登录查档主路径,同时兜底「并发首登」:两个请求同时建档,后到者拿唯一冲突,
-- service 层据此事务回滚并按 (provider, external_id) 重查走登录分支。
CREATE UNIQUE INDEX idx_identity_provider_external
	ON tb_customer_identity(provider, external_id);

-- 查「这个人绑了哪几种登录方式」(后续解绑 / 合并要用)。
CREATE INDEX idx_identity_customer ON tb_customer_identity(customer_id);

-- unionid 非唯一:同一个人的多个 openid(小程序 / 公众号 / App)共享同一个 unionid,
-- 建唯一索引会直接冲突。允许 NULL(两库的索引都把 NULL 视为不同值,可多行)。
CREATE INDEX idx_identity_unionid ON tb_customer_identity(unionid);

-- MySQL 用列上的 ON UPDATE CURRENT_TIMESTAMP 自动推进 update_time,不需要触发器。
