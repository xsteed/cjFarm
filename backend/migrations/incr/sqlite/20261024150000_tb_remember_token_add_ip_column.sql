-- 登录设备页展示来源 IP（2026-10-24）：tb_remember_token 增加 ip 列。
--
-- 背景：设备管理页需要让登录者看到「这台设备是从哪来的」，便于识别陌生 / 异地登录。
-- ip 记录签发时的客户端 IP，并在每次免登录（记住我）使用时刷新为最近一次使用的 IP。
--
-- 取值来源：handler 的 c.ClientIP()（受 TRUSTED_PROXIES 约束；经 nginx 等反代部署时
-- 需配置可信代理，否则取到的是网关地址而非真实客户端 IP）。
-- 长度：VARCHAR(64)，覆盖 IPv4、IPv4-mapped 与 IPv6 文本表示。
--
-- 幂等与重复执行：
--   - ALTER TABLE ADD COLUMN 两库都不支持 IF NOT EXISTS，列已存在时会报
--     duplicate column name，属正常现象、可忽略（见 migrations/README.md 第三节）。
--   - 本脚本与后端启动时 store.alterCols 的自动补列等价，执行其一即可；
--     两者都跑也不会出错（自动补列会跳过已存在的列）。
--   - 存量行补为空串（设备管理页显示「-」）；下一次免登录使用后自动写入真实 IP。

ALTER TABLE tb_remember_token ADD COLUMN ip VARCHAR(64) DEFAULT '';
