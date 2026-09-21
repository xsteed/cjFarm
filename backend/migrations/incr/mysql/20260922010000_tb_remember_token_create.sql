-- ============================================================================
-- 记住登录令牌(实现「记住我(7/30 天免登录)」)
-- ----------------------------------------------------------------------------
-- 令牌为后端生成的随机串,存本地后仅用于向后端换取新登录态;
-- 是否过期由 expire_time 决定(后端查库裁决,前端无法篡改),与后端访问令牌(默认 24h)解耦。
-- 改密/停用/删除账号时删掉对应行,该账号全部「记住我」会话立即失效。
--
-- 幂等性:CREATE TABLE IF NOT EXISTS 可重复执行;CREATE UNIQUE INDEX 在 MySQL 下无
--        IF NOT EXISTS 语法,重复执行会报 'Duplicate key name',可安全忽略(可用 mysql --force)。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_remember_token (
    token_id       INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id        INTEGER      NOT NULL,
    token          VARCHAR(64)  NOT NULL,
    expire_time    VARCHAR(32)  NOT NULL,
    create_time    VARCHAR(32),
    last_used_time VARCHAR(32)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE UNIQUE INDEX idx_remember_token ON tb_remember_token(token);
