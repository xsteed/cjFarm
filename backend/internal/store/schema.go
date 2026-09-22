package store

import (
	"dining-system/internal/logger"
	"fmt"
	"strings"
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
//
// 类型选择原则(两库语义尽量对齐):
//   - 主键/外键/金额/计数 一律 INTEGER:SQLite 原生 64 位,MySQL 映射为 INT(≈±21 亿,
//     金额单位为「分」,单笔上限约 2100 万元,足够覆盖餐饮单笔订单);
//   - 字符串一律 VARCHAR(n):SQLite 归入 TEXT 亲和性且不校验长度,MySQL 为变长字符串
//     并强制长度上限 —— 这是两库唯一的显式行为差异,写入前请遵守长度约定;
//   - 时间一律 VARCHAR(32) 存 'YYYY-MM-DD HH:MM:SS' 字符串,两库排序/区间比较行为一致,
//     避免 TIMESTAMP 的时区与零值('0000-00-00')差异,也便于跨库迁移。
var schemaTemplate = []string{
	`CREATE TABLE IF NOT EXISTS tb_table (
		table_id    {{PK}},
		table_no    VARCHAR(32)  NOT NULL,
		table_name  VARCHAR(64)  NOT NULL,
		capacity    INTEGER      DEFAULT 0,
		status      INTEGER      DEFAULT 0,
		sort_order  INTEGER      DEFAULT 0,
		del_flag    VARCHAR(2)   DEFAULT '0',
		create_by   VARCHAR(64)  DEFAULT '',
		create_time VARCHAR(32),
		update_by   VARCHAR(64)  DEFAULT '',
		update_time VARCHAR(32),
		remark      VARCHAR(500),
		table_code  VARCHAR(16)  DEFAULT ''
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_category (
		category_id   {{PK}},
		category_name VARCHAR(64) NOT NULL,
		sort_order    INTEGER     DEFAULT 0,
		del_flag      VARCHAR(2)  DEFAULT '0',
		create_time   VARCHAR(32),
		update_time   VARCHAR(32)
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_dish (
		dish_id     {{PK}},
		category_id INTEGER      NOT NULL,
		dish_name   VARCHAR(128) NOT NULL,
		dish_image  VARCHAR(255) DEFAULT '',
		description VARCHAR(500) DEFAULT '',
		status      INTEGER      DEFAULT 1,
		sort_order  INTEGER      DEFAULT 0,
		del_flag    VARCHAR(2)   DEFAULT '0',
		create_by   VARCHAR(64)  DEFAULT '',
		create_time VARCHAR(32),
		update_by   VARCHAR(64)  DEFAULT '',
		update_time VARCHAR(32),
		remark      VARCHAR(500)
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_spec (
		spec_id   {{PK}},
		dish_id   INTEGER      NOT NULL,
		spec_name VARCHAR(64)  NOT NULL,
		price     INTEGER      DEFAULT 0
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_remark (
		remark_id   {{PK}},
		option_name VARCHAR(64) NOT NULL,
		sort_order  INTEGER     DEFAULT 0,
		del_flag    VARCHAR(2)  DEFAULT '0',
		create_time VARCHAR(32),
		update_time VARCHAR(32)
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
		create_time  VARCHAR(32),
		update_time  VARCHAR(32)
	){{OPTS}}`,

	// 打印日志:每「向一台打印机发一次单据」记一行(成功与失败都记)。
	// 打印本身是异步的、且打印机在线与否不受后端控制,没有这张表时
	// 失败了只有后端日志留痕,商家端完全无感知(小票没出来也不知道为什么)。
	// detail 对 TCP 存「地址 / 失败原因」,对飞鹅存「云端订单号」,
	// remote_id 单独存飞鹅订单号,便于用 Open_queryOrderState 追问是否真的打出来了。
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
		detail       VARCHAR(500) DEFAULT '',
		trigger_by   VARCHAR(16)  DEFAULT '',
		operator     VARCHAR(64)  DEFAULT '',
		cost_ms      INTEGER      DEFAULT 0,
		create_time  VARCHAR(32)
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
		claim_time   VARCHAR(32),
		next_try_time VARCHAR(32),
		trigger_by   VARCHAR(16)   DEFAULT '',
		operator     VARCHAR(64)   DEFAULT '',
		create_time  VARCHAR(32),
		done_time    VARCHAR(32)
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
		create_time   VARCHAR(32)
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_config (
		cfg_key   VARCHAR(64)   PRIMARY KEY,
		cfg_value VARCHAR(4000) DEFAULT ''
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
		pay_time        VARCHAR(32),
		transaction_id  VARCHAR(64)  DEFAULT '',
		pay_channel     VARCHAR(16)  DEFAULT '',
		refund_amount   INTEGER      DEFAULT 0,
		refund_time     VARCHAR(32),
		finish_time     VARCHAR(32),
		order_remark    VARCHAR(500) DEFAULT '',
		cancel_reason   VARCHAR(255) DEFAULT '',
		begin_time      VARCHAR(32),
		end_time        VARCHAR(32),
		create_by       VARCHAR(64)  DEFAULT '',
		create_time     VARCHAR(32),
		update_by       VARCHAR(64)  DEFAULT '',
		update_time     VARCHAR(32),
		remark          VARCHAR(500),
		settle_type     VARCHAR(16)  DEFAULT 'normal',
		settle_time     VARCHAR(32),
		settle_operator VARCHAR(64)  DEFAULT '',
		settle_remark   VARCHAR(255) DEFAULT '',
		credit_status   INTEGER      DEFAULT 0,
		credit_amount   INTEGER      DEFAULT 0,
		credit_settle_time VARCHAR(32),
		credit_settle_by   VARCHAR(64) DEFAULT '',
		paid_amount     INTEGER      DEFAULT 0
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
		remark    VARCHAR(255) DEFAULT ''
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
		create_time VARCHAR(32),
		handle_time VARCHAR(32),
		handle_by   VARCHAR(64)  DEFAULT ''
	){{OPTS}}`,

	`CREATE TABLE IF NOT EXISTS tb_payment (
		payment_id       {{PK}},
		order_no         VARCHAR(64)  NOT NULL,
		channel          VARCHAR(16)  NOT NULL,
		channel_trade_no VARCHAR(64)  DEFAULT '',
		amount           INTEGER      DEFAULT 0,
		status           INTEGER      DEFAULT 0,
		prepay_id        VARCHAR(128) DEFAULT '',
		notify_time      VARCHAR(32),
		create_time      VARCHAR(32),
		update_time      VARCHAR(32)
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
		create_time       VARCHAR(32),
		update_time       VARCHAR(32)
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
		create_time VARCHAR(32),
		update_time VARCHAR(32),
		remark      VARCHAR(500)  DEFAULT ''
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
		last_login_time VARCHAR(32),
		last_login_ip   VARCHAR(64)  DEFAULT '',
		login_count     INTEGER      DEFAULT 0,
		pwd_update_time VARCHAR(32),
		token_version   INTEGER      DEFAULT 0,
		del_flag        VARCHAR(2)   DEFAULT '0',
		create_by       VARCHAR(64)  DEFAULT '',
		create_time     VARCHAR(32),
		update_by       VARCHAR(64)  DEFAULT '',
		update_time     VARCHAR(32),
		remark          VARCHAR(500) DEFAULT ''
	){{OPTS}}`,

	// 记住登录令牌(实现「记住我(7/30 天免登录)」)。
	// 令牌为后端生成的随机串,存本地后仅用于向后端换取新登录态;
	// 是否过期由 expire_time 决定(后端查库裁决,前端无法篡改),与后端访问令牌(默认 24h)解耦。
	// 改密/停用/删除账号时删掉对应行,该账号全部「记住我」会话立即失效。
	`CREATE TABLE IF NOT EXISTS tb_remember_token (
		token_id       {{PK}},
		user_id        INTEGER      NOT NULL,
		token          VARCHAR(64)  NOT NULL,
		expire_time    VARCHAR(32)  NOT NULL,
		create_time    VARCHAR(32),
		last_used_time VARCHAR(32)
	){{OPTS}}`,
}

// schemaStatements 返回指定方言下的全部建表语句。
func schemaStatements(d Dialect) []string {
	out := make([]string, 0, len(schemaTemplate))
	for _, tpl := range schemaTemplate {
		s := strings.ReplaceAll(tpl, "{{PK}}", autoIncPKFor(d))
		s = strings.ReplaceAll(s, "{{OPTS}}", tableOptionsFor(d))
		out = append(out, s)
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

// indexDefs 全部索引(共 25 个)。
// 唯一索引保证 order_no / refund_no / 渠道交易号不重复;
// 其余索引用于订单查询、看板与报表加速。
var indexDefs = []indexDef{
	{Name: "idx_order_no", Table: "tb_order", Columns: "order_no", Unique: true},
	{Name: "idx_order_table", Table: "tb_order", Columns: "table_id"},
	{Name: "idx_order_status", Table: "tb_order", Columns: "order_status"},
	{Name: "idx_order_create_time", Table: "tb_order", Columns: "create_time"},
	{Name: "idx_order_item_order", Table: "tb_order_item", Columns: "order_id"},
	{Name: "idx_payment_order", Table: "tb_payment", Columns: "order_no"},
	{Name: "idx_payment_channel_trade", Table: "tb_payment", Columns: "channel, channel_trade_no", Unique: true},
	{Name: "idx_refund_no", Table: "tb_refund", Columns: "refund_no", Unique: true},
	{Name: "idx_refund_order", Table: "tb_refund", Columns: "order_id"},
	{Name: "idx_urge_status", Table: "tb_order_urge", Columns: "status, create_time"},
	{Name: "idx_urge_order", Table: "tb_order_urge", Columns: "order_id"},
	// 打印日志:按订单倒查(「这单到底打了没」)、按时间翻页、按失败筛选。
	{Name: "idx_print_log_order", Table: "tb_print_log", Columns: "order_id"},
	{Name: "idx_print_log_time", Table: "tb_print_log", Columns: "create_time"},
	{Name: "idx_print_log_status", Table: "tb_print_log", Columns: "status, create_time"},
	// 代理任务队列:取单按「状态 + 可执行时间」筛(代理每 3 秒轮询一次,必须有索引),
	// 打印日志页按「打印机 + 状态」看某台机器的积压。
	{Name: "idx_print_job_pick", Table: "tb_print_job", Columns: "status, next_try_time, job_id"},
	{Name: "idx_print_job_printer", Table: "tb_print_job", Columns: "printer_id, status"},
	{Name: "idx_table_code", Table: "tb_table", Columns: "table_code", Unique: true, Where: "table_code!=''"},
	// 用户名/角色标识的唯一性只约束「未删除」的行 —— 删掉员工后可以重新使用同名账号。
	// 与 idx_table_code 同理:MySQL 不支持部分索引会降级为普通索引,
	// 唯一性由应用层 EnsureUsernameAvailable / EnsureRoleKeyAvailable 保证。
	{Name: "idx_user_username", Table: "tb_user", Columns: "username", Unique: true, Where: "del_flag='0'"},
	{Name: "idx_user_role", Table: "tb_user", Columns: "role_id"},
	{Name: "idx_role_key", Table: "tb_role", Columns: "role_key", Unique: true, Where: "del_flag='0'"},
	// 操作日志:按时间翻页(主查询)、按人追责、按对象倒查「谁动过这一单」、只看失败。
	{Name: "idx_operlog_time", Table: "tb_oper_log", Columns: "create_time"},
	{Name: "idx_operlog_operator", Table: "tb_oper_log", Columns: "operator_id, create_time"},
	{Name: "idx_operlog_target", Table: "tb_oper_log", Columns: "target_type, target_id"},
	{Name: "idx_operlog_status", Table: "tb_oper_log", Columns: "status, create_time"},
	{Name: "idx_remember_token", Table: "tb_remember_token", Columns: "token", Unique: true},
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
	{Table: "tb_order", Name: "credit_settle_by", Def: "VARCHAR(64) DEFAULT ''"},
	// 实收金额(分): 免单=0;挂账核销前=0,核销后为应收金额;退款时相应扣减。
	{Table: "tb_order", Name: "paid_amount", Def: "INTEGER DEFAULT 0"},
	// 桌台稳定码: 创建时生成、永不变更的随机码,二维码内容使用它(而非自增 ID)。
	{Table: "tb_table", Name: "table_code", Def: "VARCHAR(16) DEFAULT ''"},
	// 打印机厂商与云打印: provider 默认 'tcp' —— 老库里的打印机都是直连网络机,
	// 升级后行为不变;切到飞鹅只需把 provider 改成 'feie' 并填 feie_sn。
	{Table: "tb_printer", Name: "provider", Def: "VARCHAR(16) DEFAULT 'tcp'"},
	{Table: "tb_printer", Name: "feie_sn", Def: "VARCHAR(64) DEFAULT ''"},
	// 打印份数(1-5) 与「按菜品分类分单」(CSV 分类 ID,空=全收)。
	{Table: "tb_printer", Name: "copies", Def: "INTEGER DEFAULT 1"},
	{Table: "tb_printer", Name: "category_ids", Def: "VARCHAR(255) DEFAULT ''"},
}

// alterStatements 返回指定方言下的增量列 DDL。
func alterStatements(d Dialect) []string {
	out := make([]string, 0, len(alterCols))
	for _, c := range alterCols {
		out = append(out, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.Table, c.Name, c.Def))
	}
	return out
}

// ============================================================================
// 迁移执行
// ============================================================================

// migrate 建表(幂等)、补索引、补列并回填历史数据。
//
// 幂等策略:
//   - 建表:CREATE TABLE IF NOT EXISTS(两库均支持);
//   - 建索引:先查元数据判断索引是否已存在再创建
//     (SQLite 的 IF NOT EXISTS 是专有语法,MySQL 无对应写法,统一走存在性判断);
//   - 补列:先查 information_schema / PRAGMA 判断列是否已存在再 ALTER
//     (ADD COLUMN 两库都不支持 IF NOT EXISTS)。
func migrate() {
	for _, s := range schemaStatements(dialect) {
		if _, err := DB.Exec(s); err != nil {
			logger.Fatalf("建表失败(%s): %v\n%s", dialect, err, s)
		}
	}

	for _, ix := range indexDefs {
		if indexExists(ix.Name) {
			continue
		}
		stmt := indexStatement(dialect, ix)
		if _, err := DB.Exec(stmt); err != nil {
			logger.Fatalf("建索引失败(%s): %v\n%s", dialect, err, stmt)
		}
	}

	for _, c := range alterCols {
		if columnExists(c.Table, c.Name) {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.Table, c.Name, c.Def)
		if _, err := DB.Exec(stmt); err != nil {
			logger.Fatalf("补列失败(%s): %v\n%s", dialect, err, stmt)
		}
		logger.Infof("[db] 已补列 %s.%s", c.Table, c.Name)
	}

	// 历史数据回填:老订单没有实收金额,按「已支付即全额实收」补齐,保证报表口径连续。
	DB.Exec(`UPDATE tb_order SET paid_amount = total_amount - COALESCE(refund_amount,0)
		WHERE pay_status=1 AND (paid_amount IS NULL OR paid_amount=0) AND (settle_type IS NULL OR settle_type='normal')`)
	DB.Exec(`UPDATE tb_order SET settle_type='normal' WHERE settle_type IS NULL OR settle_type=''`)
	backfillTableCodes()
	migrateUploadPrefix()
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
		`UPDATE tb_config SET cfg_value = REPLACE(cfg_value, '` + legacyUploadPrefix + `', '` + UploadURLPrefix + `')
			WHERE cfg_value LIKE '` + legacyUploadPrefix + `%'`,
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
