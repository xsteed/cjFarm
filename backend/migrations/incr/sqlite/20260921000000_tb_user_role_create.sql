-- ============================================================================
-- 老库升级脚本(SQLite):员工与权限体系
-- 文件名:20260921000000_tb_user_role_create.sql
-- ----------------------------------------------------------------------------
-- 变更内容:
--   1. 新增 tb_role(角色,权限码以 CSV 存放)
--   2. 新增 tb_user(员工账号,role_id 关联角色)
--   3. 新增 3 个索引(idx_user_username / idx_user_role / idx_role_key)
--   4. 写入 4 个内置角色(admin / manager / cashier / staff)
--
-- 幂等性:建表与建索引均带 IF NOT EXISTS,角色用 INSERT OR IGNORE + 显式主键,
--         可安全重复执行。
--
-- 注意:tb_user 的初始管理员账号不在本脚本中创建 —— 其密码是 bcrypt
--       (随机盐)哈希,无法在静态脚本里预置。后端启动引导会自动创建,
--       用户名取 ADMIN_USER(默认 admin),密码取库中已有的 admin_pass_hash
--       或 ADMIN_PASS 环境变量(默认 admin123)。
--
-- 权限点与角色矩阵的权威定义见 backend/internal/store/permission.go。
-- ============================================================================

CREATE TABLE IF NOT EXISTS tb_role (
    role_id     INTEGER PRIMARY KEY AUTOINCREMENT,
    role_key    VARCHAR(32)   NOT NULL,
    role_name   VARCHAR(64)   NOT NULL,
    perms       VARCHAR(4000) DEFAULT '',
    data_scope  VARCHAR(16)   DEFAULT 'all',
    is_builtin  INTEGER       DEFAULT 0,
    sort_order  INTEGER       DEFAULT 0,
    status      INTEGER       DEFAULT 1,
    del_flag    VARCHAR(2)    DEFAULT '0',
    create_time VARCHAR(32),
    update_time VARCHAR(32),
    remark      VARCHAR(500)  DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tb_user (
    user_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username        VARCHAR(64)  NOT NULL,
    password_hash   VARCHAR(255) DEFAULT '',
    real_name       VARCHAR(64)  DEFAULT '',
    role_id         INTEGER      NOT NULL DEFAULT 0,
    phone           VARCHAR(32)  DEFAULT '',
    status          INTEGER      DEFAULT 1,
    last_login_time VARCHAR(32),
    last_login_ip   VARCHAR(64)  DEFAULT '',
    login_count     INTEGER      DEFAULT 0,
    pwd_update_time VARCHAR(32),
    token_version   INTEGER      DEFAULT 0,
    del_flag        VARCHAR(2)   DEFAULT '0',
    create_by       VARCHAR(64)  DEFAULT '',
    create_time     VARCHAR(32),
    update_by       VARCHAR(64)  DEFAULT '',
    update_time     VARCHAR(32),
    remark          VARCHAR(500) DEFAULT ''
);

-- 用户名/角色标识的唯一性只约束未删除的行:删掉员工后可以重新使用同名账号。
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_username ON tb_user(username) WHERE del_flag='0';
CREATE INDEX IF NOT EXISTS idx_user_role ON tb_user(role_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_role_key ON tb_role(role_key) WHERE del_flag='0';

-- ---- 内置角色(4 个) ----
-- admin 为全量权限,每次后端启动会强制恢复为全量(防锁死)。
INSERT OR IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(1, 'admin', '超级管理员', 'table:view,table:edit,category:view,category:edit,dish:view,dish:edit,remark:view,remark:edit,printer:view,printer:edit,order:view,order:operate,order:settle,order:edit,order:cancel,credit:view,credit:settle,refund:view,refund:operate,report:view,config:view,config:edit,user:view,user:edit,role:view,role:edit', 'all', 1, 1, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '拥有全部权限。权限不可修改,每次启动强制恢复为全量,防止锁死系统。');
INSERT OR IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(2, 'manager', '店长', 'table:view,table:edit,category:view,category:edit,dish:view,dish:edit,remark:view,remark:edit,printer:view,printer:edit,order:view,order:operate,order:settle,order:edit,order:cancel,credit:view,credit:settle,refund:view,report:view,config:view,config:edit', 'all', 1, 2, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '日常最高权限:可改配置、看报表,但不能管理员工与权限,也不能发起退款。');
INSERT OR IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(3, 'cashier', '收银员', 'table:view,category:view,dish:view,remark:view,printer:view,order:view,order:operate,order:settle,order:edit,order:cancel,credit:view,credit:settle,refund:view', 'all', 1, 3, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '前厅收银:处理订单全流程与挂账核销,不涉及配置、报表与退款发起。');
INSERT OR IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(4, 'staff', '员工', 'table:view,category:view,dish:view,remark:view,order:view,order:operate', 'all', 1, 4, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '后厨/服务员通用:只读基础资料,订单可流转(接单/上菜/完成/处理催菜)。');
