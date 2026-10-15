-- 打印台账补「票据标识 + 票面快照」三列（incr 29）
--
-- tb_print_log 新增三列:
--   label        这张票的可读标识(拆单厨房单为「【厨房单】1/4」,其余为票种名,测试页为空)。
--                必须独立成列而不是拼在 detail 前面:代理通道的回执、重试、过期作废
--                都会整段改写 detail(见 dao.UpdatePrintLogResult),拼进去的标识
--                一到那一步就没了 —— 而「一菜一单」一次下单会写多条台账,
--                不写第几张在记录页上分不清丢的是哪一张。
--   content      票面快照:这次实际送出的那张纸的完整文本行(JSON,见 print/snapshot.go)。
--                有了它,「打印记录」页的预览与「补打」都是回放这张票,而不是拿
--                现在的订单与现在的店铺配置重算一遍 —— 订单后来加过菜、店名/页脚
--                后来改过,都不会再改变一条历史记录的还原结果。
--   paper_width  生成这些行时使用的纸宽(32=58mm / 48=80mm),预览按它画网格,
--                打印机后来改过纸宽也不会让历史记录的行宽对不上。
--
-- 适用范围:仅用于存量库升级;新库请直接执行 migrations/full/{mysql,sqlite}/schema.sql
-- (建表语句已包含这三列)。须在 20261015010000(时间列补齐)之后执行。
--
-- 老记录:label / content 留空,读侧(预览/补打)回落到按当前数据重算,行为不倒退;
--        不做解析回填 —— 历史标识拼在 detail 里,靠字符串硬拆比留空更容易错。
-- 保留策略:三列与台账同寿、长期保留,不做清理(content 为空即代表无快照)。
--
-- 幂等性:ADD COLUMN 在列已存在时会报 duplicate column name,属可忽略的正常现象
-- (与启动补列被跳过是同一回事);label / paper_width 带 DEFAULT,存量行会被一并填上
-- (label='' / paper_width=48),content 不带默认值、存量为 NULL,读侧统一 COALESCE。
-- SQLite 的 ADD COLUMN 不支持 AFTER,三列追加在表尾;列序无关紧要,查询全部显式列名。

ALTER TABLE tb_print_log ADD COLUMN label VARCHAR(64) DEFAULT '';
ALTER TABLE tb_print_log ADD COLUMN content TEXT;
ALTER TABLE tb_print_log ADD COLUMN paper_width INTEGER DEFAULT 48;
