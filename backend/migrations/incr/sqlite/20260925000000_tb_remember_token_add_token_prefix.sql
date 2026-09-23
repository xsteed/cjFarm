-- ============================================================================
-- 20260925000000 | 记住登录令牌:新增设备识别前缀列 token_prefix
--
-- 用途:tb_remember_token.token 由「明文令牌」改为「SHA-256 哈希」存储后,
--   列表页原来的 SUBSTR(token,1,8) 取到的是哈希前缀,不再是客户端能识别的
--   原令牌前 8 位。token_prefix 保存原令牌前 8 位,供「登录设备管理」页
--   识别当前设备(完整令牌仍不回传)。
--
-- 升级说明:后端启动时也会做同样的补列(见 store/schema.go 的 alterCols,幂等),
--   本脚本供 DBA 离线执行/审阅,先跑后跑均无副作用。
--
-- 幂等性:SQLite 的 ALTER TABLE ADD COLUMN 重复执行会报 'duplicate column name',
--   已升级过的库重跑报此错误,可安全忽略。
-- ============================================================================

ALTER TABLE tb_remember_token ADD COLUMN token_prefix VARCHAR(8) DEFAULT '';
