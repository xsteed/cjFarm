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
--   - CREATE TABLE / CREATE INDEX 均带 IF NOT EXISTS，可安全重复执行；
--   - 本脚本与「后端启动时按 schema.go/indexDefs 自动建表建索引」等效，
--     两者都跑也不会出错（启动时跳过已存在的表和索引）。
-- 设计文档：docs/design-docs/order-review/spec.md。

CREATE TABLE IF NOT EXISTS tb_review (
        review_id    INTEGER PRIMARY KEY AUTOINCREMENT,
        order_id     INTEGER      NOT NULL,
        order_no     VARCHAR(64)  NOT NULL,
        biz_date     VARCHAR(10)  DEFAULT '',
        table_id     INTEGER      DEFAULT 0,
        table_no     VARCHAR(32)  DEFAULT '',
        table_name   VARCHAR(64)  DEFAULT '',
        person_count INTEGER      DEFAULT 0,
        customer_id  INTEGER      DEFAULT 0,
        nickname     VARCHAR(64)  DEFAULT '',
        phone        VARCHAR(20)  DEFAULT '',
        rating       INTEGER      DEFAULT 0,
        tags         VARCHAR(255) DEFAULT '',
        content      VARCHAR(500) DEFAULT '',
        ip_hash      VARCHAR(64)  DEFAULT '',
        ua_hash      VARCHAR(64)  DEFAULT '',
        create_time  VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time  VARCHAR(32) DEFAULT NULL
    );

CREATE TABLE IF NOT EXISTS tb_review_image (
        image_id     INTEGER PRIMARY KEY AUTOINCREMENT,
        review_id    INTEGER      DEFAULT 0,
        img_name     VARCHAR(128) NOT NULL,
        content_type VARCHAR(32)  DEFAULT '',
        img_data     BLOB,
        file_size    INTEGER      DEFAULT 0,
        create_time  VARCHAR(32) DEFAULT (datetime('now','localtime')),
        update_time  VARCHAR(32) DEFAULT NULL
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_review_order ON tb_review(order_no);
CREATE INDEX IF NOT EXISTS idx_review_time ON tb_review(create_time);
CREATE INDEX IF NOT EXISTS idx_review_rating ON tb_review(rating, create_time);
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_image_name ON tb_review_image(img_name);
CREATE INDEX IF NOT EXISTS idx_review_image_review ON tb_review_image(review_id);
CREATE INDEX IF NOT EXISTS idx_review_image_time ON tb_review_image(create_time);
