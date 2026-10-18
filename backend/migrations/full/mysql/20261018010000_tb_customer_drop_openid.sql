-- 33 号:tb_customer 退役 openid 列(登录方式已由 tb_customer_identity 承载)。
--
-- 背景:32 号把 openid 搬进了 tb_customer_identity(provider='wx_mp'),但
-- tb_customer.openid 仍是 NOT NULL + 唯一索引。唯一索引只允许**一行**空串,
-- 因此第二个登录方式(手机号 / 密码 / Apple)永远建不成档 —— 必须删列,而不是留着不用。
--
-- 设计见 docs/design-docs/customer-member-identity/spec.md §7.2(P2)。
--
-- 幂等性:搬迁带 NOT EXISTS 守卫,重复执行零变更;
--         DROP INDEX / DROP COLUMN 都没有 IF EXISTS,重复执行会报
--         check that column/key exists,属可忽略的正常现象。
--
-- 适用范围:仅用于存量库升级;新库直接执行 migrations/full/mysql/schema.sql(已无该列)。
-- 后端启动时的 store.migrateCustomerOpenid 会做同等且幂等的一次性迁移,
-- 因此**不跑本脚本也不会漏**(常规升级只要重启服务即可)。

-- 1) 搬迁(兼容「跳过 32 号直接升级」的库:它的 openid 还没进 identity 表)。
INSERT INTO tb_customer_identity(customer_id, provider, external_id, last_login_time, create_time)
SELECT c.customer_id, 'wx_mp', c.openid, c.last_login_time, c.create_time
FROM tb_customer c
WHERE COALESCE(c.openid, '') <> ''
  AND NOT EXISTS (
	SELECT 1 FROM tb_customer_identity i
	WHERE i.provider = 'wx_mp' AND i.external_id = c.openid
  );

-- 2) 删唯一索引(MySQL 删列会自动摘掉索引,这里显式删是为了与 SQLite 侧完全同构,
--    也让 DBA 在 SHOW CREATE TABLE 里看到同样的结果)。
DROP INDEX idx_customer_openid ON tb_customer;

-- 3) 删列。
ALTER TABLE tb_customer DROP COLUMN openid;
