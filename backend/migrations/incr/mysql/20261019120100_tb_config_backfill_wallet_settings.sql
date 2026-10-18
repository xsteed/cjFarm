-- 储值合规参数种子（2026-10-19）：与上一条结构脚本配套，给老库补上出厂配置。
--
-- 为什么单独一个文件：结构（DDL）与种子（数据）分开，DBA 只做结构升级时
-- 不会顺带覆盖门店已改过的配置 —— INSERT IGNORE 保证已有值不被改写。
--
-- 取值口径（出厂默认按最严格的监管口径，见 docs/compliance/prepaid-card.md）：
--   wallet_recharge_enabled   0  自助充值默认关（动钱的入口 fail-closed）
--   wallet_recharge_min/max   1 / 1000  单笔充值区间（元，只收整数元）
--   wallet_balance_max        1000  未记名账户余额上限（元）
--   wallet_balance_max_named  5000  已记名账户余额上限（元）
--   wallet_realname_required  1     充值前强制实名（绑手机号）
--   wallet_adjust_max         1000  后台单次调账上限（元），防误操作
--   wallet_rules_version      2026.1  《储值规则》版本；改正文必须改版本号
--   wallet_rules_text         出厂规则正文（覆盖适用范围/有效期/限额/退款/注销/凭证）
--
-- 上限 1000 / 5000 出自《单用途商业预付卡管理办法（试行）》关于单张不记名卡与
-- 记名卡限额的规定（阈值以现行条文为准）。门店要调高，须先完成相应的发卡备案。
--
-- 幂等：INSERT IGNORE 且 cfg_key 为主键，重复执行不会报错、不会覆盖已有配置。

INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_recharge_enabled', '0');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_recharge_min', '1');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_recharge_max', '1000');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_balance_max', '1000');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_balance_max_named', '5000');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_realname_required', '1');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_adjust_max', '1000');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_rules_version', '2026.1');
INSERT IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('wallet_rules_text', '一、适用范围：储值余额仅限本店堂食消费抵扣，不找零、不兑现、不计息、不可转赠他人。二、有效期：储值余额不设有效期，长期有效。三、实名与限额：未绑定手机号的账户为不记名账户，余额上限 1000 元；完成手机号绑定后为记名账户，余额上限 5000 元。四、退款：本金余额可随时向门店申请退回；充值赠送的金额（赠金）仅可用于消费，不可退现。五、注销：注销会员前须先将余额退回或消费完毕，余额未清零时无法注销。六、凭证与查询：本店完整保存每一笔储值流水，顾客可在本页随时查询余额与明细。七、安全提示：余额支付视同本人消费，请妥善保管微信账号，如有异常请及时联系门店。');
