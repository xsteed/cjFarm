-- 订单评价（2026-10-25）：仅商家可见的内部反馈通道 + 顾客上传的评价图片
--
-- 新增两张表（结构变更唯一落点在本目录；与后端启动时的自动建表等价，执行其一即可）：
--   tb_review        订单评价（1-5 星 + 快捷标签 + 文字；桌台/人数/会员昵称与手机号
--                    提交时一次性快照，IP/UA 只存胡椒哈希 sha256(auth.key|值)，明文不落库）
--   tb_review_image  评价图片（顾客上传的用户内容，刻意独立于商家资产表 tb_image：
--                    生命周期不同——级联删除/孤儿清理，且迁对象存储(COS)时可整表筛选搬迁；
--                    review_id=0 表示「已上传未绑定评价」，后台任务 24h 后清理）
--
-- 索引：order_no 唯一 = 一单一评的数据库兜底（service 层预检在事务外，并发双击靠它）；
--       img_name 唯一 = 展示与「先传后评」绑定的凭据。
-- 无存量数据迁移：全新功能，老库升级后从空表开始。
--
-- 幂等与重复执行：
--   - CREATE TABLE 带 IF NOT EXISTS，可安全重复执行；
--   - MySQL 不支持 CREATE INDEX IF NOT EXISTS，重复执行报 Duplicate key name
--     属正常现象、可忽略（见 migrations/README.md 第二节）；
--   - 本脚本与「后端启动时按 schema.go/indexDefs 自动建表建索引」等效，
--     两者都跑也不会出错（启动时跳过已存在的表和索引）。
-- 设计文档：docs/design-docs/order-review/spec.md。

CREATE TABLE IF NOT EXISTS tb_review (
        review_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '评价ID(自增主键)',
        order_id     INTEGER      NOT NULL COMMENT '关联订单ID',
        order_no     VARCHAR(64)  NOT NULL COMMENT '关联订单号(唯一,一单一评)',
        biz_date     VARCHAR(10)  DEFAULT '' COMMENT '营业日快照(筛选与报表同口径)',
        table_id     INTEGER      DEFAULT 0 COMMENT '桌台ID',
        table_no     VARCHAR(32)  DEFAULT '' COMMENT '桌号快照',
        table_name   VARCHAR(64)  DEFAULT '' COMMENT '桌名快照(桌台改名不影响历史)',
        person_count INTEGER      DEFAULT 0 COMMENT '就餐人数快照(差评语境参考)',
        customer_id  INTEGER      DEFAULT 0 COMMENT '会员ID 0=匿名 注销后仅作历史编号',
        nickname     VARCHAR(64)  DEFAULT '' COMMENT '会员昵称快照(提交时一次性写死)',
        phone        VARCHAR(20)  DEFAULT '' COMMENT '会员手机号快照(provider=phone 身份,匿名为空)',
        rating       INTEGER      DEFAULT 0 COMMENT '总分 1..5 星',
        tags         VARCHAR(255) DEFAULT '' COMMENT '快捷标签 逗号分隔 最多6个',
        content      VARCHAR(500) DEFAULT '' COMMENT '文字评价(最多200字)',
        ip_hash      VARCHAR(64)  DEFAULT '' COMMENT 'IP 胡椒哈希(sha256(auth.key|IP) 明文不落库)',
        ua_hash      VARCHAR(64)  DEFAULT '' COMMENT 'User-Agent 胡椒哈希(同 ip_hash)',
        create_time  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '提交时间',
        update_time  DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='订单评价(仅商家可见的内部反馈,快照自含追溯信息)';

CREATE TABLE IF NOT EXISTS tb_review_image (
        image_id     INT NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '图片ID(自增主键)',
        review_id    INTEGER      DEFAULT 0 COMMENT '所属评价ID 0=已上传未绑定(孤儿,24h 后清理)',
        img_name     VARCHAR(128) NOT NULL COMMENT '文件名(唯一,展示走 /uploads/{name})',
        content_type VARCHAR(32)  DEFAULT '' COMMENT 'MIME 类型',
        img_data     LONGBLOB COMMENT '图片内容(BLOB,与 tb_image 同一运行时模型)',
        file_size    INTEGER      DEFAULT 0 COMMENT '文件字节数',
        create_time  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '上传时间',
        update_time  DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间(行更新自动推进)'
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='订单评价图片(顾客上传的用户内容,独立于商家资产表 tb_image,便于区分与迁对象存储)';

CREATE UNIQUE INDEX idx_review_order ON tb_review(order_no);
CREATE INDEX idx_review_time ON tb_review(create_time);
CREATE INDEX idx_review_rating ON tb_review(rating, create_time);
CREATE UNIQUE INDEX idx_review_image_name ON tb_review_image(img_name);
CREATE INDEX idx_review_image_review ON tb_review_image(review_id);
CREATE INDEX idx_review_image_time ON tb_review_image(create_time);
