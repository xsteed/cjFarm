-- 结账小票整张放大开关种子（2026-10-19）：给老库补上出厂配置。
--
-- 背景：新增 print_guest_large_font（结账小票是否整张放大一倍）。
-- 它只是 tb_config 里的一行，**没有结构变更**（无 ALTER / CREATE）；
-- 后端启动时 EnsureSettingDefaults 也会用 INSERT OR IGNORE 补上，
-- 本脚本供「只跑 SQL、不重启后端」的离线升级路径使用，两条路径结果一致。
--
-- 取值：
--   '1' 结账小票整张放大一倍：飞鹅云通道倍宽倍高（<B>），行宽按半个纸宽重排；
--       直连/代理通道不受该开关影响。
--   '0' 保持原版式（出厂默认）。
--
-- 幂等：INSERT OR IGNORE 且 cfg_key 为主键，重复执行不会报错、不会覆盖门店已改的值。

INSERT OR IGNORE INTO tb_config(cfg_key, cfg_value) VALUES('print_guest_large_font', '0');
