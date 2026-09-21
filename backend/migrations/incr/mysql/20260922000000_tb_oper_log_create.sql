-- ============================================================================
-- 操作日志(审计留痕):新增 tb_oper_log 表(MySQL)
-- ----------------------------------------------------------------------------
-- 与 sqlite/ 同名脚本内容一致,仅建表选项与索引语法按 MySQL 适配
-- (SQLite 版见 migrations/incr/sqlite/20260922000000_tb_oper_log_create.sql)。
--
-- 注意:MySQL 不支持 CREATE INDEX IF NOT EXISTS,重复执行会报
-- 'Duplicate key name',属正常现象、可忽略(可用 mysql --force 继续)。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_oper_log (
    log_id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    module        VARCHAR(32)   DEFAULT '',
    business_type VARCHAR(16)   DEFAULT 'other',
    action        VARCHAR(64)   DEFAULT '',
    method        VARCHAR(255)  DEFAULT '',
    request_url   VARCHAR(255)  DEFAULT '',
    operator_id   INT           DEFAULT 0,
    operator      VARCHAR(64)   DEFAULT '',
    operator_role VARCHAR(64)   DEFAULT '',
    oper_ip       VARCHAR(64)   DEFAULT '',
    target_type   VARCHAR(32)   DEFAULT '',
    target_id     VARCHAR(64)   DEFAULT '',
    oper_param    VARCHAR(2000) DEFAULT '',
    detail        VARCHAR(500)  DEFAULT '',
    status        INT           DEFAULT 0,
    error_msg     VARCHAR(500)  DEFAULT '',
    cost_ms       INT           DEFAULT 0,
    create_time   VARCHAR(32)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 按时间翻页(主查询)、按人追责、按对象倒查「谁动过这一单」、只看失败。
CREATE INDEX idx_operlog_time ON tb_oper_log(create_time);
CREATE INDEX idx_operlog_operator ON tb_oper_log(operator_id, create_time);
CREATE INDEX idx_operlog_target ON tb_oper_log(target_type, target_id);
CREATE INDEX idx_operlog_status ON tb_oper_log(status, create_time);
