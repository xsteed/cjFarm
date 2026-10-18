-- 36 号:访问明细补「终端与来源」六列。
--
-- 上线时的 tb_visit_log 只有 IP/UA 的哈希列(供 UV 去重),看不到访客的
-- 终端与来源。本期补齐(明细保留 90 天,与操作日志 oper_ip 明文同一取舍):
--   ip      明文 IP(IPv6 文本最长 45 字符),排查与展示用
--   city    地理位置描述(如「中国 广东省 广州市」「内网」),来自
--           ip2region 离线库(data/ip2region.xdb,缺文件时为空)
--   os      操作系统归一值(iOS / Android / Windows / macOS / 鸿蒙 / Linux / 其他)
--   browser 浏览器归一值(微信 / 支付宝 / Chrome / Safari / Edge …)
--   device  设备类型归一值(手机 / 平板 / 电脑 / 其他)
--   model   具体机型(仅小程序端能精确直传,H5 留空)
--
-- 历史行为空,不做回填(当时未采集)。
-- 幂等性:ADD COLUMN 列已存在时报 duplicate column name,属可忽略的正常现象。
ALTER TABLE tb_visit_log ADD COLUMN ip VARCHAR(45) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN city VARCHAR(64) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN os VARCHAR(16) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN browser VARCHAR(16) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN device VARCHAR(8) DEFAULT '';
ALTER TABLE tb_visit_log ADD COLUMN model VARCHAR(64) DEFAULT '';
