-- ============================================================================
-- 增量迁移 20261008150000：顾客会员档案表
-- 文件名：20261008150000_tb_customer_create.sql
-- 表：tb_customer    动作：create
-- ----------------------------------------------------------------------------
-- 背景：小程序顾客会员体系（「微信一键注册成为会员」）。
--   顾客在小程序内一键授权后,服务端用 wx.login 的 code 调 code2session 换得 openid；
--   同一微信号在该小程序下恒定,以它做唯一键 —— 首次出现即注册,再次出现即登录。
-- 语义：
--   openid          小程序用户唯一标识。明文存储(它只是站内用户标识,非密钥),接口永不外发。
--   nickname/avatar 用户主动填写,不强制；未填时前端显示「微信用户」。
--   status          1 正常 / 0 停用。顾客令牌无状态,登录态校验时顺带查本列,停用立即生效。
--   order_count     累计下单次数（会员卡展示用冗余列,下单成功后维护,失败不影响下单）。
--   last_order_time 最近一次下单时间（展示用）。
--   create_time     即「入会日期」。
--   不设 session_key 列：一期不解密手机号等敏感数据,最小化收集。
--   不设 del_flag   ：会员不做软删除；注销直接物理删（档案不含敏感信息）。
-- 适用：尚无此表的 SQLite 库。
-- ⚠️ 幂等性：CREATE TABLE/INDEX 均带 IF NOT EXISTS,可重复执行。
-- 执行：sqlite3 dining.db < 20261008150000_tb_customer_create.sql
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_customer (
        customer_id      INTEGER PRIMARY KEY AUTOINCREMENT,
        openid           VARCHAR(64)  NOT NULL,
        nickname         VARCHAR(64)  DEFAULT '',
        avatar_url       VARCHAR(255) DEFAULT '',
        status           INTEGER      DEFAULT 1,
        order_count      INTEGER      DEFAULT 0,
        last_order_time  VARCHAR(32)  DEFAULT '',
        create_time      VARCHAR(32),
        last_login_time  VARCHAR(32)  DEFAULT '',
        remark           VARCHAR(255) DEFAULT ''
    );
CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_openid ON tb_customer(openid);
