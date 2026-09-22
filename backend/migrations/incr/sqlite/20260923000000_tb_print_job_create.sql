-- ============================================================================
-- 本地打印代理:新增 tb_print_job 任务队列表
-- ----------------------------------------------------------------------------
-- 解决的问题:后端部署到云服务器后,「直连 打印机IP:9100」够不到门店内网,
-- 换飞鹅云又要买新机器。本表让门店已有的网络热敏机继续可用:
--
--     云后端 ──入队(tb_print_job)──> 门店代理 ──TCP IP:9100──> 打印机
--
-- 门店内网常驻一个 print-agent 程序,主动出站轮询云端取单,再向打印机直发
-- ESC/POS 字节(9100 是 RAW 端口,不需要任何打印机驱动)。
-- 部署步骤见 docs/print-agent.md;接入方式在 tb_printer.provider 里选 'agent'。
--
-- 与 tb_print_log 的分工:
--   tb_print_job 是「待办」,一张票据送完即结案(可定期清理,见 store.CleanDonePrintJobs);
--   tb_print_log 是「台账」,给商家查「这单打了没」,长期保留。
--   两者由 print_log_id 关联:入队时先写一条 status=2(排队中)的日志,代理回执后回写。
--
-- payload 存的是「渲染后的等宽文本行」(以 \n 连接),不是 ESC/POS 字节:
-- 字节在代理取单时由后端现场编码(含 GBK、切纸)并 base64 下发,代理程序因此
-- 不需要理解任何打印协议;VARCHAR(4000) 约合 80mm 小票 75 行,超出时按行拆成多条任务。
--
-- 新增配置项 agent_token(代理令牌,敏感项,落库前自动加密)由后端启动时补齐,
-- 本脚本不含种子数据。
--
-- 幂等性:CREATE TABLE IF NOT EXISTS + CREATE INDEX IF NOT EXISTS,可重复执行。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_print_job (
    job_id        INTEGER PRIMARY KEY AUTOINCREMENT,
    printer_id    INTEGER       DEFAULT 0,
    printer_name  VARCHAR(64)   DEFAULT '',
    printer_type  INTEGER       DEFAULT 1,
    ip            VARCHAR(64)   DEFAULT '',
    port          INTEGER       DEFAULT 9100,
    doc_type      VARCHAR(16)   DEFAULT '',
    order_id      INTEGER       DEFAULT 0,
    order_no      VARCHAR(64)   DEFAULT '',
    table_no      VARCHAR(32)   DEFAULT '',
    copies        INTEGER       DEFAULT 1,
    print_log_id  INTEGER       DEFAULT 0,
    delivery_id   VARCHAR(36)   DEFAULT '',
    payload       VARCHAR(4000) DEFAULT '',
    status        INTEGER       DEFAULT 0,
    attempts      INTEGER       DEFAULT 0,
    last_error    VARCHAR(500)  DEFAULT '',
    claimed_by    VARCHAR(64)   DEFAULT '',
    claim_time    VARCHAR(32),
    next_try_time VARCHAR(32),
    trigger_by    VARCHAR(16)   DEFAULT '',
    operator      VARCHAR(64)   DEFAULT '',
    create_time   VARCHAR(32),
    done_time     VARCHAR(32)
);

-- 取单按「状态 + 可执行时间」筛(代理默认 3 秒轮询一次,必须有索引),
-- 打印日志页按「打印机 + 状态」看某台机器的积压。
CREATE INDEX IF NOT EXISTS idx_print_job_pick ON tb_print_job(status, next_try_time, job_id);
CREATE INDEX IF NOT EXISTS idx_print_job_printer ON tb_print_job(printer_id, status);

-- 说明:status 取值 0 待取单 / 1 已被代理取走(带 60 秒租约) / 2 已送出 / 3 重试耗尽已放弃。
-- 老库升级到本版本后无需回填:没有 agent 通道的打印机时本表为空,行为与升级前完全一致。
