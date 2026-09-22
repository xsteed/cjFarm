-- ============================================================================
-- 本地打印代理:新增 tb_print_job 任务队列表(MySQL)
-- ----------------------------------------------------------------------------
-- 与 sqlite/ 同名脚本内容一致,仅主键/建表选项/索引语法按 MySQL 适配
-- (SQLite 版见 migrations/incr/sqlite/20260923000000_tb_print_job_create.sql,
--  设计说明与「与 tb_print_log 的分工」在那里有完整注释)。
--
-- 注意:MySQL 不支持 CREATE INDEX IF NOT EXISTS,重复执行会报
-- 'Duplicate key name',属正常现象、可忽略(可用 mysql --force 继续)。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_print_job (
    job_id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    printer_id    INT           DEFAULT 0,
    printer_name  VARCHAR(64)   DEFAULT '',
    printer_type  INT           DEFAULT 1,
    ip            VARCHAR(64)   DEFAULT '',
    port          INT           DEFAULT 9100,
    doc_type      VARCHAR(16)   DEFAULT '',
    order_id      INT           DEFAULT 0,
    order_no      VARCHAR(64)   DEFAULT '',
    table_no      VARCHAR(32)   DEFAULT '',
    copies        INT           DEFAULT 1,
    print_log_id  INT           DEFAULT 0,
    delivery_id   VARCHAR(36)   DEFAULT '',
    payload       VARCHAR(4000) DEFAULT '',
    status        INT           DEFAULT 0,
    attempts      INT           DEFAULT 0,
    last_error    VARCHAR(500)  DEFAULT '',
    claimed_by    VARCHAR(64)   DEFAULT '',
    claim_time    VARCHAR(32),
    next_try_time VARCHAR(32),
    trigger_by    VARCHAR(16)   DEFAULT '',
    operator      VARCHAR(64)   DEFAULT '',
    create_time   VARCHAR(32),
    done_time     VARCHAR(32)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 取单按「状态 + 可执行时间」筛(代理默认 3 秒轮询一次,必须有索引),
-- 打印日志页按「打印机 + 状态」看某台机器的积压。
CREATE INDEX idx_print_job_pick ON tb_print_job(status, next_try_time, job_id);
CREATE INDEX idx_print_job_printer ON tb_print_job(printer_id, status);
