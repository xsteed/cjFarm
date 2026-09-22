-- ============================================================================
-- 扫码点餐管理系统 — 种子/存量数据(MySQL 8.0 / 5.7,MariaDB 10.3+)
-- 文件名:seed.sql —— 种子/存量数据(通用命名)
-- ----------------------------------------------------------------------------
-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的种子数据生成,
-- 请勿手工修改;需要调整初始数据时改 Go 定义后重新生成。
--
-- 内容:系统配置 31 项、内置角色 4 个、桌台 8 张、分类 6 个、菜品 21 道、规格 32 条、备注 6 项、打印机 2 台。
-- 执行方式:mysql -u dining -p dining < seed.sql
-- 幂等性  :全部使用 INSERT IGNORE + 显式主键,可重复执行不会产生重复数据。
--
-- 注意:金额一律为「分」;菜品图片与收款码存的是 /uploads/ 虚拟路径,
--       需将 backend/uploads/ 下对应文件放到后端静态托管的 /uploads/ 目录才能显示。
-- ============================================================================

SET NAMES utf8mb4;

INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('agent_token', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('alipay_appid', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('alipay_enabled', '0');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('alipay_notify_url', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('alipay_private_key_path', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('alipay_public_key', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('feie_api_url', 'https://api.de.feieyun.com/Api/Open/');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('feie_ukey', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('feie_user', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('h5_base_url', 'http://localhost:8080');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('pay_qr_ali', '/uploads/pay_ali.jpg');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('pay_qr_wx', '/uploads/pay_wx.png');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('print_enabled', '1');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('print_kitchen_show_price', '0');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('promotion_discount', '0');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('promotion_enabled', '0');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('promotion_threshold', '100');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('seat_fee', '6');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('seat_fee_enabled', '1');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('shop_logo', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('shop_name', '长健农场 柴火农家土菜');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_apiv3_key', '');  -- 敏感项,落库前会自动加密
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_appid', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_enabled', '0');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_mchid', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_notify_url', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_platform_cert_path', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_private_key_path', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_pubkey_id', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_pubkey_path', '');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wxpay_serial_no', '');

-- 桌台:桌台码为固定值,保证「先出脚本、后印二维码」也能对应上;运行期新建的桌台仍使用随机码。
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(1, '01', '大厅01桌', 10, 0, 1, '0', 'A3F7K9M2', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(2, '02', '大厅02桌', 10, 0, 2, '0', 'B4G8L2N3', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(3, '03', '大厅03桌', 10, 0, 3, '0', 'C5H9M3P4', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(4, '04', '大厅04桌', 10, 0, 4, '0', 'D6J2N4Q5', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(5, '07', '大包间(一)', 8, 0, 5, '0', 'E7K3P5R6', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(6, '08', '小包间(二)', 6, 0, 6, '0', 'F8L4Q6S7', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(7, '09', '露台大桌', 8, 0, 7, '0', 'G9M5R7T8', '2026-09-12 13:06:43', '2026-09-13 20:14:16');
INSERT IGNORE INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(8, '10', '露台小桌', 10, 0, 8, '0', 'H2N6S8U9', '2026-09-12 13:06:43', '2026-09-13 20:14:16');

-- 菜品分类
INSERT IGNORE INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(1, '凉菜素菜', 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(2, '荤菜', 2, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(3, '特色农家菜', 3, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(4, '汤品', 4, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(5, '主食', 5, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(6, '海鲜预订', 6, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');

-- 菜品(21 道):category_id 与上面的分类按顺序一一对应。
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(1, 1, '凉拌青瓜', '/uploads/dining_20260918_001.jpeg', '清爽开胃', 1, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(2, 1, '本场时蔬', '/uploads/dining_20260918_002.jpeg', '当日新鲜时令蔬菜', 1, 2, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(3, 1, '清炒腐竹', '/uploads/dining_20260918_003.jpeg', '', 1, 3, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(4, 1, '山泉水豆腐', '/uploads/dining_20260918_004.png', '', 1, 4, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(5, 2, '香煎万绿湖鱼干', '/uploads/dining_20260918_005.png', '推荐加辣', 1, 5, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(6, 2, '砵仔黑土猪肉', '/uploads/dining_20260918_006.png', '', 1, 6, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(7, 2, '盐水猪脚', '/uploads/dining_20260918_007.png', '', 1, 7, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(8, 2, '沙姜猪肚', '/uploads/dining_20260918_008.png', '', 1, 8, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(9, 2, '葱姜焗鱼（清蒸）', '/uploads/dining_20260918_009.png', '', 1, 9, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(10, 2, '秘制萝卜牛腩煲', '/uploads/dining_20260918_010.png', '', 1, 10, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(11, 3, '五指毛桃鸡', '/uploads/dining_20260918_011.png', '', 1, 11, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(12, 3, '茶油蒸长健果园鸡', '/uploads/dining_20260918_012.png', '', 1, 12, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(13, 3, '荔枝果园土鹅', '/uploads/dining_20260918_013.png', '', 1, 13, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(14, 3, '地胆头蒸老鸭', '/uploads/dining_20260918_014.png', '', 1, 14, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(15, 3, '荔枝柴火窑/烧鸡', '/uploads/dining_20260918_015.png', '新鲜宰杀，需提前2小时预约', 1, 15, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(16, 4, '本场黑猪汤', '/uploads/dining_20260918_016.png', '小份3人 / 中份6人 / 大份10人', 1, 16, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(17, 4, '预约柴火炖汤', '/uploads/dining_20260918_017.png', '按位计费，需提前预约，138元起(3-5人)', 1, 17, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(18, 4, '柴火炖鸡', '/uploads/dining_20260918_018.png', '需提前2小时预约', 1, 18, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(19, 4, '柴火炖纯鸡汤', '/uploads/dining_20260918_019.png', '需提前2小时预约', 1, 19, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(20, 5, '长健特色炒饭', '/uploads/dining_20260918_020.png', '', 1, 20, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(21, 6, '海鲜预订（当日时价）', '/uploads/dining_20260918_021.png', '当日时价，下单后店家将与您联系确认', 1, 21, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');

-- 菜品规格(32 条):price 单位为「分」(如 2800 = 28.00 元)。
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(1, 1, '份', 2800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(2, 2, '份', 2800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(3, 3, '份', 3800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(4, 4, '小份', 4800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(5, 4, '大份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(6, 5, '份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(7, 6, '份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(8, 7, '份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(9, 8, '份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(10, 9, '小条', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(11, 9, '大条', 8800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(12, 10, '份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(13, 11, '份', 9800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(14, 11, '只', 18800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(15, 12, '份', 9800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(16, 12, '只', 18800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(17, 13, '份', 9800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(18, 14, '份', 9800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(19, 15, '只', 13800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(20, 16, '小份(3人)', 3800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(21, 16, '中份(6人)', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(22, 16, '大份(10人)', 9800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(23, 17, '23元/位', 2300);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(24, 17, '30元/位', 3000);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(25, 17, '38元/位', 3800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(26, 17, '138元起(3-5人)', 13800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(27, 18, '份', 21800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(28, 19, '份', 21800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(29, 20, '小份', 3800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(30, 20, '中份', 6800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(31, 20, '大份', 8800);
INSERT IGNORE INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(32, 21, '时价', 0);

-- 备注常用语
INSERT IGNORE INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(1, '加辣', 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(2, '微辣', 2, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(3, '免辣', 3, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(4, '少盐', 4, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(5, '少油', 5, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(6, '不要香菜', 6, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');

-- 打印机(默认停用,录入真实 IP 后再启用)
INSERT IGNORE INTO tb_printer(printer_id, printer_name, printer_type, ip, port, paper_width, status, del_flag, create_time, update_time) VALUES(1, '后厨厨房单打印机', 1, '192.168.1.8', 9100, 48, 0, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');
INSERT IGNORE INTO tb_printer(printer_id, printer_name, printer_type, ip, port, paper_width, status, del_flag, create_time, update_time) VALUES(2, '前台食客小票打印机', 2, '192.168.1.201', 9100, 48, 0, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43');

-- 内置角色(4 个):admin 为全量权限,权限矩阵见 internal/store/permission.go
-- 员工账号(tb_user)由后端启动引导自动创建,不在此脚本中预置。
INSERT IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(1, 'admin', '超级管理员', 'table:view,table:edit,category:view,category:edit,dish:view,dish:edit,remark:view,remark:edit,printer:view,printer:edit,order:view,order:operate,order:settle,order:edit,order:cancel,credit:view,credit:settle,refund:view,refund:operate,report:view,config:view,config:edit,user:view,user:edit,role:view,role:edit,log:view,log:manage', 'all', 1, 1, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '拥有全部权限。权限不可修改,每次启动强制恢复为全量,防止锁死系统。');
INSERT IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(2, 'manager', '店长', 'table:view,table:edit,category:view,category:edit,dish:view,dish:edit,remark:view,remark:edit,printer:view,printer:edit,order:view,order:operate,order:settle,order:edit,order:cancel,credit:view,credit:settle,refund:view,report:view,config:view,config:edit,log:view', 'all', 1, 2, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '日常最高权限:可改配置、看报表、查操作日志,但不能管理员工与权限,也不能发起退款。');
INSERT IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(3, 'cashier', '收银员', 'table:view,category:view,dish:view,remark:view,printer:view,order:view,order:operate,order:settle,order:edit,order:cancel,credit:view,credit:settle,refund:view', 'all', 1, 3, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '前厅收银:处理订单全流程与挂账核销,不涉及配置、报表与退款发起。');
INSERT IGNORE INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(4, 'staff', '员工', 'table:view,category:view,dish:view,remark:view,order:view,order:operate', 'all', 1, 4, 1, '0', '2026-09-12 13:06:43', '2026-09-12 13:06:43', '后厨/服务员通用:只读基础资料,订单可流转(接单/上菜/完成/处理催菜)。');
