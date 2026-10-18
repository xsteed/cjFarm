-- 时间列自动维护 update_time（incr 28）
--
-- SQLite 没有 ON UPDATE CURRENT_TIMESTAMP，只能靠 AFTER UPDATE 触发器实现，
-- 本脚本为每张含 update_time 的表建一个触发器。
--
-- 与 MySQL 的差异（有意为之，见 store/schema.go 的方言分叉注释）：
--   - SQLite 的时间列**保持 VARCHAR(32)**：SQLite 的类型只是亲和性提示，声明成
--     DATETIME 会让驱动在读取时尝试解析，历史脏值（空串 / '0000-00-00' / 外部
--     导入的异常格式）会直接报错中断查询；保持文本更抗打。
--   - 建表默认值 DEFAULT (datetime('now','localtime')) 只对**新建库**生效，
--     已有库无法通过 ALTER 补默认值（SQLite 不支持），所以老库的 create_time
--     仍由应用层显式写入 —— 两者结果一致，不冲突。
--
-- 触发器只在「行被更新但没带 update_time」时补时间：
-- WHEN NEW.update_time IS OLD.update_time 保证应用显式写入的值不被覆盖。
--
-- 幂等性：CREATE TRIGGER IF NOT EXISTS，可重复执行。

CREATE TRIGGER IF NOT EXISTS trg_tb_table_touch
    AFTER UPDATE ON tb_table FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_table SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_category_touch
    AFTER UPDATE ON tb_category FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_category SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_dish_touch
    AFTER UPDATE ON tb_dish FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_dish SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_spec_touch
    AFTER UPDATE ON tb_spec FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_spec SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_remark_touch
    AFTER UPDATE ON tb_remark FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_remark SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_printer_touch
    AFTER UPDATE ON tb_printer FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_printer SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_print_log_touch
    AFTER UPDATE ON tb_print_log FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_print_log SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_print_job_touch
    AFTER UPDATE ON tb_print_job FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_print_job SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_print_agent_touch
    AFTER UPDATE ON tb_print_agent FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_print_agent SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_oper_log_touch
    AFTER UPDATE ON tb_oper_log FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_oper_log SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_config_touch
    AFTER UPDATE ON tb_config FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_config SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_day_seq_touch
    AFTER UPDATE ON tb_day_seq FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_day_seq SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_order_touch
    AFTER UPDATE ON tb_order FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_order SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_order_item_touch
    AFTER UPDATE ON tb_order_item FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_order_item SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_order_urge_touch
    AFTER UPDATE ON tb_order_urge FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_order_urge SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_payment_touch
    AFTER UPDATE ON tb_payment FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_payment SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_refund_touch
    AFTER UPDATE ON tb_refund FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_refund SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_role_touch
    AFTER UPDATE ON tb_role FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_role SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_user_touch
    AFTER UPDATE ON tb_user FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_user SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_customer_touch
    AFTER UPDATE ON tb_customer FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_customer SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_remember_token_touch
    AFTER UPDATE ON tb_remember_token FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_remember_token SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
CREATE TRIGGER IF NOT EXISTS trg_tb_image_touch
    AFTER UPDATE ON tb_image FOR EACH ROW
    WHEN NEW.update_time IS OLD.update_time
    BEGIN
        UPDATE tb_image SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
    END;
