package store

import (
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
)

// 种子数据的「时间戳」口径:与目标站一致,固定值便于各环境数据一致。
const (
	seedCreateTime    = "2026-09-12 13:06:43"
	seedTableUpdTime  = "2026-09-13 20:14:16"
	seedSysCreateTime = "2026-09-12 13:06:43"
)

// seed 首次初始化种子数据。
//
// 幂等策略:以「分类表是否为空」作为整体短路条件 —— 只要库里已有业务数据
// 就不再灌业务种子,避免覆盖商户后续的增删改。配置项除外,它们
// 每次启动都要补齐(见 EnsureSettingDefaults)。
func seed() {
	// 配置项默认值必须「每次启动」都补齐:老库(建库时还没有某个配置键)不会走下面的
	// 种子分支,若不补齐,GetSetting 对缺失键返回空串,前端开关组件会因此报错并卡死路由渲染。
	EnsureSettingDefaults()
	// 启动审计:任何「出厂默认非空、当前却被清空」的配置项都会打 [warn]。
	// 这是之前 8 个配置项静默失效的根因 —— INSERT OR IGNORE 不会回填已存在的空键。
	AuditSettingEmpties()

	// 角色/管理员引导已由 dao.BootstrapRoles 负责,由 main 在 store.Init 之后调用。
	// 这里不再调用 SyncBuiltinRoles / EnsureAdminUser / NormalizeRolePerms,
	// 以避免 store 根包反向依赖 dao 包。

	var n int
	DB.QueryRow(`SELECT COUNT(*) FROM tb_category`).Scan(&n)
	if n > 0 {
		return
	}
	seedTables()
	seedCategories()
	seedDishes()
	seedRemarks()
	seedPrinters()
	logger.Infof("种子数据已初始化")
}

// settingDefaults 配置项出厂默认值。
// 既用于首次初始化,也用于每次启动补齐「缺失」的键(冲突则忽略,绝不覆盖已有值)。
// 该表同时是 migrations/*/seed.sql 中 tb_config 部分的数据来源。
var settingDefaults = map[string]string{
	// 店铺名即「品牌 · 标语」组合:顾客端顶部、小票抬头直接整串展示,
	// 桌牌二维码按 [·•] 取前一段作品牌名(见前端 shopBrandName),故分隔符必须是中点。
	"shop_name":           "长健农场 · 柴火农家土菜",
	"shop_logo":           "",
	"seat_fee_enabled":    "1",
	"seat_fee":            "6",
	"pay_qr_wx":           "/uploads/pay_wx.png",
	"pay_qr_ali":          "/uploads/pay_ali.jpg",
	"h5_base_url":         "http://localhost:8080",
	"promotion_enabled":   "0",
	"promotion_threshold": "100",
	"promotion_discount":  "0",
	// 小票打印(默认开启:没有打印机时下单也不会报错,只是日志里记一条「无可用打印机」)。
	//   print_enabled             打印总开关,关闭后所有自动打印一律跳过(手动补打仍可用);
	//   print_kitchen_show_price  厨房单是否带单价与金额(默认不打,后厨只看菜名和数量);
	//   print_guest_footer        顾客小票页脚文案,清空则不打印页脚(门店可改成自己的口号);
	//   print_guest_show_discount 结账小票是否显示优惠行(默认显示,有优惠时才出现该行;点菜单不出优惠行);
	//   print_guest_on_append     加菜后是否向小票机出「加菜小票」(只列本次新菜,默认开);
	//   print_guest_on_order      下单后是否向小票机出「点菜单」(默认开,嫌与结账小票重复可关)。
	//   print_kitchen_split_mode  厨房单打印粒度:whole=整单一张(默认)/ item=一菜一单 /
	//                             portion=一菜一单且按份数拆(点 3 份出 3 张)。拆单后每张小票
	//                             带桌号(放大居中)与序号(如 2/5),方便夹在菜上、传菜对口;
	//                             整单备注会随每张小票重复打印,菜品多时更费纸。
	"print_enabled":             "1",
	"print_kitchen_show_price":  "0",
	"print_kitchen_split_mode":  "whole",
	"print_guest_footer":        "谢谢惠顾，欢迎再次光临！",
	"print_guest_show_discount": "1",
	"print_guest_on_append":     "1",
	"print_guest_on_order":      "1",
	// 飞鹅云打印(provider=feie 的打印机使用)。
	//   feie_user    飞鹅云后台注册账号(手机号/邮箱);
	//   feie_ukey    开发者 UKEY —— 敏感项,落库前自动加密,接口不回显;
	//   feie_api_url 接口地址,国内默认 api.feieyun.cn,海外/私有云可改。
	"feie_user":    "",
	"feie_ukey":    "",
	"feie_api_url": "https://api.feieyun.cn/Api/Open/",
	// 本地打印代理(provider=agent 的打印机使用)。
	//   agent_token 门店代理程序出站拉单时的鉴权令牌 —— 敏感项,落库前自动加密、
	//   接口不回显;留空表示未启用代理通道(代理程序会被拒绝,并提示去系统配置填写)。
	//   生成方式见 docs/print-agent.md(要求足够长且随机,等同于一台打印机的操作权限)。
	"agent_token": "",
	// 在线支付(默认关闭,待商户申请 key 后填写并开启)
	//   wxpay_jsapi_enabled 小程序内支付(JSAPI):默认关。它比 Native 多一个硬前提 ——
	//   wxpay_appid 必须是小程序 AppID 且配了 wx_app_secret,否则顾客点了只会看到
	//   「支付暂不可用」;宁可少一个入口,也不要出现点了调不起收银台的情况。
	"wxpay_enabled":            "0",
	"wxpay_jsapi_enabled":      "0",
	// 储值自助充值:默认关(动钱的入口一律 fail-closed)。开启前须先配好微信支付(JSAPI),
	// 否则顾客点了充值只会看到「支付暂不可用」。关闭不影响已有余额的消费与退款 ——
	// 那是顾客的钱,进得来就出得去。
	"wallet_recharge_enabled":  "0",
	"alipay_enabled":           "0",
	"wxpay_mchid":              "",
	"wxpay_appid":              "",
	"wxpay_apiv3_key":          "",
	"wxpay_serial_no":          "",
	"wxpay_private_key_path":   "",
	"wxpay_platform_cert_path": "",
	"wxpay_pubkey_id":          "",
	"wxpay_pubkey_path":        "",
	"wxpay_notify_url":         "",
	// 小程序桌牌码(AppID 与微信支付共用 wxpay_appid)。
	//   wx_app_secret        小程序 AppSecret —— 敏感项,落库前自动加密;留空则不出小程序码。
	//   wx_code_env_version  出码指向的版本:release 正式版 / trial 体验版 / develop 开发版。
	//                        微信要求 page 必须存在于目标版本,故未发布前必须设成 trial/develop,
	//                        否则接口一律返回 41030(详见 docs/miniprogram.md)。
	"wx_app_secret":           "",
	"wx_code_env_version":     "release",
	"alipay_appid":            "",
	"alipay_private_key_path": "",
	"alipay_public_key":       "",
	"alipay_notify_url":       "",
	// 桌台遗留订单防护(两项默认都关:开不开取决于店家的营业习惯与营业时段)。
	// 未接在线支付时「收款」全靠店家手动点,漏点一次桌台就永远占用,下一桌扫码会被
	// 直接带进上一桌的账单。这里提供两道防线(实现见 application/service/stale_service.go):
	//   stale_release_enabled 跨日自动清台:超过一个营业日仍未结账的订单,其桌台自动恢复
	//     空闲,且顾客端读取/下单防重/桌台释放判定同步隔离这些旧单。只动桌台状态,不动
	//     订单与账务 —— 自动补结账会虚增营收、自动作废会漏记收入,都比留一张待处理的单危险。
	//   stale_alert_enabled   未结账超时告警:看板上标出当天开台超过 N 小时还没收款的桌。
	//     不改变任何状态,只是把「可能要忘」推到店员眼前。
	//   stale_alert_hours     告警阈值(小时,合法区间 1~72;越界回落 4)。
	//   biz_date_cutoff_hour  营业日切换时刻(0~12 点,0 = 按自然日 00:00 切换)。
	// 营业日不等同于自然日:晚市客人 23:50 下单、00:30 还在吃是常态。若一律按 00:00 切,
	// 这些订单会在午夜瞬间变成「上一营业日的遗留单」—— 客人看不到自己的单、还能重复
	// 下单,服务员在看板上也找不到它去收款。把切换时刻后移到打烊之后(如凌晨 4 点),
	// 营业日才真正对上「这一场营业」;它同时决定当日取餐号在哪一刻重置回 1。
	"stale_release_enabled": "0",
	"stale_alert_enabled":   "0",
	"stale_alert_hours":     "4",
	"biz_date_cutoff_hour":  "0",
	// 先付/后付模式(见 application/service.PayFirstEnabled):
	//   "0"(默认)后付 —— 顾客下单即出单做菜,餐后/离店前结账;
	//   "1" 先付 —— 顾客下单需先完成支付,后厨在收款成功后才出单。
	// 默认关是刻意的:先付模式下没收到钱的单不会出单,一旦误开会让后厨一单都收不到。
	"pay_first_enabled": "0",
	// 会员功能总闸(见 application/service.memberSwitchOn):
	//   "1" 开放 —— 小程序底部「我的」入口与会员注册/登录照常;
	//   "0"(默认)关闭 —— 顾客端整体隐藏会员入口,登录/静默重登一律拒绝。
	// 它只由商家在管理端「营业设置 → 会员功能」控制,与 AppSecret 等登录能力
	// 配置解耦。默认关与 wxpay_jsapi_enabled 同一思路:宁可少一个入口,也不要在
	// 未配 AppSecret 的部署上出现「入口在、点注册必失败」的哑火状态。
	"member_enabled": "0",
}

// SettingDefault 返回配置项出厂默认值(不存在时返回空串)。
// 供 dao 包读取配置项默认值,避免直接暴露 settingDefaults map。
func SettingDefault(key string) string {
	return settingDefaults[key]
}

// EnsureSettingDefaults 补齐缺失的配置项(幂等,已存在的键保持原值)。
// 语句由方言层生成:SQLite 为 INSERT OR IGNORE,MySQL 为 INSERT IGNORE。
func EnsureSettingDefaults() {
	// 新插入的键记一次 create_time;已有键被 IGNORE 忽略,首次时间不被启动流程改写。
	stmt := InsertIgnoreInto("tb_config", "cfg_key", "cfg_value", "create_time")
	var added, existed int
	for k, v := range settingDefaults {
		res, err := DB.Exec(stmt, k, v, Now())
		if err != nil {
			logger.Warnf("[setting] 补全默认配置项 %s 失败: %v", k, err)
			continue
		}
		// INSERT OR IGNORE:新插入返回 1 行受影响,已存在则 0(不会覆盖现值为空)。
		if n, e := res.RowsAffected(); e == nil && n > 0 {
			added++
			logger.Infof("[setting] 新增缺失配置项 %s = %q", k, v)
		} else {
			existed++
		}
	}
	logger.Infof("[setting] 配置项补齐完成: 新增 %d 项, 已存在 %d 项", added, existed)

	// 页脚出厂文案更版(全角逗号 + 感叹号):把仍停留在**旧出厂文案**的存量值一并升级。
	// 只精确匹配旧默认串 —— 商户改成过自定义文案的不受影响;幂等,可重复执行。
	const legacyGuestFooter = "谢谢惠顾,欢迎再次光临"
	if settingDefaults["print_guest_footer"] != legacyGuestFooter {
		if _, err := DB.Exec(`UPDATE tb_config SET cfg_value=?, update_time=?
			WHERE cfg_key='print_guest_footer' AND cfg_value=?`,
			settingDefaults["print_guest_footer"], Now(), legacyGuestFooter); err != nil {
			logger.Warnf("[setting] 升级旧版页脚默认文案失败: %v", err)
		}
	}
}

// AuditSettingEmpties 扫描配置表,对「出厂默认非空但当前为空」的键打 [warn]。
// 这类键是上次事故的根因:配置项被清空后 INSERT OR IGNORE 不会回填,导致功能静默失效。
// 出厂默认本就为空的项(shop_logo / 各类密钥占位)不计入,避免误报。
func AuditSettingEmpties() {
	rows, err := DB.Query(`SELECT cfg_key, cfg_value FROM tb_config`)
	if err != nil {
		logger.Warnf("[setting][告警] 读取配置表失败,无法审计空值: %v", err)
		return
	}
	defer rows.Close()
	var empties []string
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		if v == "" && settingDefaults[k] != "" {
			empties = append(empties, k)
		}
	}
	if len(empties) == 0 {
		logger.Infof("[setting] 配置审计通过: 无「应非空却为空」的配置项")
		return
	}
	logger.Warnf("[setting][告警] 发现 %d 个配置项被清空(出厂默认非空,当前为空): %s",
		len(empties), strings.Join(empties, ", "))
}

// UploadURLPrefix 是图片/收款码在库里存储的路径前缀,与后端静态路由、Nginx 反代、
// 上传接口返回值三处必须一致。存量库中的 /picture/ 前缀由 migrateUploadPrefix 改写。
const UploadURLPrefix = "/uploads/"

// ============================================================================
// 种子数据定义(业务数据唯一来源)
//
// 这里的切片同时被两处消费:
//  1. 运行期 seedXxx():首次启动时写入数据库;
//  2. cmd/gensql:生成 migrations/*/seed.sql 脚本。
//
// 因此新增/修改种子数据只需改这里一处,SQL 脚本用 `go run ./cmd/gensql` 重新生成。
// ============================================================================

// seedTableRows 桌台(8 张):01~04 大厅桌、07~08 包间、09~10 露台桌。
var seedTableRows = []po.Table{
	{TableNo: "01", TableName: "大厅01桌", Capacity: 10, Status: 0, SortOrder: 1},
	{TableNo: "02", TableName: "大厅02桌", Capacity: 10, Status: 0, SortOrder: 2},
	{TableNo: "03", TableName: "大厅03桌", Capacity: 10, Status: 0, SortOrder: 3},
	{TableNo: "04", TableName: "大厅04桌", Capacity: 10, Status: 0, SortOrder: 4},
	{TableNo: "07", TableName: "大包间(一)", Capacity: 8, Status: 0, SortOrder: 5},
	{TableNo: "08", TableName: "小包间(二)", Capacity: 6, Status: 0, SortOrder: 6},
	{TableNo: "09", TableName: "露台大桌", Capacity: 8, Status: 0, SortOrder: 7},
	{TableNo: "10", TableName: "露台小桌", Capacity: 10, Status: 0, SortOrder: 8},
}

// seedCategoryRows 菜品分类(6 个)。
var seedCategoryRows = []po.Category{
	{CategoryName: "凉菜素菜", SortOrder: 1},
	{CategoryName: "荤菜", SortOrder: 2},
	{CategoryName: "特色农家菜", SortOrder: 3},
	{CategoryName: "汤品", SortOrder: 4},
	{CategoryName: "主食", SortOrder: 5},
	{CategoryName: "海鲜预订", SortOrder: 6},
}

// seedRemarkRows 备注常用语(6 项)。
var seedRemarkRows = []string{"加辣", "微辣", "免辣", "少盐", "少油", "不要香菜"}

// printerSeed 打印机种子。
type printerSeed struct {
	Name  string
	PType int // 1 厨房单 / 2 顾客小票
	IP    string
	Port  int
	Width int // 48=80mm
}

// seedPrinterRows 打印机(2 台,默认停用)。
//
// 与目标站一致:1=厨房单, 2=顾客小票; paper_width 48=80mm; status 0=停用。
// 种子打印机默认「停用」(status=0):IP 为占位地址,若默认启用,每次下单都会异步尝试
// 连接不存在的打印机(2×3s 超时),由商户录入真实 IP 后自行启用。
var seedPrinterRows = []printerSeed{
	{"后厨厨房单打印机", 1, "192.168.1.8", 9100, 48},
	{"前台顾客小票打印机", 2, "192.168.1.201", 9100, 48},
}

// dishSeed 菜品及其规格。
type dishSeed struct {
	cat   string
	name  string
	desc  string
	img   string // 图片相对路径(/uploads/...)
	specs []specSeed
}

// specSeed 规格:name 规格名,price 单价(单位:分)。
type specSeed struct {
	name  string
	price int64
}

// seedDishRows 菜品(21 道)+ 规格(32 条)。
var seedDishRows = []dishSeed{
	{"凉菜素菜", "凉拌青瓜", "清爽开胃", "/uploads/dining_20260918_001.jpeg", []specSeed{{"份", 2800}}},
	{"凉菜素菜", "本场时蔬", "当日新鲜时令蔬菜", "/uploads/dining_20260918_002.jpeg", []specSeed{{"份", 2800}}},
	{"凉菜素菜", "清炒腐竹", "", "/uploads/dining_20260918_003.jpeg", []specSeed{{"份", 3800}}},
	{"凉菜素菜", "山泉水豆腐", "", "/uploads/dining_20260918_004.png", []specSeed{{"小份", 4800}, {"大份", 6800}}},
	{"荤菜", "香煎万绿湖鱼干", "推荐加辣", "/uploads/dining_20260918_005.png", []specSeed{{"份", 6800}}},
	{"荤菜", "砵仔黑土猪肉", "", "/uploads/dining_20260918_006.png", []specSeed{{"份", 6800}}},
	{"荤菜", "盐水猪脚", "", "/uploads/dining_20260918_007.png", []specSeed{{"份", 6800}}},
	{"荤菜", "沙姜猪肚", "", "/uploads/dining_20260918_008.png", []specSeed{{"份", 6800}}},
	{"荤菜", "葱姜焗鱼（清蒸）", "", "/uploads/dining_20260918_009.png", []specSeed{{"小条", 6800}, {"大条", 8800}}},
	{"荤菜", "秘制萝卜牛腩煲", "", "/uploads/dining_20260918_010.png", []specSeed{{"份", 6800}}},
	{"特色农家菜", "五指毛桃鸡", "", "/uploads/dining_20260918_011.png", []specSeed{{"份", 9800}, {"只", 18800}}},
	{"特色农家菜", "茶油蒸长健果园鸡", "", "/uploads/dining_20260918_012.png", []specSeed{{"份", 9800}, {"只", 18800}}},
	{"特色农家菜", "荔枝果园土鹅", "", "/uploads/dining_20260918_013.png", []specSeed{{"份", 9800}}},
	{"特色农家菜", "地胆头蒸老鸭", "", "/uploads/dining_20260918_014.png", []specSeed{{"份", 9800}}},
	{"特色农家菜", "荔枝柴火窑/烧鸡", "新鲜宰杀，需提前2小时预约", "/uploads/dining_20260918_015.png", []specSeed{{"只", 13800}}},
	{"汤品", "本场黑猪汤", "小份3人 / 中份6人 / 大份10人", "/uploads/dining_20260918_016.png", []specSeed{{"小份(3人)", 3800}, {"中份(6人)", 6800}, {"大份(10人)", 9800}}},
	{"汤品", "预约柴火炖汤", "按位计费，需提前预约，138元起(3-5人)", "/uploads/dining_20260918_017.png", []specSeed{{"23元/位", 2300}, {"30元/位", 3000}, {"38元/位", 3800}, {"138元起(3-5人)", 13800}}},
	{"汤品", "柴火炖鸡", "需提前2小时预约", "/uploads/dining_20260918_018.png", []specSeed{{"份", 21800}}},
	{"汤品", "柴火炖纯鸡汤", "需提前2小时预约", "/uploads/dining_20260918_019.png", []specSeed{{"份", 21800}}},
	{"主食", "长健特色炒饭", "", "/uploads/dining_20260918_020.png", []specSeed{{"小份", 3800}, {"中份", 6800}, {"大份", 8800}}},
	{"海鲜预订", "海鲜预订（当日时价）", "当日时价，下单后店家将与您联系确认", "/uploads/dining_20260918_021.png", []specSeed{{"时价", 0}}},
}

// ============================================================================
// 运行期写入
// ============================================================================

// seedTables 写入桌台,桌台码随机生成(保证不可枚举)。
func seedTables() {
	for _, t := range seedTableRows {
		DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?)`, t.TableNo, t.TableName, t.Capacity, t.Status, t.SortOrder, "0", NewUniqueTableCode(), seedCreateTime, seedTableUpdTime)
	}
}

func seedCategories() {
	for _, c := range seedCategoryRows {
		DB.Exec(`INSERT INTO tb_category(category_name, sort_order, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?)`, c.CategoryName, c.SortOrder, "0", seedSysCreateTime, seedSysCreateTime)
	}
}

func seedDishes() {
	// 分类名 -> id
	catID := map[string]int{}
	rows, _ := DB.Query(`SELECT category_id, category_name FROM tb_category`)
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		catID[name] = id
	}
	rows.Close()

	for i, s := range seedDishRows {
		res, _ := DB.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			catID[s.cat], s.name, s.img, s.desc, 1, i+1, "0", seedSysCreateTime, seedSysCreateTime)
		dishID, _ := res.LastInsertId()
		for _, sp := range s.specs {
			DB.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price, create_time) VALUES(?,?,?,?)`,
				dishID, sp.name, sp.price, seedSysCreateTime)
		}
	}
}

func seedRemarks() {
	for i, r := range seedRemarkRows {
		DB.Exec(`INSERT INTO tb_remark(option_name, sort_order, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?)`, r, i+1, "0", seedSysCreateTime, seedSysCreateTime)
	}
}

func seedPrinters() {
	for _, p := range seedPrinterRows {
		DB.Exec(`INSERT INTO tb_printer(printer_name, printer_type, ip, port, paper_width, status, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?)`, p.Name, p.PType, p.IP, p.Port, p.Width, 0, "0", seedSysCreateTime, seedSysCreateTime)
	}
}
