-- ============================================================================
-- 本地打印代理 v2:新增 tb_print_agent 代理身份表(MySQL)。
-- ----------------------------------------------------------------------------
-- 背景:全局唯一 agent_token 是 legacy 兼容通道;v2 允许逐台签发 per-agent 令牌,
-- 可单独吊销、可限定授权打印机范围(printer_ids),拿到一台代理的令牌不再等于
-- 拿到整个待打印队列。与 sqlite/ 同名脚本内容一致,仅主键/建表选项/索引语法按 MySQL 适配。
--
-- 注意:MySQL 不支持 CREATE INDEX IF NOT EXISTS,重复执行会报
-- 'Duplicate key name',属正常现象、可忽略(可用 mysql --force 继续)。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_print_agent (
    agent_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    agent_name  VARCHAR(64)  DEFAULT '',
    token_hash  VARCHAR(64)  DEFAULT '',   -- SHA-256(令牌) hex,不存明文
    token_hint  VARCHAR(8)   DEFAULT '',   -- 令牌前 4 位,列表页辨认用
    printer_ids VARCHAR(255) DEFAULT '',   -- 授权打印机 id 逗号串;空 = 全部
    status      INT          DEFAULT 1,    -- 1=启用 0=吊销
    last_seen   VARCHAR(32),
    last_report VARCHAR(64)  DEFAULT '',   -- 最近上报的 agentId/主机名/版本
    create_time VARCHAR(32),
    update_time VARCHAR(32)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE INDEX idx_print_agent_token ON tb_print_agent(token_hash, status);
