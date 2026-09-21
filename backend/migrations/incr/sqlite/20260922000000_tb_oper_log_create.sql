-- ============================================================================
-- 操作日志(审计留痕):新增 tb_oper_log 表
-- ----------------------------------------------------------------------------
-- 业务表上的 create_by / update_by 只回答「谁改的」,回答不了
-- 「什么时候、传了什么参数、成功了没」。权限变更、免单、退款、改单这类
-- 事后必须说得清的动作,靠这张表兜底。
--
-- 采集方式:管理端写操作由 internal/handler/audit.go 的 AuditLog 中间件自动落库;
-- 登录与越权尝试(发生在鉴权中间件里)单独埋点。
-- 开关:AUDIT_LOG_ENABLED(默认 1)/ AUDIT_LOG_GET(默认 0)/ AUDIT_RETENTION_DAYS(默认 365)。
--
-- 幂等性:CREATE TABLE IF NOT EXISTS + CREATE INDEX IF NOT EXISTS,可重复执行。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_oper_log (
    log_id        INTEGER PRIMARY KEY AUTOINCREMENT,
    module        VARCHAR(32)   DEFAULT '',
    business_type VARCHAR(16)   DEFAULT 'other',
    action        VARCHAR(64)   DEFAULT '',
    method        VARCHAR(255)  DEFAULT '',
    request_url   VARCHAR(255)  DEFAULT '',
    operator_id   INTEGER       DEFAULT 0,
    operator      VARCHAR(64)   DEFAULT '',
    operator_role VARCHAR(64)   DEFAULT '',
    oper_ip       VARCHAR(64)   DEFAULT '',
    target_type   VARCHAR(32)   DEFAULT '',
    target_id     VARCHAR(64)   DEFAULT '',
    oper_param    VARCHAR(2000) DEFAULT '',
    detail        VARCHAR(500)  DEFAULT '',
    status        INTEGER       DEFAULT 0,
    error_msg     VARCHAR(500)  DEFAULT '',
    cost_ms       INTEGER       DEFAULT 0,
    create_time   VARCHAR(32)
);

-- 按时间翻页(主查询)、按人追责、按对象倒查「谁动过这一单」、只看失败。
CREATE INDEX IF NOT EXISTS idx_operlog_time ON tb_oper_log(create_time);
CREATE INDEX IF NOT EXISTS idx_operlog_operator ON tb_oper_log(operator_id, create_time);
CREATE INDEX IF NOT EXISTS idx_operlog_target ON tb_oper_log(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_operlog_status ON tb_oper_log(status, create_time);
