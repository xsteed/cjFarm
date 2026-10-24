package store

import (
	"fmt"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
)

// ============================================================================
// 建表 DDL
// ============================================================================
//
// 模板中的占位符(由 schemaStatements 按方言替换):
//
//	{{PK}}   自增主键列定义   SQLite: INTEGER PRIMARY KEY AUTOINCREMENT
//	                          MySQL : INT NOT NULL AUTO_INCREMENT PRIMARY KEY
//	{{OPTS}} 建表尾选项       MySQL : ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ...
//	{{BLOB}} 大二进制列类型   SQLite: BLOB
//	                          MySQL : LONGBLOB(BLOB 上限 64KB,装不下单张上传图)
//	{{TEXT}} 大文本列类型     SQLite: TEXT
//	                          MySQL : LONGTEXT(见 textTypeFor;不带 DEFAULT)
//
// 类型选择原则(两库语义尽量对齐):
//   - 主键/外键/金额/计数 一律 INTEGER:SQLite 原生 64 位,MySQL 映射为 INT(≈±21 亿,
//     金额单位为「分」,单笔上限约 2100 万元,足够覆盖餐饮单笔订单);
//   - 字符串一律 VARCHAR(n):SQLite 归入 TEXT 亲和性且不校验长度,MySQL 为变长字符串
//     并强制长度上限 —— 这是两库唯一的显式行为差异,写入前请遵守长度约定;
//   - 业务时间列(支付/打印/登录/用餐等)一律 VARCHAR(32) 存 'YYYY-MM-DD HH:MM:SS' 字符串,
//     两库排序/区间比较行为一致,避免 TIMESTAMP 的时区与零值('0000-00-00')差异;
//   - 仅 create_time / update_time 按方言分叉(见 tsTypeFor):MySQL 为 DATETIME,
//     由数据库负责默认值与自动更新;SQLite 仍为 VARCHAR(32) + datetime('now') 默认值 + 触发器。
var schemaTemplate = []string{
	`CREATE TABLE IF NOT EXISTS tb_table (
		table_id    {{PK}},
		table_no    VARCHAR(32)  NOT NULL,
		table_name  VARCHAR(64)  NOT NULL,
		capacity    INTEGER      DEFAULT 0,
		status      INTEGER      DEFAULT 0,
		lock_status INTEGER      DEFAULT 0,
		lock_remark VARCHAR(255) DEFAULT '',
		sort_order  INTEGER      DEFAULT 0,
		del_flag    VARCHAR(2)   DEFAULT '0',
		create_by   VARCHAR(64)  DEFAULT '',
		update_by   VARCHAR(64)  DEFAULT '',
		remark      VARCHAR(500),
		table_code  VARCHAR(16)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_category (
		category_id   {{PK}},
		category_name VARCHAR(64) NOT NULL,
		sort_order    INTEGER     DEFAULT 0,
		del_flag      VARCHAR(2)  DEFAULT '0',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_dish (
		dish_id     {{PK}},
		category_id INTEGER      NOT NULL,
		dish_name   VARCHAR(128) NOT NULL,
		dish_image  VARCHAR(255) DEFAULT '',
		description VARCHAR(500) DEFAULT '',
		status      INTEGER      DEFAULT 1,
		sold_out    INTEGER      DEFAULT 0,
		signature   INTEGER      DEFAULT 0,
		sort_order  INTEGER      DEFAULT 0,
		del_flag    VARCHAR(2)   DEFAULT '0',
		create_by   VARCHAR(64)  DEFAULT '',
		update_by   VARCHAR(64)  DEFAULT '',
		remark      VARCHAR(500),
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_spec (
		spec_id   {{PK}},
		dish_id   INTEGER      NOT NULL,
		spec_name VARCHAR(64)  NOT NULL,
		price     INTEGER      DEFAULT 0,
		stock     INTEGER      DEFAULT -1,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_remark (
		remark_id   {{PK}},
		option_name VARCHAR(64) NOT NULL,
		sort_order  INTEGER     DEFAULT 0,
		del_flag    VARCHAR(2)  DEFAULT '0',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// provider 决定「怎么把票据送出去」:
	//   - tcp   直连网络热敏机,后端拼 ESC/POS 指令走 IP:9100,
	//           要求后端与打印机在同一局域网(云端部署连不上门店打印机);
	//   - feie  飞鹅云打印机,后端按开放平台 HTTP 接口推单,
	//           打印机自己联网到飞鹅云取单,后端在哪里都能用;
	//   - agent 本地打印代理:后端只入队(tb_print_job),门店内网的代理程序
	//           出站拉单后再向 ip:port 直发。云后端 + 门店已有 9100 网络机的场景用它。
	// ip/port 三种通道都要填:agent 通道下它是「打印机在门店内网的地址」,
	// 由代理程序使用,云后端自己不会去连(见 docs/print-agent.md)。
	// category_ids 为「按菜品分类分单」:CSV 分类 ID,空串=收全部菜品。
	// 典型用法是凉菜/热菜各一台厨房机,不同分类的菜分别落到对应机器上。
	`CREATE TABLE IF NOT EXISTS tb_printer (
		printer_id   {{PK}},
		printer_name VARCHAR(64) NOT NULL,
		printer_type INTEGER     DEFAULT 1,
		provider     VARCHAR(16) DEFAULT 'tcp',
		ip           VARCHAR(64) DEFAULT '',
		port         INTEGER     DEFAULT 9100,
		feie_sn      VARCHAR(64) DEFAULT '',
		paper_width  INTEGER     DEFAULT 48,
		copies       INTEGER     DEFAULT 1,
		category_ids VARCHAR(255) DEFAULT '',
		status       INTEGER     DEFAULT 1,
		del_flag     VARCHAR(2)  DEFAULT '0',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 打印日志:每「向一台打印机发一次单据」记一行(成功与失败都记)。
	// 打印本身是异步的、且打印机在线与否不受后端控制,没有这张表时
	// 失败了只有后端日志留痕,商家端完全无感知(小票没出来也不知道为什么)。
	// detail 对 TCP 存「地址 / 失败原因」,对飞鹅存「云端订单号」,
	// remote_id 单独存飞鹅订单号,便于用 Open_queryOrderState 追问是否真的打出来了。
	//
	// label 是这张票的可读标识(拆单厨房单为「【厨房单】1/4」,其余为票种名;
	// 测试页为空)。必须是独立列而不是拼在 detail 前面:代理通道的回执、重试、
	// 过期作废都会整段改写 detail(见 dao.UpdatePrintLogResult),拼进去的标识
	// 一到那一步就没了 —— 而「一菜一单」下一次下单会写多条台账,不写第几张
	// 在记录页上根本分不清丢的是哪一张。
	//
	// content / paper_width 是「票面快照」:content 存这次实际送出的每一张纸的
	// 完整文本行(JSON),paper_width 存当时的纸宽。有了它,「打印记录」里的预览与
	// 「补打」都是回放这张票,而不是拿现在的订单与现在的店铺配置重算一遍 ——
	// 订单后来加过菜、店名或页脚后来改过,都不会再改变一条历史记录的还原结果。
	// 老记录(升级前)该列为空,读侧回落到重算(见 print/snapshot.go)。
	// 三列都长期保留(与台账同寿),不做清理。
	`CREATE TABLE IF NOT EXISTS tb_print_log (
		print_id     {{PK}},
		order_id     INTEGER      DEFAULT 0,
		order_no     VARCHAR(64)  DEFAULT '',
		table_no     VARCHAR(32)  DEFAULT '',
		table_name   VARCHAR(64)  DEFAULT '',
		printer_id   INTEGER      DEFAULT 0,
		printer_name VARCHAR(64)  DEFAULT '',
		printer_type INTEGER      DEFAULT 1,
		provider     VARCHAR(16)  DEFAULT 'tcp',
		doc_type     VARCHAR(16)  DEFAULT '',
		copies       INTEGER      DEFAULT 1,
		status       INTEGER      DEFAULT 0,
		remote_id    VARCHAR(128) DEFAULT '',
		label        VARCHAR(64)  DEFAULT '',
		detail       VARCHAR(500) DEFAULT '',
		content      {{TEXT}},
		paper_width  INTEGER      DEFAULT 48,
		trigger_by   VARCHAR(16)  DEFAULT '',
		operator     VARCHAR(64)  DEFAULT '',
		cost_ms      INTEGER      DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 本地打印代理任务队列(tb_printer.provider='agent' 的打印机使用)。
	//
	// 为什么需要一张表:后端部署在云服务器时够不到门店内网的打印机(见 escpos.go
	// 顶部说明),只能把票据存下来等门店内网的代理程序主动来取。这也顺带带来了
	// 「打印机离线不丢单」——代理恢复后会把积压的任务依次补打出来。
	//
	// payload 存的是渲染后的等宽文本行(以 '\n' 连接),不是 ESC/POS 字节:
	//   1. 文本行与厂商无关,与直连/飞鹅云共用同一份渲染结果(ticket.go);
	//   2. ESC/POS 字节含 GBK 编码与切纸指令,在代理取单时现场编码后以 base64 下发,
	//      代理程序因此完全不需要理解打印协议,换个语言重写代理也不用改协议;
	//   3. 长度可控:VARCHAR(4000) 约合 80 行 80mm 小票,超出时后端按行拆成多条任务
	//      (与飞鹅云超长内容分段推送同一策略)。
	//
	// status: 0 待取单 / 1 已取单(带租约) / 2 已送出 / 3 放弃(重试次数用尽)。
	`CREATE TABLE IF NOT EXISTS tb_print_job (
		job_id       {{PK}},
		printer_id   INTEGER       DEFAULT 0,
		printer_name VARCHAR(64)   DEFAULT '',
		printer_type INTEGER       DEFAULT 1,
		ip           VARCHAR(64)   DEFAULT '',
		port         INTEGER       DEFAULT 9100,
		doc_type     VARCHAR(16)   DEFAULT '',
		order_id     INTEGER       DEFAULT 0,
		order_no     VARCHAR(64)   DEFAULT '',
		table_no     VARCHAR(32)   DEFAULT '',
		copies       INTEGER       DEFAULT 1,
		print_log_id INTEGER       DEFAULT 0,
		delivery_id  VARCHAR(36)   DEFAULT '',
		payload      VARCHAR(4000) DEFAULT '',
		status       INTEGER       DEFAULT 0,
		attempts     INTEGER       DEFAULT 0,
		last_error   VARCHAR(500)  DEFAULT '',
		claimed_by   VARCHAR(64)   DEFAULT '',
		claim_time VARCHAR(32) DEFAULT NULL,
		next_try_time VARCHAR(32) DEFAULT NULL,
		trigger_by   VARCHAR(16)   DEFAULT '',
		operator     VARCHAR(64)   DEFAULT '',
		done_time VARCHAR(32) DEFAULT NULL,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 本地打印代理身份(per-agent 令牌)。
	// 全局 agent_token 继续作为 legacy 兼容通道;新表给 v2 代理逐台签发令牌,
	// 可单独吊销、可限定 printer_ids 授权范围,降低单台代理令牌泄漏后的队列暴露面。
	`CREATE TABLE IF NOT EXISTS tb_print_agent (
		agent_id    {{PK}},
		agent_name  VARCHAR(64)  DEFAULT '',
		token_hash  VARCHAR(64)  DEFAULT '',
		token_hint  VARCHAR(8)   DEFAULT '',
		printer_ids VARCHAR(255) DEFAULT '',
		status      INTEGER      DEFAULT 1,
		last_seen VARCHAR(32) DEFAULT NULL,
		last_report VARCHAR(64)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 操作日志(审计留痕):管理端每次写操作落一行,只追加、不修改。
	//
	// 业务表上的 create_by / update_by 只回答「谁改的」,回答不了
	// 「什么时候、传了什么参数、成功了没」。权限变更、免单、退款、改价这类
	// 事后必须能说清楚的动作,全靠这张表兜底。
	//
	// operator / operator_role 存的是快照:员工改名或换角色后,历史日志不应跟着变。
	// target_type + target_id 让「这张订单被谁动过」可以直接查,不必扫全表。
	`CREATE TABLE IF NOT EXISTS tb_oper_log (
		log_id        {{PK}},
		module        VARCHAR(32)   DEFAULT '',
		business_type VARCHAR(16)   DEFAULT 'other',
		action        VARCHAR(64)   DEFAULT '',
		method        VARCHAR(255)  DEFAULT '',
		request_url   VARCHAR(255)  DEFAULT '',
		operator_id   INTEGER       DEFAULT 0,
		operator      VARCHAR(64)   DEFAULT '',
		operator_role VARCHAR(64)   DEFAULT '',
		oper_ip       VARCHAR(64)   DEFAULT '',
		target_type   VARCHAR(32)   DEFAULT '',
		target_id     VARCHAR(64)   DEFAULT '',
		oper_param    VARCHAR(2000) DEFAULT '',
		detail        VARCHAR(500)  DEFAULT '',
		status        INTEGER       DEFAULT 0,
		error_msg     VARCHAR(500)  DEFAULT '',
		cost_ms       INTEGER       DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_config (
		cfg_key   VARCHAR(64)   PRIMARY KEY,
		cfg_value VARCHAR(4000) DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// tb_day_seq 按营业日维护取餐号(pickup_no)计数器。
	//
	// 存在的前提:内部单号 order_no 必须保持不可枚举 —— 顾客端
	// `/customer/order/:orderNo`(handler/customer.go)免登录即可凭单号查单,
	// 自增或雪花这类线性编号等于开放对全量订单的遍历。而 20 位随机串又长到无法口头报读。
	// 因此另起一套「当天从 1 递增」的短号,专供顾客报号与厨房/服务员叫号:
	//   - 写入路径只有「下单」一处,自增在事务行锁下串行化(见 dao.NextPickupNoTx);
	//   - 计数行按 biz_date 作主键而非自增 INTEGER,故此处手写 PRIMARY KEY 而非 {{PK}}。
	`CREATE TABLE IF NOT EXISTS tb_day_seq (
		biz_date VARCHAR(10) PRIMARY KEY,
		next_val INTEGER      DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_order (
		order_id        {{PK}},
		order_no        VARCHAR(64)  NOT NULL,
		table_id        INTEGER,
		table_no        VARCHAR(32),
		table_name      VARCHAR(64),
		person_count    INTEGER      DEFAULT 1,
		order_status    INTEGER      DEFAULT 1,
		dish_amount     INTEGER      DEFAULT 0,
		seat_fee        INTEGER      DEFAULT 0,
		discount_amount INTEGER      DEFAULT 0,
		total_amount    INTEGER      DEFAULT 0,
		pay_status      INTEGER      DEFAULT 0,
		pay_type        VARCHAR(16),
		pay_time VARCHAR(32) DEFAULT NULL,
		transaction_id  VARCHAR(64)  DEFAULT '',
		pay_channel     VARCHAR(16)  DEFAULT '',
		refund_amount   INTEGER      DEFAULT 0,
		refund_time VARCHAR(32) DEFAULT NULL,
		finish_time VARCHAR(32) DEFAULT NULL,
		order_remark    VARCHAR(500) DEFAULT '',
		cancel_reason   VARCHAR(255) DEFAULT '',
		create_by       VARCHAR(64)  DEFAULT '',
		update_by       VARCHAR(64)  DEFAULT '',
		remark          VARCHAR(500),
		settle_type     VARCHAR(16)  DEFAULT 'normal',
		settle_time VARCHAR(32) DEFAULT NULL,
		settle_operator VARCHAR(64)  DEFAULT '',
		settle_remark   VARCHAR(255) DEFAULT '',
		credit_status   INTEGER      DEFAULT 0,
		credit_amount   INTEGER      DEFAULT 0,
		credit_settle_time VARCHAR(32) DEFAULT NULL,
		credit_settle_by   VARCHAR(64) DEFAULT '',
		paid_amount     INTEGER      DEFAULT 0,
		-- 赠送金额(分):赠送菜品按原价计的金额合计,不计入应收金额(让利口径)。
		gift_amount     INTEGER      DEFAULT 0,
		-- 下单会员:0 = 匿名(历史单与未登录单);会员体系见 tb_customer。
		-- 请求体不带该值,一律以顾客令牌为准(防止伪造归属)。
		customer_id     INTEGER      DEFAULT 0,
		-- 结账时(状态被置 4 前)的订单状态快照:撤销结算时恢复到结账前的真实状态,
		-- 而不是无条件回到 3(订单可能在 1/2 状态被提前结账,撤销后应回到 1/2 继续制作流程)。
		-- 0 表示历史数据未记录(撤销时回退为 3)。
		pre_settle_status INTEGER     DEFAULT 0,
		-- 营业日(YYYY-MM-DD)与当日取餐号:专供顾客口头报号 / 厨房叫号,每天从 1 递增,
		-- 与不可枚举的内部单号 order_no 各司其职(见 tb_day_seq 的注释)。
		-- biz_date 缺省 NULL 而非空串的用意:让「非下单写入的行」不参与下面
		-- (biz_date, pickup_no) 的唯一判定 —— 两种数据库对含 NULL 的组合都不判重复。
		biz_date   VARCHAR(10) DEFAULT NULL,
		pickup_no  INTEGER     DEFAULT 0,
		-- 用餐起止时间(落库值):
		--   meal_start_time —— 下单时写入(用餐开始);
		--   meal_end_time   —— 订单走到终态(4 已完成 / 5 已取消)时写入(用餐结束)。
		-- 默认 NULL 而非空串:区分「还没结束」与「时间未知」,回填脚本与报警 SQL 一眼明了。
		meal_start_time VARCHAR(32) DEFAULT NULL,
		meal_end_time VARCHAR(32) DEFAULT NULL,
		-- 商家首次实际操作该订单的时间:NULL = 还没操作过(仅打开详情查看不算)。
		-- 列表据此给「已下单」的未操作订单打「新」角标,任意操作后写入,只写一次。
		viewed_at VARCHAR(32) DEFAULT NULL,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_order_item (
		item_id   {{PK}},
		order_id  INTEGER      NOT NULL,
		dish_id   INTEGER,
		dish_name VARCHAR(128),
		spec_id   INTEGER,
		spec_name VARCHAR(64),
		price     INTEGER      DEFAULT 0,
		quantity  INTEGER      DEFAULT 1,
		amount    INTEGER      DEFAULT 0,
		remark    VARCHAR(255) DEFAULT '',
		-- 加菜批次:1=首次下单,每次加菜 +1,用于菜品明细里区分「首次下单/第N次加菜」。
		batch_no  INTEGER      DEFAULT 1,
		-- 幂等键:同一次下单/加菜提交的所有明细共享同一 client_request_id,用于去重(适当冗余)。
		client_request_id VARCHAR(64) DEFAULT '',
		-- 是否赠送:0 正常计费 1 赠送(商家改单标记,amount 记 0,原价计入订单 gift_amount)。
		is_gift   INTEGER      DEFAULT 0,
		gift_reason VARCHAR(255) DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_order_urge (
		urge_id     {{PK}},
		order_id    INTEGER      NOT NULL,
		order_no    VARCHAR(64)  NOT NULL,
		table_id    INTEGER      NOT NULL,
		table_no    VARCHAR(32)  DEFAULT '',
		table_name  VARCHAR(64)  DEFAULT '',
		urge_type   VARCHAR(16)  DEFAULT 'urge',
		status      INTEGER      DEFAULT 0,
		remark      VARCHAR(255) DEFAULT '',
		handle_time VARCHAR(32)  DEFAULT NULL,
		handle_by   VARCHAR(64)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 订单评价(仅商家可见的内部反馈通道,见 docs/design-docs/order-review/spec.md):
	// 追溯字段(桌台快照/人数/会员昵称与手机号)提交时一次性写死,不联表 ——
	// 会员注销会物理删除档案并把订单归属置 0,联表方案的追溯链会断裂。
	// ip_hash/ua_hash 是 sha256(auth.key|值) 胡椒哈希,明文 IP 永不落库。
	`CREATE TABLE IF NOT EXISTS tb_review (
		review_id    {{PK}},
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
		reviewer_key VARCHAR(64)  DEFAULT '',
		ip_hash      VARCHAR(64)  DEFAULT '',
		ua_hash      VARCHAR(64)  DEFAULT '',
		create_time  {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time  {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 订单评价图片:与 tb_image 刻意分开的两张表 —— 菜品图是商家管理的资产、
	// 评价图是顾客上传的用户内容,生命周期(级联删除/孤儿清理)与合规属性完全不同,
	// 混存一张表既无法区分业务,将来迁对象存储(COS)时也难以整表筛选搬迁。
	// 图片本体仍以 BLOB 入库(与 tb_image 同一运行时模型,展示统一走 /uploads/{name},
	// 见 service.GetImage 的两级回落);review_id=0 表示「已上传未绑定评价」,
	// 由后台孤儿清理任务按 create_time 回收。
	`CREATE TABLE IF NOT EXISTS tb_review_image (
		image_id     {{PK}},
		review_id    INTEGER      DEFAULT 0,
		img_name     VARCHAR(128) NOT NULL,
		content_type VARCHAR(32)  DEFAULT '',
		img_data     {{BLOB}},
		file_size    INTEGER      DEFAULT 0,
		create_time  {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time  {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_payment (
		payment_id       {{PK}},
		order_no         VARCHAR(64)  NOT NULL,
		channel          VARCHAR(16)  NOT NULL,
		channel_trade_no VARCHAR(64)  DEFAULT '',
		amount           INTEGER      DEFAULT 0,
		status           INTEGER      DEFAULT 0,
		prepay_id        VARCHAR(128) DEFAULT '',
		notify_time VARCHAR(32) DEFAULT NULL,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_refund (
		refund_id         {{PK}},
		order_id          INTEGER      NOT NULL,
		order_no          VARCHAR(64)  NOT NULL,
		payment_id        INTEGER      NOT NULL,
		refund_no         VARCHAR(64)  NOT NULL,
		channel           VARCHAR(16)  NOT NULL,
		channel_refund_no VARCHAR(64)  DEFAULT '',
		amount            INTEGER      DEFAULT 0,
		status            INTEGER      DEFAULT 0,
		reason            VARCHAR(255) DEFAULT '',
		operator          VARCHAR(64)  DEFAULT '',
		fail_reason       VARCHAR(255) DEFAULT '',
		-- 1 = 重复支付自动退回(顾客在两个渠道各付了一笔,后到的一笔原路退回)。
		-- 这类退款的金额从未计入订单营收(paid_amount),因此不计入订单退款额度、
		-- 也不扣减订单实收 —— 与普通退款(SumRefunded/applyRefundSuccess)严格区分。
		is_duplicate      INTEGER      DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// ---- 员工与权限(见 docs/user-permission-design.md) ----
	//
	// perms 用 CSV 而非独立映射表:权限点共 26 个、最长 CSV 约 450 字符,
	// VARCHAR(4000) 余量充足;角色个位数且永远整读,没有「反查哪些角色拥有 X」的需求。
	// 代价是不能用 SQL 直接按权限点筛选角色(当前不需要);若将来需要,
	// 再加映射表并做一次回填即可,不影响现有结构。
	`CREATE TABLE IF NOT EXISTS tb_role (
		role_id     {{PK}},
		role_key    VARCHAR(32)   NOT NULL,
		role_name   VARCHAR(64)   NOT NULL,
		perms       VARCHAR(4000) DEFAULT '',
		data_scope  VARCHAR(16)   DEFAULT 'all',
		is_builtin  INTEGER       DEFAULT 0,
		sort_order  INTEGER       DEFAULT 0,
		status      INTEGER       DEFAULT 1,
		del_flag    VARCHAR(2)    DEFAULT '0',
		remark      VARCHAR(500)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// real_name 用于订单操作留痕(显示中文姓名比显示登录名直观);
	// token_version 在改密/强制下线时 +1,使旧令牌立即失效。
	`CREATE TABLE IF NOT EXISTS tb_user (
		user_id         {{PK}},
		username        VARCHAR(64)  NOT NULL,
		password_hash   VARCHAR(255) DEFAULT '',
		real_name       VARCHAR(64)  DEFAULT '',
		role_id         INTEGER      NOT NULL DEFAULT 0,
		phone           VARCHAR(32)  DEFAULT '',
		status          INTEGER      DEFAULT 1,
		last_login_time VARCHAR(32) DEFAULT NULL,
		last_login_ip   VARCHAR(64)  DEFAULT '',
		login_count     INTEGER      DEFAULT 0,
		pwd_update_time VARCHAR(32) DEFAULT NULL,
		token_version   INTEGER      DEFAULT 0,
		del_flag        VARCHAR(2)   DEFAULT '0',
		create_by       VARCHAR(64)  DEFAULT '',
		update_by       VARCHAR(64)  DEFAULT '',
		remark          VARCHAR(500) DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 顾客会员档案(见 docs/design-docs/customer-member-identity/spec.md)。
	// 本表只存「账号」,与登录方式无关:同一份档案可以绑小程序 openid、手机号、网站密码…
	// 具体绑定关系在 tb_customer_identity,账号可用性只由本表的 status 裁决。
	// 不存 session_key(一期不解密手机号等敏感数据,最小化收集);
	// 无 del_flag:会员不做软删除,注销直接物理删(档案不含敏感信息,见 spec 2.6)。
	// 昵称/头像由用户主动填写,不强制,未填时前端显示「微信用户」。
	//
	// 历史:openid 曾直接做本表的唯一键。P0 拆表时保留了一版(过渡),
	// P2 已彻底删除 —— 老库由启动迁移 migrateCustomerOpenid 搬迁并删列,
	// 新库不再有这一列。登录查档一律走 tb_customer_identity。
	`CREATE TABLE IF NOT EXISTS tb_customer (
		customer_id      {{PK}},
		nickname         VARCHAR(64)  DEFAULT '',
		avatar_url       VARCHAR(255) DEFAULT '',
		status           INTEGER      DEFAULT 1,
		order_count      INTEGER      DEFAULT 0,
		last_order_time VARCHAR(32) DEFAULT NULL,
		last_login_time VARCHAR(32) DEFAULT NULL,
		remark           VARCHAR(255) DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 顾客登录方式(账号 ↔ 登录凭据 的绑定)。
	//
	// (provider, external_id) 唯一:一个登录凭据只能指向一个会员,同时兜底
	// 「并发首登」—— 两个请求同时建档时,后到者撞唯一约束,service 层据此
	// 回滚整个事务并按 (provider, external_id) 重查走登录分支。
	// 反过来一个会员可以有多条(小程序 + 公众号 + 手机号…),这是 App/网站
	// 共用同一份会员档案的基础。
	//
	// unionid 建普通索引而非唯一索引:同一个人的多个 openid 共享同一个 unionid,
	// 建唯一索引会直接冲突。
	// 不设 status:账号可用性只由 tb_customer.status 裁决;解绑 = 物理删行。
	`CREATE TABLE IF NOT EXISTS tb_customer_identity (
		identity_id      {{PK}},
		customer_id      INTEGER      NOT NULL,
		provider         VARCHAR(32)  NOT NULL,
		external_id      VARCHAR(128) NOT NULL,
		credential       VARCHAR(255) DEFAULT '',
		unionid          VARCHAR(64)  DEFAULT NULL,
		last_login_time {{TSTYPE}} DEFAULT NULL,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 储值账户(1:1 挂在会员上,主键即 customer_id,不另设自增 ID)。
	//
	// 金额一律以「分」存 INTEGER(全库统一口径,见 migrations/README.md 第七节)。
	// 余额只由 wallet DAO 在事务内增减(带 balance>=? 守卫),任何绕过 DAO 的
	// 直接 UPDATE 都可能把余额写成负数 —— 流水与余额对不上时以流水为准。
	//
	// 两个余额桶:balance 本金 / balance_gift 赠金(见 po.Wallet 的说明)。
	// 消费时先扣赠金、再扣本金;退余额只退本金 —— 这是「赠金不可套现」的落点。
	//
	// 不设 del_flag:会员注销前必须把余额清零(退余额或调账),注销事务里
	// 删账户行 —— 绝不允许「会员没了、钱还挂在账上」(见 dao.DeleteCustomerAccount)。
	`CREATE TABLE IF NOT EXISTS tb_wallet (
		customer_id    INTEGER PRIMARY KEY,
		balance        INTEGER      DEFAULT 0,
		balance_gift   INTEGER      DEFAULT 0,
		total_recharge INTEGER      DEFAULT 0,
		total_consume  INTEGER      DEFAULT 0,
		total_gift     INTEGER      DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 储值流水(台账,只增不改 —— 调账/退款都追加新行,余额变动必须与流水对得上)。
	//
	// biz_type: recharge 充值 / consume 消费 / refund 退回 / gift 后台赠送 /
	//           gift_void 赠金作废 / gift_back 赠金退回(撤销) / adjust 后台扣减 /
	//           withdraw 退余额出店。
	// amount 带符号:正数入账、负数出账;balance_after 是本笔变动后的本金余额,
	// 逐行累加必须等于当前本金余额 —— 对账脚本就靠这条不变式。
	//
	// gift_amount / gift_after 是赠金桶的镜像:本笔变动中属于赠金的部分与变动后
	// 赠金余额。退款时靠 gift_amount 算出「这单消费里有多少是赠金」—— 赠金不退,
	// 否则「充 100 送 10 → 用赠金消费 → 退款拿回本金」可以无限套现。
	// operator 只在后台操作时有值(操作人姓名);顾客自助的充值/消费/退回为空。
	`CREATE TABLE IF NOT EXISTS tb_wallet_txn (
		txn_id         {{PK}},
		customer_id    INTEGER      NOT NULL,
		biz_type       VARCHAR(16)  NOT NULL,
		amount         INTEGER      NOT NULL,
		balance_after  INTEGER      NOT NULL,
		gift_amount    INTEGER      DEFAULT 0,
		gift_after     INTEGER      DEFAULT 0,
		order_no       VARCHAR(32)  DEFAULT '',
		payment_no     VARCHAR(64)  DEFAULT '',
		reason         VARCHAR(255) DEFAULT '',
		operator       VARCHAR(64)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 《储值规则》同意留痕(合规:发卡须明示章程,见 docs/compliance/prepaid-card.md)。
	//
	// 一人一行,customer_id 主键 —— 只记「当前同意的是哪一版」,不记历史:
	// 争议时需要回答的是「他充值时看到的是哪一版规则」,而 rules_version 变更
	// 会强制重新确认,所以「当前版本」即「他最近一次充值时看到的版本」。
	// 会员注销时随档案一并删除(它是会员的行为记录,不是门店的经营数据)。
	`CREATE TABLE IF NOT EXISTS tb_wallet_agreement (
		customer_id    INTEGER PRIMARY KEY,
		rules_ver      VARCHAR(16)  NOT NULL,
		agreed_time {{TSTYPE}} DEFAULT NULL,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 储值充值单:顾客在小程序里发起的充值,走微信 JSAPI 支付,回调成功后入账。
	//
	// 单号固定以 R 开头(订单是 D 开头)—— 支付回调 handler 靠前缀分流:
	// R 开头走充值入账,D 开头走订单入账。status: 0 待支付 / 1 已入账 / 2 已关闭。
	// 回调用条件更新(status=0 → 1)做幂等,微信重发的通知不会重复加钱。
	//
	// gift_amount 是本笔充值附带的赠送金额(会员系统的「充 100 送 10」),
	// 由 service 在建单时按活动规则算出并冻结在单上:回调入账只认单上的金额,
	// 不重新算活动 —— 否则活动一改,已支付未入账的单会入成另一个数。
	`CREATE TABLE IF NOT EXISTS tb_recharge (
		recharge_id      {{PK}},
		recharge_no      VARCHAR(32)  NOT NULL,
		customer_id      INTEGER      NOT NULL,
		amount           INTEGER      NOT NULL,
		gift_amount      INTEGER      DEFAULT 0,
		status           INTEGER      DEFAULT 0,
		channel          VARCHAR(16)  DEFAULT '',
		channel_trade_no VARCHAR(64)  DEFAULT '',
		pay_time        VARCHAR(32) DEFAULT NULL,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 记住登录令牌(实现「记住我(7/30 天免登录)」)。
	// 令牌为后端生成的随机串,存本地后仅用于向后端换取新登录态;
	// 是否过期由 expire_time 决定(后端查库裁决,前端无法篡改),与后端访问令牌(默认 24h)解耦。
	// token 列存 SHA-256 哈希(库内不落明文),token_prefix 存原令牌前 8 位供设备管理页识别。
	// ip 为签发/最近一次免登录使用的客户端 IP(设备管理页展示,便于识别陌生设备);
	// 取 ClientIP()(受 TRUSTED_PROXIES 约束),空串表示未取到。
	// 改密/停用/删除账号时删掉对应行,该账号全部「记住我」会话立即失效。
	`CREATE TABLE IF NOT EXISTS tb_remember_token (
		token_id       {{PK}},
		user_id        INTEGER      NOT NULL,
		token          VARCHAR(64)  NOT NULL,
		token_prefix   VARCHAR(8)   DEFAULT '',
		expire_time VARCHAR(32) DEFAULT NULL,
		last_used_time VARCHAR(32) DEFAULT NULL,
		ua             VARCHAR(255) DEFAULT '',
		ip             VARCHAR(64)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 图片内容存储:菜品图/收款码/店铺 Logo 的二进制内容随数据库走(磁盘不再是
	// 运行时依赖),业务表(tb_dish/tb_config)仍存 /uploads/<img_name> 相对路径,
	// 由本表按名字解析。设计详见 docs/design-docs/store/image-db-storage/spec.md。
	// img_data 类型按方言生成({{BLOB}}):SQLite BLOB 无上限差异;MySQL 用 LONGBLOB
	// (BLOB 上限 64KB,装不下 64MB 的单张上传图)。
	`CREATE TABLE IF NOT EXISTS tb_image (
		img_id       {{PK}},
		img_name     VARCHAR(128) NOT NULL,
		content_type VARCHAR(32)  DEFAULT '',
		img_data     {{BLOB}},
		file_size    INTEGER      DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}},
		update_time {{TSTYPE}} DEFAULT NULL{{ONUPD}}
	){{OPTS}}`,

	// 访问明细(站点访问统计,见 docs/design-docs/visit-analytics/spec.md)。
	//
	// 定位与 tb_print_job 同构:「待办」性质 —— 日聚合台账(tb_visit_daily)落好后
	// 即可按保留天数清理,渠道 UV 的窗口查询只覆盖保留期内的明细。
	// 只插不改(无 update_time),与 tb_oper_log 一致。
	//
	// visitor_key 是 UV 去重键,三种前缀(见 spec 第 1 节):
	//   m:<customer_id>      登录会员(小程序带顾客令牌),最可靠;
	//   c:<visitorId>        前端生成并持久化的随机访客 ID(localStorage / wx.storage);
	//   a:<ipHash>:<uaHash>  前两者都缺失时的兜底(存储被禁用的浏览器)。
	// ip_hash / ua_hash 是 sha256(值 + pepper) 的前 32 位:库文件泄露也无法反推
	// 原始 IP 与 UA,pepper 与配置加密同密钥体系(见 InitSecretKeys)。
	//
	// entry 存入口标识:H5 是桌台点餐码(哪张桌的二维码被扫了),小程序一期留空。
	`CREATE TABLE IF NOT EXISTS tb_visit_log (
		visit_id    {{PK}},
		visit_date  VARCHAR(10)  NOT NULL,
		channel     VARCHAR(16)  NOT NULL,
		visitor_key VARCHAR(80)  NOT NULL,
		is_member   INTEGER      DEFAULT 0,
		customer_id INTEGER      DEFAULT 0,
		entry       VARCHAR(16)  DEFAULT '',
		ip_hash     VARCHAR(64)  DEFAULT '',
		ua_hash     VARCHAR(64)  DEFAULT '',
		-- 终端与来源信息(2026-10-18 增补):IP 明文用于排查与展示(明细 90 天清理,
		-- 与操作日志 oper_ip 明文同一取舍);城市来自 ip2region 离线库
		-- (data/ip2region.xdb,缺文件时为空);OS/浏览器/设备类型为归一后的展示值
		-- (iOS/微信/手机…),机型安卓 UA 可解出型号,iOS 的 UA 不含机型存空,
		-- 小程序端上直传最准。
		ip          VARCHAR(45)  DEFAULT '',
		city        VARCHAR(64)  DEFAULT '',
		os          VARCHAR(16)  DEFAULT '',
		browser     VARCHAR(16)  DEFAULT '',
		device      VARCHAR(8)   DEFAULT '',
		model       VARCHAR(64)  DEFAULT '',
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}}
	){{OPTS}}`,

	// 访客注册表(台账,长期保留):一行一个独立访客,主键即 visitor_key。
	// 存在的意义是让「新访客」精确 —— first_visit_date 等于统计日即为当日新增;
	// 没有它就只能靠明细窗口近似(访客上一次出现早于保留期时会被误判成新客)。
	// 量级 = 累计独立访客数(单店数千行),永久保留无压力。
	`CREATE TABLE IF NOT EXISTS tb_visitor (
		visitor_key      VARCHAR(80) PRIMARY KEY,
		first_visit_date VARCHAR(10) NOT NULL,
		last_visit_date  VARCHAR(10) NOT NULL,
		visit_count      INTEGER     DEFAULT 1,
		is_member        INTEGER     DEFAULT 0,
		customer_id      INTEGER     DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}}
	){{OPTS}}`,

	// 日聚合台账(长期保留):每日每个渠道一行(channel='all' 为跨渠道去重后的
	// 全站合计行,各单渠道行之和不等于它 —— 同一访客可能两个渠道都来过)。
	// rollup 采用「绝对重算」而非累加:同一 (visit_date, channel) 重复执行结果
	// 一致,崩溃后可安全重跑;写入路径是先 UPDATE 后 INSERT(同 NextPickupNoTx),
	// 不依赖 MySQL 的 ON DUPLICATE KEY / SQLite 的 ON CONFLICT。
	`CREATE TABLE IF NOT EXISTS tb_visit_daily (
		id         {{PK}},
		visit_date VARCHAR(10) NOT NULL,
		channel    VARCHAR(16) NOT NULL,
		pv         INTEGER     DEFAULT 0,
		uv         INTEGER     DEFAULT 0,
		new_uv     INTEGER     DEFAULT 0,
		member_uv  INTEGER     DEFAULT 0,
		create_time {{TSTYPE}} DEFAULT {{NOWDEF}}
	){{OPTS}}`,
}

// schemaStatements 返回指定方言下的全部建表语句。
// MySQL 分支额外注入表/列 COMMENT(注释定义见 schema_comment.go);
// SQLite 没有注释语法,原样返回,生成的脚本与历史版本保持一致。
func schemaStatements(d Dialect) []string {
	out := make([]string, 0, len(schemaTemplate))
	for _, tpl := range schemaTemplate {
		stmt := expandDialectPlaceholders(tpl, d)
		if d == DialectMySQL {
			stmt = applySQLComments(stmt)
		}
		out = append(out, stmt)
	}
	return out
}

// expandDialectPlaceholders 把模板里的方言占位符替换为具体 DDL 片段。
// 建表语句与增量补列(alterCols 的 Def 也可能带占位符)共用,避免两处展开逻辑漂移。
func expandDialectPlaceholders(s string, d Dialect) string {
	s = strings.ReplaceAll(s, "{{PK}}", autoIncPKFor(d))
	s = strings.ReplaceAll(s, "{{OPTS}}", tableOptionsFor(d))
	s = strings.ReplaceAll(s, "{{BLOB}}", blobTypeFor(d))
	s = strings.ReplaceAll(s, "{{TEXT}}", textTypeFor(d))
	s = strings.ReplaceAll(s, "{{TSTYPE}}", tsTypeFor(d))
	s = strings.ReplaceAll(s, "{{NOWDEF}}", tsNowDefaultFor(d))
	s = strings.ReplaceAll(s, "{{ONUPD}}", tsOnUpdateFor(d))
	return s
}

// ---- 时间列的方言分叉 ----
//
// 目标:时间由**数据库**负责生成 —— 插入自动填 create_time,行更新自动推进 update_time,
// 应用层(以及任何手工 SQL / 外部导入)都不必再关心这两个字段。
//
// 两库能力不同,所以在这里分叉:
//
//	MySQL  —— create_time DATETIME DEFAULT CURRENT_TIMESTAMP
//	          update_time DATETIME DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP
//	          原生支持,不需要触发器。
//	SQLite —— 没有真正的时间类型,也没有 ON UPDATE:列仍是 VARCHAR(32),
//	         插入默认值用 DEFAULT (datetime('now','localtime')),
//	         自动更新由 AFTER UPDATE 触发器完成(见 touchTriggerStatements)。
//
// 为什么不把 SQLite 也声明成 DATETIME:SQLite 的类型只是亲和性提示,声明 DATETIME 后
// 驱动会在**读取时**尝试把文本解析成 time.Time,遇到历史脏值(空串、'0000-00-00'、
// 外部导入的异常格式)会直接报错中断查询;保持 VARCHAR 则脏值原样读出,由 dao.TS()
// 统一折叠,更抗打。
//
// 时区:两端都取「本地时间」。SQLite 的 datetime('now','localtime') 用进程所在时区;
// MySQL 的 CURRENT_TIMESTAMP 取会话时区,部署时需保证 MySQL 的 time_zone 与应用
// 时区一致(见 docs/mysql-migration.md),否则会出现「应用写本地时间、DB 默认值写 UTC」
// 两套时间混在同一列里。
func tsTypeFor(d Dialect) string {
	if d == DialectMySQL {
		return "DATETIME"
	}
	return "VARCHAR(32)"
}

func tsNowDefaultFor(d Dialect) string {
	if d == DialectMySQL {
		return "CURRENT_TIMESTAMP"
	}
	return "(datetime('now','localtime'))"
}

func tsOnUpdateFor(d Dialect) string {
	if d == DialectMySQL {
		return " ON UPDATE CURRENT_TIMESTAMP"
	}
	return ""
}

// updateTimeTables 含 update_time 列的表(由 DDL 定义自动推导,不手工维护,避免漂移)。
var updateTimeTables = func() []string {
	out := []string{}
	for _, tpl := range schemaTemplate {
		name := tableNameOf(tpl)
		if name == "" {
			continue
		}
		if strings.Contains(tpl, "update_time") {
			out = append(out, name)
		}
	}
	return out
}()

// tableNameOf 从建表语句里取出表名。
func tableNameOf(stmt string) string {
	const prefix = "CREATE TABLE IF NOT EXISTS "
	i := strings.Index(stmt, prefix)
	if i < 0 {
		return ""
	}
	rest := stmt[i+len(prefix):]
	if j := strings.IndexAny(rest, " \n("); j > 0 {
		return rest[:j]
	}
	return ""
}

// touchTriggerStatements 返回「行更新时自动推进 update_time」的触发器语句。
//
// 只有 SQLite 需要:MySQL 用列上的 ON UPDATE CURRENT_TIMESTAMP 实现同一效果。
// 触发器体里的 UPDATE 不会递归触发自己 —— SQLite 默认关闭 recursive_triggers,
// 且这里只改 update_time 一列,不涉及业务字段。
//
// 条件 NEW.update_time = OLD.update_time 的含义:应用显式写了新的 update_time(或
// 业务上确实改了 update_time)时不覆盖调用方的值;只有「其他列变了但没带 update_time」
// 才由数据库补当前时间 —— 与 MySQL 的 ON UPDATE 语义保持一致。
func touchTriggerStatements(d Dialect) []string {
	if d == DialectMySQL {
		return nil
	}
	out := make([]string, 0, len(updateTimeTables))
	for _, t := range updateTimeTables {
		out = append(out, fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS trg_%s_touch
		AFTER UPDATE ON %s FOR EACH ROW
		WHEN NEW.update_time IS OLD.update_time
		BEGIN
			UPDATE %s SET update_time = datetime('now','localtime') WHERE rowid = NEW.rowid;
		END`, t, t, t))
	}
	return out
}

// indexDef 索引定义(方言无关),由 indexStatements 生成各方言的具体语句。
type indexDef struct {
	Name    string
	Table   string
	Columns string
	Unique  bool
	// Where 部分索引条件:仅 SQLite 支持。
	// MySQL 没有部分索引,遇到 Where 时会自动降级为普通索引,
	// 唯一性改由应用层保证(见 EnsureTableCode)。
	Where string
}

// indexDefs 全部索引(共 50 个)。
// 唯一索引保证 order_no / refund_no / 渠道交易号不重复;
// 其余索引用于订单查询、看板与报表加速。
var indexDefs = []indexDef{
	{Name: "idx_order_no", Table: "tb_order", Columns: "order_no", Unique: true},
	// 取餐号当天唯一:正常路径由 dao.NextPickupNoTx 的行锁串行化保证,
	// 唯一索引作为「有人绕过该函数手工插起订」时的兜底防线。
	// biz_date 为 NULL 的行不参与判定(两库语义一致),因此不影响手工补录的历史行。
	{Name: "idx_order_pickup", Table: "tb_order", Columns: "biz_date, pickup_no", Unique: true},
	{Name: "idx_order_table", Table: "tb_order", Columns: "table_id"},
	{Name: "idx_order_status", Table: "tb_order", Columns: "order_status"},
	// 「未结账单」判定与看板按桌分组查询:table_id + pay_status + order_status 组合索引。
	{Name: "idx_order_table_unpaid", Table: "tb_order", Columns: "table_id, pay_status, order_status"},
	{Name: "idx_order_create_time", Table: "tb_order", Columns: "create_time"},
	// 报表按「钱到账时间」过滤(见 dao.RangeAmount):正常收款按 pay_time、
	// 挂账核销按 credit_settle_time 归属,补索引避免区间统计逐日扫全表。
	{Name: "idx_order_pay_time", Table: "tb_order", Columns: "pay_time"},
	{Name: "idx_order_credit_settle_time", Table: "tb_order", Columns: "credit_settle_time"},
	// 「已完成订单数」(dao.CountFinishedIn)与「用餐时长」(dao.DiningDurationRows)
	// 都按 finish_time 的区间归属,此前缺索引只能全表扫订单主表。
	// order_status 是等值条件、必须排在范围列之前:BTree 才能先收敛到 status=4
	// 再沿 finish_time 做范围扫描;反过来写则范围列之后的部分用不上。
	{Name: "idx_order_finish_time", Table: "tb_order", Columns: "order_status, finish_time"},
	// 会员「我的订单」按时间倒序分页(见 tb_customer)。
	{Name: "idx_order_customer", Table: "tb_order", Columns: "customer_id, create_time"},
	// 会员登录查档主路径(provider + external_id),同时兜底并发首登的幂等性。
	// (历史上还有 idx_customer_openid,P2 删列时一并移除 —— 见 migrateCustomerOpenid)
	{Name: "idx_identity_provider_external", Table: "tb_customer_identity", Columns: "provider, external_id", Unique: true},
	// 「这个人绑了哪几种登录方式」:后续解绑 / 多端合并要用。
	{Name: "idx_identity_customer", Table: "tb_customer_identity", Columns: "customer_id"},
	// 充值单按单号查(支付回调 / 前端轮询入账状态);单号唯一同时兜底并发建单。
	{Name: "idx_recharge_no", Table: "tb_recharge", Columns: "recharge_no", Unique: true},
	// 会员的储值流水(「我的」页与后台流水按时间倒序翻页)。
	{Name: "idx_wallet_txn_customer", Table: "tb_wallet_txn", Columns: "customer_id, txn_id"},
	// 同一人的多个 openid 共享 unionid,按它反查可把多端归到一个人;非唯一(必然重复)。
	{Name: "idx_identity_unionid", Table: "tb_customer_identity", Columns: "unionid"},
	{Name: "idx_order_item_order", Table: "tb_order_item", Columns: "order_id"},
	{Name: "idx_payment_order", Table: "tb_payment", Columns: "order_no"},
	{Name: "idx_payment_channel_trade", Table: "tb_payment", Columns: "channel, channel_trade_no", Unique: true},
	{Name: "idx_refund_no", Table: "tb_refund", Columns: "refund_no", Unique: true},
	{Name: "idx_refund_order", Table: "tb_refund", Columns: "order_id"},
	// 退款按「退款到账时间(update_time)」归属并扣减(见 dao.RangeAmount),补索引加速区间聚合。
	{Name: "idx_refund_update_time", Table: "tb_refund", Columns: "update_time"},
	{Name: "idx_urge_status", Table: "tb_order_urge", Columns: "status, create_time"},
	{Name: "idx_urge_order", Table: "tb_order_urge", Columns: "order_id"},
	// 订单评价:(order_no, reviewer_key) 唯一 = 一人一评的数据库兜底 —— 同一桌的
	// 多位顾客各自评价(会员按身份派生键、匿名按设备持久化随机键),同一人重复
	// 提交(双击/刷新重试)由这里兜住。时间索引用于列表翻页,评分复合索引用于按档位筛选。
	{Name: "idx_review_order_reviewer", Table: "tb_review", Columns: "order_no, reviewer_key", Unique: true},
	{Name: "idx_review_time", Table: "tb_review", Columns: "create_time"},
	{Name: "idx_review_rating", Table: "tb_review", Columns: "rating, create_time"},
	// 评价图片:文件名唯一(展示/绑定凭据);按评价取图(管理端详情/级联删除);按时间扫孤儿。
	{Name: "idx_review_image_name", Table: "tb_review_image", Columns: "img_name", Unique: true},
	{Name: "idx_review_image_review", Table: "tb_review_image", Columns: "review_id"},
	{Name: "idx_review_image_time", Table: "tb_review_image", Columns: "create_time"},
	// 打印日志:按订单倒查(「这单到底打了没」)、按时间翻页、按失败筛选。
	{Name: "idx_print_log_order", Table: "tb_print_log", Columns: "order_id"},
	{Name: "idx_print_log_time", Table: "tb_print_log", Columns: "create_time"},
	{Name: "idx_print_log_status", Table: "tb_print_log", Columns: "status, create_time"},
	// 代理任务队列:取单按「状态 + 可执行时间」筛(代理每 3 秒轮询一次,必须有索引),
	// 打印日志页按「打印机 + 状态」看某台机器的积压。
	{Name: "idx_print_job_pick", Table: "tb_print_job", Columns: "status, next_try_time, job_id"},
	{Name: "idx_print_job_printer", Table: "tb_print_job", Columns: "printer_id, status"},
	// per-agent 令牌鉴权按 hash 精确查找,并只接受启用中的身份。
	{Name: "idx_print_agent_token", Table: "tb_print_agent", Columns: "token_hash, status"},
	{Name: "idx_table_code", Table: "tb_table", Columns: "table_code", Unique: true, Where: "table_code!=''"},
	// 用户名/角色标识的唯一性只约束「未删除」的行 —— 删掉员工后可以重新使用同名账号。
	// 与 idx_table_code 同理:MySQL 不支持部分索引会降级为普通索引,
	// 唯一性由应用层 EnsureUsernameAvailable / EnsureRoleKeyAvailable 保证。
	{Name: "idx_user_username", Table: "tb_user", Columns: "username", Unique: true, Where: "del_flag='0'"},
	{Name: "idx_user_role", Table: "tb_user", Columns: "role_id"},
	// 角色标识全局唯一(含已删除行):SQLite 旧定义为带 WHERE 的部分唯一索引,
	// MySQL 不支持部分索引会降级为普通索引,导致启动引导 INSERT IGNORE 形同虚设、
	// 每次启动重复插入内置角色。改为两库统一的全局唯一后,由数据库层面兜底防并发;
	// 被软删除的角色行仍占用标识,重建同名角色走「复活原行」语义(见 dao.InsertRole)。
	{Name: "idx_role_key", Table: "tb_role", Columns: "role_key", Unique: true},
	// 操作日志:按时间翻页(主查询)、按人追责、按对象倒查「谁动过这一单」、只看失败。
	{Name: "idx_operlog_time", Table: "tb_oper_log", Columns: "create_time"},
	{Name: "idx_operlog_operator", Table: "tb_oper_log", Columns: "operator_id, create_time"},
	{Name: "idx_operlog_target", Table: "tb_oper_log", Columns: "target_type, target_id"},
	{Name: "idx_operlog_status", Table: "tb_oper_log", Columns: "status, create_time"},
	{Name: "idx_remember_token", Table: "tb_remember_token", Columns: "token", Unique: true},
	// 名字唯一 = 同名内容幂等写入的前提。
	{Name: "idx_image_name", Table: "tb_image", Columns: "img_name", Unique: true},
	// 访问统计:按天扫明细(rollup 与清理)、按天+访客键去重(UV)、
	// 按天+渠道+访客键(渠道 UV 窗口查询),见 docs/design-docs/visit-analytics/spec.md。
	{Name: "idx_visitlog_date", Table: "tb_visit_log", Columns: "visit_date"},
	{Name: "idx_visitlog_visitor", Table: "tb_visit_log", Columns: "visit_date, visitor_key"},
	{Name: "idx_visitlog_channel", Table: "tb_visit_log", Columns: "visit_date, channel, visitor_key"},
	// 访客注册表:新访客按首访日期圈选。
	{Name: "idx_visitor_first", Table: "tb_visitor", Columns: "first_visit_date"},
	// 日聚合台账:rollup「先 UPDATE 后 INSERT」的定位键,同时兜底并发重复行。
	{Name: "idx_visit_daily_uk", Table: "tb_visit_daily", Columns: "visit_date, channel", Unique: true},
}

// indexStatements 返回指定方言下的全部建索引语句。
func indexStatements(d Dialect) []string {
	out := make([]string, 0, len(indexDefs))
	for _, ix := range indexDefs {
		out = append(out, indexStatement(d, ix))
	}
	return out
}

// indexStatement 生成单条 CREATE INDEX 语句。
func indexStatement(d Dialect, ix indexDef) string {
	unique, where := ix.Unique, ix.Where
	if d == DialectMySQL && where != "" {
		// MySQL 不支持带 WHERE 的部分索引:退化为普通索引。
		unique, where = false, ""
	}
	var b strings.Builder
	b.WriteString("CREATE ")
	if unique {
		b.WriteString("UNIQUE ")
	}
	fmt.Fprintf(&b, "INDEX %s ON %s(%s)", ix.Name, ix.Table, ix.Columns)
	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}
	return b.String()
}

// alterCol 老库补齐列的定义。Def 必须使用两库通用的类型写法。
type alterCol struct {
	Table string
	Name  string
	Def   string
}

// alterCols 历史上通过 ALTER TABLE 追加的列(新库已在建表语句中包含,此处用于老库升级)。
var alterCols = []alterCol{
	// 在线支付。
	{Table: "tb_order", Name: "transaction_id", Def: "VARCHAR(64) DEFAULT ''"},
	{Table: "tb_order", Name: "pay_channel", Def: "VARCHAR(16) DEFAULT ''"},
	{Table: "tb_order", Name: "refund_amount", Def: "INTEGER DEFAULT 0"},
	{Table: "tb_order", Name: "refund_time", Def: "VARCHAR(32)"},
	// 结算方式: normal 正常收款 / free 免单 / credit 挂账。
	{Table: "tb_order", Name: "settle_type", Def: "VARCHAR(16) DEFAULT 'normal'"},
	{Table: "tb_order", Name: "settle_time", Def: "VARCHAR(32)"},
	{Table: "tb_order", Name: "settle_operator", Def: "VARCHAR(64) DEFAULT ''"},
	{Table: "tb_order", Name: "settle_remark", Def: "VARCHAR(255) DEFAULT ''"},
	// 挂账(记账): credit_status 0 非挂账 / 1 待收款 / 2 已结清。
	{Table: "tb_order", Name: "credit_status", Def: "INTEGER DEFAULT 0"},
	{Table: "tb_order", Name: "credit_amount", Def: "INTEGER DEFAULT 0"},
	{Table: "tb_order", Name: "credit_settle_time", Def: "VARCHAR(32)"},
	// 记住登录:环境指纹(设备管理页展示 + 免登录环境绑定)。
	{Table: "tb_remember_token", Name: "ua", Def: "VARCHAR(255) DEFAULT ''"},
	// 记住登录令牌改为哈希存储后,新增 token_prefix 保存原令牌前 8 位(设备管理页识别用)。
	{Table: "tb_remember_token", Name: "token_prefix", Def: "VARCHAR(8) DEFAULT ''"},
	{Table: "tb_order", Name: "credit_settle_by", Def: "VARCHAR(64) DEFAULT ''"},
	// 实收金额(分): 免单=0;挂账核销前=0,核销后为应收金额;退款时相应扣减。
	{Table: "tb_order", Name: "paid_amount", Def: "INTEGER DEFAULT 0"},
	// 撤销结算回退用:结账时记录结账前状态,撤销时恢复(0=历史数据,回退为 3)。
	{Table: "tb_order", Name: "pre_settle_status", Def: "INTEGER DEFAULT 0"},
	// 重复支付自动原路退回的退款单标记:不占订单退款额度、不扣订单实收。
	{Table: "tb_refund", Name: "is_duplicate", Def: "INTEGER DEFAULT 0"},
	// 桌台稳定码: 创建时生成、永不变更的随机码,二维码内容使用它(而非自增 ID)。
	{Table: "tb_table", Name: "table_code", Def: "VARCHAR(16) DEFAULT ''"},
	// 桌台人工状态(与自动占用 status 正交): 0 正常 / 1 停用(不可用) / 2 预约。
	// 老库默认 0(正常),不影响存量桌台;lock_remark 记录停用原因或预约人/时段。
	{Table: "tb_table", Name: "lock_status", Def: "INTEGER DEFAULT 0"},
	{Table: "tb_table", Name: "lock_remark", Def: "VARCHAR(255) DEFAULT ''"},
	// 打印机厂商与云打印: provider 默认 'tcp' —— 老库里的打印机都是直连网络机,
	// 升级后行为不变;切到飞鹅只需把 provider 改成 'feie' 并填 feie_sn。
	{Table: "tb_printer", Name: "provider", Def: "VARCHAR(16) DEFAULT 'tcp'"},
	{Table: "tb_printer", Name: "feie_sn", Def: "VARCHAR(64) DEFAULT ''"},
	// 打印份数(1-5) 与「按菜品分类分单」(CSV 分类 ID,空=全收)。
	{Table: "tb_printer", Name: "copies", Def: "INTEGER DEFAULT 1"},
	{Table: "tb_printer", Name: "category_ids", Def: "VARCHAR(255) DEFAULT ''"},
	// 加菜批次:首次下单=1,每次加菜 +1;老库明细默认回填为 1。
	{Table: "tb_order_item", Name: "batch_no", Def: "INTEGER DEFAULT 1"},
	// 幂等键:下单/加菜去重(冗余存储于明细行,老库默认 '' 不参与去重)。
	{Table: "tb_order_item", Name: "client_request_id", Def: "VARCHAR(64) DEFAULT ''"},
	// 赠送标记:0 正常计费 1 赠送;老库明细默认 0。
	{Table: "tb_order_item", Name: "is_gift", Def: "INTEGER DEFAULT 0"},
	// 赠送原因(可选),老库默认 ''。
	{Table: "tb_order_item", Name: "gift_reason", Def: "VARCHAR(255) DEFAULT ''"},
	// 订单赠送金额合计(分),老库默认 0。
	{Table: "tb_order", Name: "gift_amount", Def: "INTEGER DEFAULT 0"},
	// 订单会员归属(顾客会员体系),老库默认 0(匿名单);历史数据不回填。
	{Table: "tb_order", Name: "customer_id", Def: "INTEGER DEFAULT 0"},
	// 营业日与当日取餐号(取餐叫号用):老库升级补列,与建表语句中的 biz_date/pickup_no 对齐。
	{Table: "tb_order", Name: "biz_date", Def: "VARCHAR(10) DEFAULT NULL"},
	{Table: "tb_order", Name: "pickup_no", Def: "INTEGER DEFAULT 0"},
	// 售罄标记（人工）：1 售罄 0 正常，与「下架(status)」正交。老库默认 0。
	{Table: "tb_dish", Name: "sold_out", Def: "INTEGER DEFAULT 0"},
	// 招牌菜标记：1 招牌 0 普通，顾客端图片左上角渲染「招牌」角标。老库默认 0。
	{Table: "tb_dish", Name: "signature", Def: "INTEGER DEFAULT 0"},
	// 规格库存（自动售罄）：-1 不限量，>=0 剩余份数。老库默认 -1。
	{Table: "tb_spec", Name: "stock", Def: "INTEGER DEFAULT -1"},
	// 用餐起止时间（落库）：老库历史行保持 NULL，由 incr 回填脚本按 create_time /
	// finish_time 补齐；读侧一律 COALESCE 回落到这两列，保证升级前后统计口径一致。
	{Table: "tb_order", Name: "meal_start_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_order", Name: "meal_end_time", Def: "VARCHAR(32) DEFAULT NULL"},
	// 商家首次操作时间:老库补列后为 NULL,历史订单不会被批量打上「新」角标
	// (角标条件还叠加了「已下单」,已完成/已取消的历史单本来就不会命中)。
	{Table: "tb_order", Name: "viewed_at", Def: "VARCHAR(32) DEFAULT NULL"},
	// 访问明细补终端与来源列(2026-10-18):上线时的 tb_visit_log 没有这些列。
	{Table: "tb_visit_log", Name: "ip", Def: "VARCHAR(45) DEFAULT ''"},
	{Table: "tb_visit_log", Name: "city", Def: "VARCHAR(64) DEFAULT ''"},
	{Table: "tb_visit_log", Name: "os", Def: "VARCHAR(16) DEFAULT ''"},
	{Table: "tb_visit_log", Name: "browser", Def: "VARCHAR(16) DEFAULT ''"},
	{Table: "tb_visit_log", Name: "device", Def: "VARCHAR(8) DEFAULT ''"},
	{Table: "tb_visit_log", Name: "model", Def: "VARCHAR(64) DEFAULT ''"},
	// ---- 通用记录时间补齐 ----
	// 早期只给「主数据表」(桌台/菜品/订单/员工/角色…)留了 create_time/update_time,
	// 其余表按需加了单列甚至没有,排查问题时无从判断「这行什么时候进来的、最后动过没」。
	// 这里统一补齐,由写入侧自动填写(见各 DAO),历史行为 NULL,不做回填 ——
	// 老行的真实时间已无从得知,硬凑反而比留空误导性更强。
	//
	// 填写口径(全局约定,新表与存量表一致):
	//   create_time —— 只在 INSERT 时写一次,此后任何 UPDATE 都不得改动;
	//   update_time —— 只在 UPDATE 时推进,INSERT 时留 NULL(「从未被改过」与
	//                  「刚创建」是两个可区分的状态,插入时填成同一时刻就把这份信息抹掉了)。
	// 例外:tb_config 走 REPLACE 整行替换,只能推进 update_time 而写不了 create_time
	//       (写了会把首次落库时间冲掉),首次时间由种子语句负责。
	{Table: "tb_spec", Name: "create_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_spec", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_print_log", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	// 票面快照:content 存这次实际送出的每一张纸的完整文本行(JSON),
	// paper_width 存当时的纸宽。老记录留空,读侧回落到按当前数据重算。
	{Table: "tb_print_log", Name: "content", Def: "{{TEXT}}"},
	{Table: "tb_print_log", Name: "paper_width", Def: "INTEGER DEFAULT 48"},
	// 票据标识(【厨房单】1/4)。老库升级补列后历史行为空:那时它拼在 detail 里,
	// 不做解析回填(格式靠字符串拼出来,硬拆比留空更容易错)。
	{Table: "tb_print_log", Name: "label", Def: "VARCHAR(64) DEFAULT ''"},
	{Table: "tb_print_job", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_oper_log", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_config", Name: "create_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_config", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_day_seq", Name: "create_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_day_seq", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_order_item", Name: "create_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_order_item", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_order_urge", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	// 评价者键:一单一评×一人一次的唯一索引(idx_review_order_reviewer)依赖此列,
	// 必须在补列清单里 —— 老库先补列、后建索引,否则启动建索引因列不存在而失败。
	{Table: "tb_review", Name: "reviewer_key", Def: "VARCHAR(64) DEFAULT ''"},
	{Table: "tb_customer", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	{Table: "tb_remember_token", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	// 登录设备页展示的来源 IP(签发时写入,每次免登录使用刷新为最近 IP)。
	{Table: "tb_remember_token", Name: "ip", Def: "VARCHAR(64) DEFAULT ''"},
	{Table: "tb_image", Name: "update_time", Def: "VARCHAR(32) DEFAULT NULL"},
	// ---- 储值:本金/赠金分桶与合规留痕(2026-10-18) ----
	// 老库补列后,存量余额**全部落在本金桶**、历史流水的 gift_amount 为 0:
	// 那时还没有赠送功能,不存在「需要拆开却拆不开」的数据。赠金桶从 0 起算,
	// 与「历史消费全额可退」的既有语义一致(退款可退本金上限 = 该单消费总额)。
	{Table: "tb_wallet", Name: "balance_gift", Def: "INTEGER DEFAULT 0"},
	{Table: "tb_wallet_txn", Name: "gift_amount", Def: "INTEGER DEFAULT 0"},
	{Table: "tb_wallet_txn", Name: "gift_after", Def: "INTEGER DEFAULT 0"},
	// 充值单附带赠送额:老单默认 0(建单时还没有赠送活动)。
	{Table: "tb_recharge", Name: "gift_amount", Def: "INTEGER DEFAULT 0"},
}

// alterStatements 返回指定方言下的增量列 DDL。
func alterStatements(d Dialect) []string {
	out := make([]string, 0, len(alterCols))
	for _, c := range alterCols {
		out = append(out, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
			c.Table, c.Name, expandDialectPlaceholders(c.Def, d)))
	}
	return out
}

// ============================================================================
// 迁移执行
// ============================================================================

// migrate 建表(幂等)、补索引、补列并回填历史数据。
//
// stmts 由调用方(store.Init)预先算好并传进来:建表语句条数要先写进启动日志,
// 让「迁移跑了几十秒」这件事在日志里能对应上具体条数(见 Init 的启动打点)。
//
// 幂等策略:
//   - 建表:CREATE TABLE IF NOT EXISTS(两库均支持);
//   - 建索引:先查元数据判断索引是否已存在再创建
//     (SQLite 的 IF NOT EXISTS 是专有语法,MySQL 无对应写法,统一走存在性判断);
//   - 补列:先查 information_schema / PRAGMA 判断列是否已存在再 ALTER
//     (ADD COLUMN 两库都不支持 IF NOT EXISTS)。
func migrate(stmts []string) {
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			logger.Fatalf("建表失败(%s): %v\n%s", dialect, err, s)
		}
	}

	// 先补列再建索引:老库升级时,索引(如 idx_order_pickup)可能依赖本次新增的列
	// (biz_date/pickup_no),若先建索引会因列不存在而失败。
	for _, c := range alterCols {
		if columnExists(c.Table, c.Name) {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
			c.Table, c.Name, expandDialectPlaceholders(c.Def, dialect))
		if _, err := DB.Exec(stmt); err != nil {
			logger.Fatalf("补列失败(%s): %v\n%s", dialect, err, stmt)
		}
		logger.Infof("[db] 已补列 %s.%s", c.Table, c.Name)
		// viewed_at 只在「存量库补列」这一刻做一次性回填:此刻库里的订单都早于本功能上线,
		// 商家早就处理过了,统一按已查看落库,避免升级后列表整屏挂「新」角标。
		// 放在补列分支内(而非 migrate 末尾),保证只会执行一次 —— 放到末尾会在每次启动
		// 把当天真正的未读新单也一并标成已读。
		if c.Table == "tb_order" && c.Name == "viewed_at" {
			if _, err := DB.Exec(`UPDATE tb_order SET viewed_at = COALESCE(update_time, create_time) WHERE viewed_at IS NULL`); err != nil {
				logger.Warnf("[db] 回填订单已查看时间失败: %v", err)
			}
			// 空串 / MySQL 历史零值('0000-00-00 …')都会被读侧(dao.TS)折叠成 NULL,
			// 这类脏行同样会挂着「新」角标,统一用哨兵时间兜底。
			if _, err := DB.Exec(`UPDATE tb_order SET viewed_at='1970-01-01 00:00:00'
				WHERE viewed_at IS NULL OR viewed_at='' OR viewed_at LIKE '0000-00-00%'`); err != nil {
				logger.Warnf("[db] 兜底回填订单已查看时间失败: %v", err)
			}
		}
	}

	// viewed_at 脏值归一(幂等,每次启动执行):空串 / MySQL 历史零值在读侧都会被
	// dao.TS 折叠成「未操作」,但 SQL 筛选(newOnly)按 IS NULL 判定 —— 不归一会出现
	// 「角标显示新单、筛选取不到」的分裂。仅改写异常行,正常行零写入。
	if _, err := DB.Exec(`UPDATE tb_order SET viewed_at=NULL WHERE viewed_at='' OR viewed_at LIKE '0000-00-00%'`); err != nil {
		logger.Warnf("[db] 归一 viewed_at 脏值失败: %v", err)
	}

	// 建唯一索引前先清理 tb_role 的历史重复行:老版本 MySQL 下 idx_role_key 退化为
	// 普通索引 + 启动引导依赖唯一约束,导致每次启动重复插入内置角色。若带着重复
	// 数据直接建唯一索引会失败,老库升级会卡死在启动阶段。
	dedupeRoleKeys()

	for _, ix := range indexDefs {
		if indexExists(ix.Name) {
			continue
		}
		stmt := indexStatement(dialect, ix)
		if _, err := DB.Exec(stmt); err != nil {
			logger.Fatalf("建索引失败(%s): %v\n%s", dialect, err, stmt)
		}
	}
	// idx_role_key 的定义从「部分唯一/普通索引」升级为全局唯一:同名索引已存在的
	// 老库不会被上面的循环重建,这里检测旧定义并原地重建。
	recreateRoleKeyIndex()

	// P2:让 tb_customer.openid 彻底退役(登录方式已由 tb_customer_identity 承载)。
	// 放在建索引之后:identity 表与其唯一索引已就绪,搬迁才有地方落、也才能去重。
	migrateCustomerOpenid()

	// 历史数据回填:老订单没有实收金额,按「已支付即全额实收」补齐,保证报表口径连续。
	// 失败只告警不中断启动(回填是幂等的,下次启动会重试),但不能静默 —
	// 否则报表口径悄悄不一致,排查时无从下手。
	if _, err := DB.Exec(`UPDATE tb_order SET paid_amount = total_amount - COALESCE(refund_amount,0)
		WHERE pay_status=1 AND (paid_amount IS NULL OR paid_amount=0) AND (settle_type IS NULL OR settle_type='normal')`); err != nil {
		logger.Warnf("[db] 回填历史订单实收金额失败(下次启动重试): %v", err)
	}
	if _, err := DB.Exec(`UPDATE tb_order SET settle_type='normal' WHERE settle_type IS NULL OR settle_type=''`); err != nil {
		logger.Warnf("[db] 回填历史订单结算方式失败(下次启动重试): %v", err)
	}
	backfillTableCodes()
	backfillMealTimes()
	migrateUploadPrefix()
	migrateShopNameDot()
	normalizeFeieSNs()

	// 最后建「自动推进 update_time」的触发器:它引用 update_time 列,必须等上面的
	// 补列跑完(老库的 tb_image/tb_print_job 等列是本次才补上的)。
	// CREATE TRIGGER IF NOT EXISTS 幂等,已存在的老库直接跳过。
	for _, trg := range touchTriggerStatements(dialect) {
		if _, err := DB.Exec(trg); err != nil {
			// 不 Fatalf:触发器只影响 update_time 是否自动刷新,不该让服务起不来;
			// 应用层仍会显式写 update_time,降级后行为与改造前一致。
			logger.Warnf("[db] 建 update_time 触发器失败: %v", err)
			continue
		}
	}
	logger.Infof("[db] update_time 自动维护就绪(方言 %s)", dialect)
}

// migrateCustomerOpenid 让 tb_customer.openid 彻底退役(P2)。
//
// 背景:登录方式已迁到 tb_customer_identity(provider + external_id),openid 只是其中
// 一种。老库里这列还带着 NOT NULL + 唯一索引 —— 唯一索引只允许**一行**空串,
// 所以第二个登录方式(手机号等)永远建不成档。必须删列,而不是留着不用。
//
// 幂等:列不存在直接返回(新库 / 已升级过的库),重复启动零副作用。
// 安全:先搬迁、再删索引、最后删列;搬迁或删索引失败就中止启动,绝不在数据未搬完时删列。
//
// 方言差异:SQLite 3.35+ 与 MySQL 5.7+ 都支持 DROP COLUMN(项目内置 SQLite 3.46),
// 但 SQLite 不允许删除**仍被索引引用**的列,必须先删唯一索引。
func migrateCustomerOpenid() {
	if !columnExists("tb_customer", "openid") {
		return
	}

	// 1) 搬迁:兼容「跳过 P0 直接从老版本升上来」的库 —— 它们的 openid 还没进 identity 表。
	//    NOT EXISTS 守卫与索引是否存在无关,重复执行零变更。
	if _, err := DB.Exec(`INSERT INTO tb_customer_identity(customer_id, provider, external_id, last_login_time, create_time)
		SELECT c.customer_id, ?, c.openid, c.last_login_time, c.create_time
		FROM tb_customer c
		WHERE COALESCE(c.openid,'') <> ''
		  AND NOT EXISTS (SELECT 1 FROM tb_customer_identity i
		                  WHERE i.provider=? AND i.external_id=c.openid)`,
		po.CustomerProviderWxMP, po.CustomerProviderWxMP); err != nil {
		logger.Fatalf("[db] 搬迁 tb_customer.openid → tb_customer_identity 失败,已中止启动(不会删列): %v", err)
	}

	// 2) 删唯一索引。MySQL 的 DROP INDEX 需要 ON <table>;SQLite 不需要。
	if indexExists("idx_customer_openid") {
		stmt := `DROP INDEX IF EXISTS idx_customer_openid`
		if dialect == DialectMySQL {
			stmt = `DROP INDEX idx_customer_openid ON tb_customer`
		}
		if _, err := DB.Exec(stmt); err != nil {
			logger.Fatalf("[db] 删除索引 idx_customer_openid 失败,已中止启动(不会删列): %v", err)
		}
	}

	// 3) 删列。删不掉就起不来 —— 否则建档会因 NOT NULL 缺值而失败,表现是「注册不了会员」,
	//    比启动失败更难排查。
	if _, err := DB.Exec(`ALTER TABLE tb_customer DROP COLUMN openid`); err != nil {
		logger.Fatalf("[db] 删除 tb_customer.openid 失败,已中止启动: %v", err)
	}
	logger.Infof("[db] 已退役 tb_customer.openid(登录方式统一由 tb_customer_identity 承载)")
}

// backfillMealTimes 把历史订单的用餐起止时间补齐到新列(幂等,只写 NULL 行)。
//
// 老列的语义正好对应:
//   - meal_start_time ← create_time:下单即开台;
//   - meal_end_time ← finish_time(已完成,哪怕为空也照原样写,读侧会回落)。
//
// 新列「有值优先、NULL 回落 create_time/finish_time」的读法让统计口径天然连续,
// 但把老数据真正落下来,查询才不用常年带着 COALESCE,导出/对账也更直白。
func backfillMealTimes() {
	res, err := DB.Exec(`UPDATE tb_order SET meal_start_time = create_time
		WHERE meal_start_time IS NULL AND COALESCE(create_time,'') <> ''`)
	if err != nil {
		logger.Warnf("[db] 回填用餐开始时间失败: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logger.Infof("[db] 已回填 %d 条订单的用餐开始时间", n)
	}
	// 结束时间只在订单确已结束时才有意义,故限定已完成(4)/ 已取消(5)且 finish_time 有值。
	if _, err := DB.Exec(`UPDATE tb_order SET meal_end_time = finish_time
		WHERE meal_end_time IS NULL AND order_status IN (4,5) AND COALESCE(finish_time,'') <> ''`); err != nil {
		logger.Warnf("[db] 回填用餐结束时间失败: %v", err)
	}
}

// normalizeFeieSNs 规范存量打印机的飞鹅 SN(幂等,只在有变更时写库)。
//
// 机身标签印的是「SN 961828266」,商户照抄进输入框就带着前缀入库。飞鹅接口按 SN 精确
// 匹配,带着前缀推过去等于推给一台不存在的机器,而飞鹅很可能仍返回 ret=0 ——
// 表现就是「日志成功、纸却没出来」,事后极难排查。保存路径已做规范化(po.NormalizeFeieSN),
// 这里把老库里的存量脏值一次性收敛;剥离前缀后仍不合法的(如中间带空格)只告警、不擅自
// 改写,留给商户到「打印机管理」按机身标签核对后重存。
func normalizeFeieSNs() {
	rows, err := DB.Query(`SELECT printer_id, COALESCE(printer_name,''), COALESCE(feie_sn,'')
		FROM tb_printer WHERE provider=? AND COALESCE(feie_sn,'')<>''`, po.PrinterProviderFeie)
	if err != nil {
		logger.Warnf("[db] 读取飞鹅打印机失败: %v", err)
		return
	}
	type snRow struct {
		id   int
		name string
		sn   string
	}
	fixes := []snRow{}
	invalid := []snRow{}
	for rows.Next() {
		var r snRow
		if err := rows.Scan(&r.id, &r.name, &r.sn); err != nil {
			continue
		}
		norm := po.NormalizeFeieSN(r.sn)
		if norm == r.sn {
			continue // 已是规范值
		}
		r.sn = norm
		if po.IsFeieSN(norm) {
			fixes = append(fixes, r)
		} else {
			invalid = append(invalid, r)
		}
	}
	// 必须查 Err():扫描中途出错时 Next 会提前返回 false,不查会静默少修复几台。
	err = rows.Err()
	rows.Close()
	if err != nil {
		logger.Warnf("[db] 遍历飞鹅打印机失败: %v", err)
	}

	fixed := 0
	for _, r := range fixes {
		if _, err := DB.Exec(`UPDATE tb_printer SET feie_sn=? WHERE printer_id=?`, r.sn, r.id); err != nil {
			logger.Warnf("[db] 规范化打印机[%s]的飞鹅 SN 失败: %v", r.name, err)
			continue
		}
		fixed++
	}
	if fixed > 0 {
		logger.Infof("[db] 已规范化 %d 台飞鹅打印机的 SN(剥离「SN」标签前缀)", fixed)
	}
	for _, r := range invalid {
		logger.Warnf("[db] 飞鹅打印机[%s]的 SN %q 含非法字符,请按机身标签核对后到「打印机管理」重新保存", r.name, r.sn)
	}
}

// ============================================================================
// tb_role 历史数据修复
// ============================================================================

// dedupeRoleKeys 清理 tb_role 中 role_key 重复的行(幂等,无重复时零写入)。
//
// 背景:老版本在 MySQL 下 idx_role_key 退化为普通索引(不支持带 WHERE 的部分
// 索引),启动引导 SyncBuiltinRoles 依赖「INSERT IGNORE 撞唯一约束」保证幂等,
// 于是每次启动都会重复插入 4 个内置角色。升级到全局唯一索引前必须先去重,否则
// 建索引直接失败,老库升级会卡死在启动阶段。
//
// 保留策略:每个 role_key 保留一行 —— 优先保留未删除(del_flag='0')且 role_id
// 最小的行;该 key 下全是已删除行时保留 role_id 最小的行。被丢弃行的员工引用
// (tb_user.role_id)先改挂到保留行,再物理删除多余行。
func dedupeRoleKeys() {
	rows, err := DB.Query(`SELECT role_id, role_key, COALESCE(del_flag,'0') FROM tb_role ORDER BY role_id`)
	if err != nil {
		return
	}
	type roleRow struct {
		id      int
		key     string
		deleted bool
	}
	var all []roleRow
	for rows.Next() {
		var r roleRow
		var del string
		// 扫描失败不能跳过该行继续:下面的去重会 DELETE 角色行,若恰好漏读了
		// 「未删除的那一行」,keep 会指向一条已删除行,进而把唯一有效角色删掉,
		// 所有挂在该角色上的员工瞬间失去权限。宁可本轮不去重(下次启动再试)。
		if err := rows.Scan(&r.id, &r.key, &del); err != nil {
			logger.Warnf("[db] 读取角色行失败,跳过本轮角色去重: %v", err)
			rows.Close()
			return
		}
		r.deleted = del != "0"
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		logger.Warnf("[db] 读取角色行失败,跳过本轮角色去重: %v", err)
		rows.Close()
		return
	}
	rows.Close()
	if len(all) == 0 {
		return
	}

	keep := make(map[string]int, len(all))
	// 第一遍:优先保留未删除行中 role_id 最小的。
	for _, r := range all {
		if !r.deleted {
			if _, ok := keep[r.key]; !ok {
				keep[r.key] = r.id
			}
		}
	}
	// 第二遍:该 key 下全是已删除行时,保留 role_id 最小的。
	for _, r := range all {
		if _, ok := keep[r.key]; !ok {
			keep[r.key] = r.id
		}
	}

	dropped := 0
	for _, r := range all {
		if keep[r.key] == r.id {
			continue
		}
		// 员工引用改挂到保留行(表间无外键约束,顺序敏感:必须先改后删)。
		// 改挂失败时绝不能继续删除:tb_user.role_id 会指向不存在的角色,
		// 这些员工下一次请求即命中 AuthzRoleMissing,登录与鉴权直接失效。
		if _, err := DB.Exec(`UPDATE tb_user SET role_id=? WHERE role_id=?`, keep[r.key], r.id); err != nil {
			logger.Warnf("[db] 重复角色行 %d 的员工引用改挂失败,保留该行不删除: %v", r.id, err)
			continue
		}
		if _, err := DB.Exec(`DELETE FROM tb_role WHERE role_id=?`, r.id); err == nil {
			dropped++
		}
	}
	if dropped > 0 {
		logger.Infof("[db] 已清理 tb_role 中重复的角色行 %d 条(每个 role_key 保留一行)", dropped)
	}
}

// recreateRoleKeyIndex 检测 idx_role_key 的旧定义并原地重建为全局唯一索引。
//
// SQLite 旧定义是带 WHERE 的部分唯一索引、MySQL 旧定义是普通索引(部分索引
// 降级产物);两者与新定义同名,migrate 的「索引已存在则跳过」逻辑不会自动
// 升级,只能检测到旧定义后先删后建。
func recreateRoleKeyIndex() {
	if !indexExists("idx_role_key") {
		return
	}
	needRebuild := false
	if IsMySQL() {
		var nonUnique int
		err := DB.QueryRow(`SELECT NON_UNIQUE FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='tb_role' AND INDEX_NAME='idx_role_key'
			LIMIT 1`).Scan(&nonUnique)
		needRebuild = err == nil && nonUnique != 0
	} else {
		var def string
		err := DB.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='index' AND name='idx_role_key'`).Scan(&def)
		needRebuild = err == nil && strings.Contains(strings.ToUpper(def), "WHERE")
	}
	if !needRebuild {
		return
	}
	drop := `DROP INDEX idx_role_key`
	if IsMySQL() {
		drop = `DROP INDEX idx_role_key ON tb_role`
	}
	if _, err := DB.Exec(drop); err != nil {
		logger.Warnf("[db] 删除旧 idx_role_key 失败: %v", err)
		return
	}
	if _, err := DB.Exec(`CREATE UNIQUE INDEX idx_role_key ON tb_role(role_key)`); err != nil {
		logger.Fatalf("重建 idx_role_key 为唯一索引失败: %v", err)
	}
	logger.Infof("[db] idx_role_key 已重建为全局唯一索引")
}

// legacyShopNameWithSpace 出厂默认的店铺名曾用空格分隔「品牌 标语」,与全系统
// 「品牌 · 标语」的约定不一致:桌牌二维码的品牌名提取(前端 shopBrandName 按 [·•] 切分)
// 会取到整串,小票抬头与顾客端顶部也少一个分隔点。
//
// 保留该常量只用于一次性数据改写,新代码一律以 settingDefaults["shop_name"] 为准。
const legacyShopNameWithSpace = "长健农场 柴火农家土菜"

// migrateShopNameDot 把仍是出厂默认、且用空格分隔的店铺名统一成中点分隔。
//
// 只命中出厂默认那一条精确值:门店若已自行改名(哪怕是别的空格分隔写法),一律不动,
// 避免启动时悄悄改写商家自己的配置。幂等,重复执行无副作用。
func migrateShopNameDot() {
	want := settingDefaults["shop_name"]
	if want == "" {
		return
	}
	res, err := DB.Exec(`UPDATE tb_config SET cfg_value=?, update_time=? WHERE cfg_key='shop_name' AND cfg_value=?`,
		want, Now(), legacyShopNameWithSpace)
	if err != nil {
		// 不 Fatalf:店铺名只是展示口径,不该让服务起不来。
		logger.Warnf("[db] 店铺名统一为「品牌 · 标语」失败: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logger.Infof("[db] 已将店铺名改写为 %q(与品牌名提取规则一致)", want)
	}
}

// legacyUploadPrefix 是图片/收款码的历史路径前缀,已废弃。
// 保留此常量只用于一次性数据改写,新代码一律使用 UploadURLPrefix。
const legacyUploadPrefix = "/picture/"

// migrateUploadPrefix 把存量数据里的 /picture/ 前缀统一改写为 /uploads/。
//
// 背景:菜品图与收款码早期写入的是 /picture/xxx,但上传接口返回、后端静态路由
// 与 Nginx 反代用的都是 /uploads/ —— 两套前缀并存时,/picture/ 在生产会落进前端
// SPA 兜底而返回 index.html(收款码在顾客端直接显示不出来)。
// 这里做一次性幂等改写:改完再删掉 /picture 路由,存量数据不会 404。
func migrateUploadPrefix() {
	updates := []string{
		`UPDATE tb_dish SET dish_image = REPLACE(dish_image, '` + legacyUploadPrefix + `', '` + UploadURLPrefix + `')
			WHERE dish_image LIKE '` + legacyUploadPrefix + `%'`,
		`UPDATE tb_config SET cfg_value = REPLACE(cfg_value, '` + legacyUploadPrefix + `', '` + UploadURLPrefix + `'),
			update_time = '` + Now() + `' WHERE cfg_value LIKE '` + legacyUploadPrefix + `%'`,
	}
	for _, stmt := range updates {
		res, err := DB.Exec(stmt)
		if err != nil {
			// 不 Fatalf:路径前缀是展示问题,不该让服务起不来。
			logger.Warnf("[db] 统一图片路径前缀失败: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			logger.Infof("[db] 已将 %d 条 %s 路径改写为 %s", n, legacyUploadPrefix, UploadURLPrefix)
		}
	}
}

// backfillTableCodes 为存量桌台补发稳定码(老库升级时一次性完成)。
func backfillTableCodes() {
	rows, err := DB.Query(`SELECT table_id FROM tb_table WHERE table_code IS NULL OR table_code=''`)
	if err != nil {
		logger.Warnf("backfillTableCodes: %v", err)
		return
	}
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := EnsureTableCode(id); err != nil {
			logger.Warnf("backfillTableCodes(table %d): %v", id, err)
		}
	}
}
