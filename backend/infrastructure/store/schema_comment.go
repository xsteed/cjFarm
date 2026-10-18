package store

import (
	"regexp"
	"strings"
)

// ============================================================================
// 字段注释(MySQL COMMENT)
// ============================================================================
//
// 规范:migrations/full/mysql/schema.sql 的每张表、每一列都要带 COMMENT,
// 让 DBA / 运维在 SHOW CREATE TABLE 里直接看懂字段含义,不必翻代码与文档。
//
// SQLite 没有列注释语法(DATABASE 大小写无关,sqlite_master 也不存注释),
// 因此注释只在 MySQL 方言下生成;SQLite 侧的字段说明由 schemaTemplate 里的
// `--` 行内注释与 migrations/README.md 承担(见 README 第七节「字段注释规范」)。
//
// 维护方式:注释以 表名.列名 为键登记在下面两张 map 里,由 schemaStatements
// 在 MySQL 分支注入;新增表/列时必须同步补注释 —— sqlgen_test.go 的
// TestSQLCommentsCoverEveryColumn 会对全量建表语句强制校验覆盖率。
// 注意:注释文本里不要出现英文单引号(生成时会转义,但规范上直接避免)。

// tableComments 表级注释(MySQL 建表尾 COMMENT='…')。
var tableComments = map[string]string{
	"tb_table":             "桌台",
	"tb_category":          "菜品分类",
	"tb_dish":              "菜品",
	"tb_spec":              "菜品规格",
	"tb_remark":            "备注常用语",
	"tb_printer":           "打印机",
	"tb_print_log":         "打印台账(每次向打印机发单的留痕)",
	"tb_print_job":         "本地打印代理任务队列",
	"tb_print_agent":       "本地打印代理身份",
	"tb_oper_log":          "操作日志(审计留痕)",
	"tb_config":            "系统配置(键值对)",
	"tb_day_seq":           "营业日取餐号计数器",
	"tb_order":             "订单主表",
	"tb_order_item":        "订单明细",
	"tb_order_urge":        "催菜记录",
	"tb_payment":           "支付流水",
	"tb_refund":            "退款流水",
	"tb_role":              "角色(权限点集合)",
	"tb_user":              "员工账号",
	"tb_customer":          "会员档案(账号与登录方式解耦)",
	"tb_customer_identity": "会员登录方式",
	"tb_wallet":            "会员储值账户",
	"tb_wallet_txn":        "储值流水(只增不改)",
	"tb_recharge":          "储值充值单",
	"tb_remember_token":    "记住登录令牌",
	"tb_image":             "图片内容(菜品图/收款码随库走)",
	"tb_visit_log":         "访问明细(待办性质,按保留期清理)",
	"tb_visitor":           "访客注册表(新访客精确口径)",
	"tb_visit_daily":       "访问日聚合台账(报表主读)",
}

// columnComments 列级注释,key 为「表名.列名」。
var columnComments = map[string]string{
	// ---- 通用列(各表语义一致) ----
	"create_time": "创建时间(数据库自动填)",
	"update_time": "更新时间(行更新自动推进)",

	// ---- tb_table 桌台 ----
	"tb_table.table_id":    "桌台ID(自增主键)",
	"tb_table.table_no":    "桌号,如 A1",
	"tb_table.table_name":  "桌台名称",
	"tb_table.capacity":    "座位数",
	"tb_table.status":      "占用状态 0空闲 1占用(订单自动流转,勿手工置位)",
	"tb_table.lock_status": "人工可用性 0正常 1停用 2预约(与status正交)",
	"tb_table.lock_remark": "停用原因或预约人/时段",
	"tb_table.sort_order":  "排序值(小在前)",
	"tb_table.del_flag":    "软删除标记 0正常 1已删除",
	"tb_table.create_by":   "创建人",
	"tb_table.update_by":   "更新人",
	"tb_table.remark":      "备注",
	"tb_table.table_code":  "桌台码(二维码内容,生成后不变,防自增ID被枚举)",

	// ---- tb_category 菜品分类 ----
	"tb_category.category_id":   "分类ID(自增主键)",
	"tb_category.category_name": "分类名称",
	"tb_category.sort_order":    "排序值(小在前)",
	"tb_category.del_flag":      "软删除标记 0正常 1已删除",

	// ---- tb_dish 菜品 ----
	"tb_dish.dish_id":     "菜品ID(自增主键)",
	"tb_dish.category_id": "所属分类ID",
	"tb_dish.dish_name":   "菜品名称",
	"tb_dish.dish_image":  "图片路径(/uploads/ 下,内容存 tb_image)",
	"tb_dish.description": "菜品描述",
	"tb_dish.status":      "上架状态 1上架 0下架",
	"tb_dish.sold_out":    "售罄标记 1售罄 0正常(人工标记)",
	"tb_dish.signature":   "招牌菜标记 1招牌 0普通",
	"tb_dish.sort_order":  "排序值(小在前)",
	"tb_dish.del_flag":    "软删除标记 0正常 1已删除",
	"tb_dish.create_by":   "创建人",
	"tb_dish.update_by":   "更新人",
	"tb_dish.remark":      "备注",

	// ---- tb_spec 菜品规格 ----
	"tb_spec.spec_id":   "规格ID(自增主键)",
	"tb_spec.dish_id":   "所属菜品ID",
	"tb_spec.spec_name": "规格名称",
	"tb_spec.price":     "售价(分)",
	"tb_spec.stock":     "库存 -1不限量 其余为剩余份数(触发自动售罄)",

	// ---- tb_remark 备注常用语 ----
	"tb_remark.remark_id":   "备注ID(自增主键)",
	"tb_remark.option_name": "备注常用语文案",
	"tb_remark.sort_order":  "排序值(小在前)",
	"tb_remark.del_flag":    "软删除标记 0正常 1已删除",

	// ---- tb_printer 打印机 ----
	"tb_printer.printer_id":   "打印机ID(自增主键)",
	"tb_printer.printer_name": "打印机名称",
	"tb_printer.printer_type": "票种 1厨房单 2顾客小票",
	"tb_printer.provider":     "接入方式 tcp直连 feie飞鹅云 agent本地代理",
	"tb_printer.ip":           "打印机IP(agent通道为门店内网地址)",
	"tb_printer.port":         "打印机端口(默认9100)",
	"tb_printer.feie_sn":      "飞鹅云终端SN(provider=feie时必填)",
	"tb_printer.paper_width":  "纸宽 32=58mm 48=80mm",
	"tb_printer.copies":       "打印份数(1-5)",
	"tb_printer.category_ids": "按分类分单的CSV分类ID(空=收全部菜品)",
	"tb_printer.status":       "启用状态 1启用 0停用",
	"tb_printer.del_flag":     "软删除标记 0正常 1已删除",

	// ---- tb_print_log 打印台账 ----
	"tb_print_log.print_id":     "日志ID(自增主键)",
	"tb_print_log.order_id":     "关联订单ID",
	"tb_print_log.order_no":     "关联订单号",
	"tb_print_log.table_no":     "桌号快照",
	"tb_print_log.table_name":   "桌名快照",
	"tb_print_log.printer_id":   "打印机ID",
	"tb_print_log.printer_name": "打印机名称快照",
	"tb_print_log.printer_type": "票种 1厨房单 2顾客小票",
	"tb_print_log.provider":     "发送通道 tcp/feie/agent",
	"tb_print_log.doc_type":     "票据类型 kitchen厨房单 guest顾客小票 guest_append加菜单",
	"tb_print_log.copies":       "打印份数",
	"tb_print_log.status":       "结果 0失败 1已送出 2已入队(仅agent通道)",
	"tb_print_log.remote_id":    "飞鹅云订单号(追问真实出纸状态用)",
	"tb_print_log.label":        "票据标识,如【厨房单】1/4(拆单场景区分丢的是哪张)",
	"tb_print_log.detail":       "发送详情/失败原因(代理通道会整段改写)",
	"tb_print_log.content":      "票面快照(JSON文本行,预览与补打回放用)",
	"tb_print_log.paper_width":  "快照纸宽 32=58mm 48=80mm",
	"tb_print_log.trigger_by":   "触发场景 order下单 append加菜 settle结账 test测试 reprint补打",
	"tb_print_log.operator":     "操作人",
	"tb_print_log.cost_ms":      "发送耗时(毫秒)",

	// ---- tb_print_job 打印任务队列 ----
	"tb_print_job.job_id":        "任务ID(自增主键)",
	"tb_print_job.printer_id":    "目标打印机ID",
	"tb_print_job.printer_name":  "打印机名称快照",
	"tb_print_job.printer_type":  "票种 1厨房单 2顾客小票",
	"tb_print_job.ip":            "打印机门店内网IP(代理直发用)",
	"tb_print_job.port":          "打印机端口",
	"tb_print_job.doc_type":      "票据类型(同 tb_print_log.doc_type)",
	"tb_print_job.order_id":      "关联订单ID",
	"tb_print_job.order_no":      "关联订单号",
	"tb_print_job.table_no":      "桌号快照",
	"tb_print_job.copies":        "打印份数",
	"tb_print_job.print_log_id":  "关联打印台账ID",
	"tb_print_job.delivery_id":   "投递批次ID(代理回执对账用)",
	"tb_print_job.payload":       "票据等宽文本行(换行分隔,取单时才转ESC/POS)",
	"tb_print_job.status":        "0待取单 1已取单(租约) 2已送出 3已放弃",
	"tb_print_job.attempts":      "已尝试次数(上限3)",
	"tb_print_job.last_error":    "最近一次失败原因",
	"tb_print_job.claimed_by":    "取单代理标识",
	"tb_print_job.claim_time":    "取单时间(租约起点)",
	"tb_print_job.next_try_time": "下次可取单时间",
	"tb_print_job.trigger_by":    "触发场景(同 tb_print_log.trigger_by)",
	"tb_print_job.operator":      "操作人",
	"tb_print_job.done_time":     "送达时间",

	// ---- tb_print_agent 打印代理身份 ----
	"tb_print_agent.agent_id":    "代理ID(自增主键)",
	"tb_print_agent.agent_name":  "代理名称",
	"tb_print_agent.token_hash":  "令牌SHA-256(不存明文)",
	"tb_print_agent.token_hint":  "令牌前8位(识别用)",
	"tb_print_agent.printer_ids": "授权打印机ID(CSV)",
	"tb_print_agent.status":      "1启用 0停用",
	"tb_print_agent.last_seen":   "最近心跳时间",
	"tb_print_agent.last_report": "最近上报状态摘要",

	// ---- tb_oper_log 操作日志 ----
	"tb_oper_log.log_id":        "日志ID(自增主键)",
	"tb_oper_log.module":        "模块中文名,如 订单管理",
	"tb_oper_log.business_type": "操作类型 insert/update/delete/login/grant/print/other",
	"tb_oper_log.action":        "动作中文名,如 修改菜品",
	"tb_oper_log.method":        "HTTP方法",
	"tb_oper_log.request_url":   "请求URL",
	"tb_oper_log.operator_id":   "操作人ID",
	"tb_oper_log.operator":      "操作人姓名快照(改名不回溯)",
	"tb_oper_log.operator_role": "操作人角色快照",
	"tb_oper_log.oper_ip":       "操作IP",
	"tb_oper_log.target_type":   "对象类型",
	"tb_oper_log.target_id":     "对象ID",
	"tb_oper_log.oper_param":    "请求参数(脱敏后)",
	"tb_oper_log.detail":        "结果详情",
	"tb_oper_log.status":        "结果 0失败 1成功",
	"tb_oper_log.error_msg":     "失败原因",
	"tb_oper_log.cost_ms":       "耗时(毫秒)",

	// ---- tb_config 系统配置 ----
	"tb_config.cfg_key":   "配置键(主键)",
	"tb_config.cfg_value": "配置值(敏感项AES-256-GCM密文)",

	// ---- tb_day_seq 取餐号计数器 ----
	"tb_day_seq.biz_date": "营业日YYYY-MM-DD(主键)",
	"tb_day_seq.next_val": "当日下一个取餐号",

	// ---- tb_order 订单主表 ----
	"tb_order.order_id":           "订单ID(自增主键)",
	"tb_order.order_no":           "订单号(D开头随机串,不可枚举)",
	"tb_order.table_id":           "桌台ID",
	"tb_order.table_no":           "桌号快照",
	"tb_order.table_name":         "桌名快照",
	"tb_order.person_count":       "用餐人数",
	"tb_order.order_status":       "状态 1已下单 2制作中 3已上齐 4已完成 5已取消",
	"tb_order.dish_amount":        "菜品金额(分)",
	"tb_order.seat_fee":           "座位费(分)",
	"tb_order.discount_amount":    "优惠金额(分)",
	"tb_order.total_amount":       "应收总额(分)",
	"tb_order.pay_status":         "支付状态 0未支付 1已支付",
	"tb_order.pay_type":           "支付方式(预留)",
	"tb_order.pay_time":           "支付时间",
	"tb_order.transaction_id":     "第三方渠道交易号",
	"tb_order.pay_channel":        "支付渠道 wxpay/alipay/offline(码牌)/空",
	"tb_order.refund_amount":      "累计退款金额(分)",
	"tb_order.refund_time":        "最近退款时间",
	"tb_order.finish_time":        "完成/取消时间",
	"tb_order.order_remark":       "顾客订单备注",
	"tb_order.cancel_reason":      "取消原因",
	"tb_order.create_by":          "创建人",
	"tb_order.update_by":          "更新人",
	"tb_order.remark":             "商家备注",
	"tb_order.settle_type":        "结算方式 normal正常 free免单 credit挂账",
	"tb_order.settle_time":        "结算时间",
	"tb_order.settle_operator":    "结算操作人",
	"tb_order.settle_remark":      "结算备注",
	"tb_order.credit_status":      "挂账状态 0非挂账 1待收款 2已结清",
	"tb_order.credit_amount":      "挂账金额(分)",
	"tb_order.credit_settle_time": "挂账核销时间",
	"tb_order.credit_settle_by":   "挂账核销人",
	"tb_order.paid_amount":        "实收金额(分,免单=0)",
	"tb_order.gift_amount":        "赠送金额(分,让利口径不计应收)",
	"tb_order.customer_id":        "下单会员ID(0=匿名;以顾客令牌为准防伪造)",
	"tb_order.pre_settle_status":  "结账前状态快照(0=未记录,撤销结算时恢复用)",
	"tb_order.biz_date":           "营业日YYYY-MM-DD(NULL不参与取餐号唯一判定)",
	"tb_order.pickup_no":          "当日取餐号(顾客报号/厨房叫号)",
	"tb_order.meal_start_time":    "用餐开始时间(下单时写入)",
	"tb_order.meal_end_time":      "用餐结束时间(订单到终态时写入)",
	"tb_order.viewed_at":          "商家首次操作时间(NULL=新单,只写一次)",

	// ---- tb_order_item 订单明细 ----
	"tb_order_item.item_id":           "明细ID(自增主键)",
	"tb_order_item.order_id":          "所属订单ID",
	"tb_order_item.dish_id":           "菜品ID",
	"tb_order_item.dish_name":         "菜名快照(菜品后改不回溯)",
	"tb_order_item.spec_id":           "规格ID",
	"tb_order_item.spec_name":         "规格名快照",
	"tb_order_item.price":             "下单时单价(分)",
	"tb_order_item.quantity":          "数量",
	"tb_order_item.amount":            "小计(分)",
	"tb_order_item.remark":            "单品备注",
	"tb_order_item.batch_no":          "加菜批次(1=首次下单,每次加菜+1)",
	"tb_order_item.client_request_id": "幂等键(同次下单/加菜提交共享,去重用)",
	"tb_order_item.is_gift":           "赠送标记 1赠送(amount记0,原价计入gift_amount)",
	"tb_order_item.gift_reason":       "赠送原因",

	// ---- tb_order_urge 催菜记录 ----
	"tb_order_urge.urge_id":     "记录ID(自增主键)",
	"tb_order_urge.order_id":    "关联订单ID",
	"tb_order_urge.order_no":    "关联订单号",
	"tb_order_urge.table_id":    "桌台ID",
	"tb_order_urge.table_no":    "桌号快照",
	"tb_order_urge.table_name":  "桌名快照",
	"tb_order_urge.urge_type":   "类型 urge=催菜",
	"tb_order_urge.status":      "处理状态 0待处理 1已处理",
	"tb_order_urge.remark":      "处理备注",
	"tb_order_urge.handle_time": "处理时间",
	"tb_order_urge.handle_by":   "处理人",

	// ---- tb_payment 支付流水 ----
	"tb_payment.payment_id":       "支付单ID(自增主键)",
	"tb_payment.order_no":         "关联订单号",
	"tb_payment.channel":          "支付渠道 wxpay/alipay",
	"tb_payment.channel_trade_no": "渠道交易号",
	"tb_payment.amount":           "支付金额(分)",
	"tb_payment.status":           "0待支付 1已支付 2已关闭 3退款中 4已退款 5退款失败",
	"tb_payment.prepay_id":        "微信预支付ID",
	"tb_payment.notify_time":      "回调通知时间",

	// ---- tb_refund 退款流水 ----
	"tb_refund.refund_id":         "退款单ID(自增主键)",
	"tb_refund.order_id":          "关联订单ID",
	"tb_refund.order_no":          "关联订单号",
	"tb_refund.payment_id":        "关联支付单ID",
	"tb_refund.refund_no":         "退款单号",
	"tb_refund.channel":           "支付渠道",
	"tb_refund.channel_refund_no": "渠道退款单号",
	"tb_refund.amount":            "退款金额(分)",
	"tb_refund.status":            "0处理中 1成功 2失败",
	"tb_refund.reason":            "退款原因",
	"tb_refund.operator":          "操作人",
	"tb_refund.fail_reason":       "失败原因",
	"tb_refund.is_duplicate":      "1=重复支付自动退回(不计营收不占退款额度)",

	// ---- tb_role 角色 ----
	"tb_role.role_id":    "角色ID(自增主键)",
	"tb_role.role_key":   "角色标识(全局唯一,含已删除行)",
	"tb_role.role_name":  "角色名称",
	"tb_role.perms":      "权限点CSV(权限矩阵见 permission.go)",
	"tb_role.data_scope": "数据范围(当前仅all)",
	"tb_role.is_builtin": "内置角色标记(启动时校准)",
	"tb_role.sort_order": "排序值(小在前)",
	"tb_role.status":     "1启用 0停用",
	"tb_role.del_flag":   "软删除标记 0正常 1已删除",
	"tb_role.remark":     "备注",

	// ---- tb_user 员工账号 ----
	"tb_user.user_id":         "员工ID(自增主键)",
	"tb_user.username":        "登录名(未删除行内唯一)",
	"tb_user.password_hash":   "密码bcrypt哈希",
	"tb_user.real_name":       "姓名(订单留痕展示用)",
	"tb_user.role_id":         "角色ID",
	"tb_user.phone":           "手机号",
	"tb_user.status":          "1启用 0停用(停用即失去访问权)",
	"tb_user.last_login_time": "最近登录时间",
	"tb_user.last_login_ip":   "最近登录IP",
	"tb_user.login_count":     "登录次数",
	"tb_user.pwd_update_time": "密码修改时间",
	"tb_user.token_version":   "令牌版本(改密/强制下线+1使旧令牌失效)",
	"tb_user.del_flag":        "软删除标记 0正常 1已删除",
	"tb_user.create_by":       "创建人",
	"tb_user.update_by":       "更新人",
	"tb_user.remark":          "备注",

	// ---- tb_customer 会员档案 ----
	"tb_customer.customer_id":     "会员ID(自增主键)",
	"tb_customer.nickname":        "昵称(空时前端显示微信用户)",
	"tb_customer.avatar_url":      "头像路径",
	"tb_customer.status":          "1正常 0停用",
	"tb_customer.order_count":     "累计订单数",
	"tb_customer.last_order_time": "最近下单时间",
	"tb_customer.last_login_time": "最近登录时间",
	"tb_customer.remark":          "备注",

	// ---- tb_customer_identity 会员登录方式 ----
	"tb_customer_identity.identity_id":     "身份ID(自增主键)",
	"tb_customer_identity.customer_id":     "会员ID",
	"tb_customer_identity.provider":        "登录方式 wx_mp/wx_oa/wx_app/phone/password/apple",
	"tb_customer_identity.external_id":     "该方式下的用户标识(如openid),(provider,external_id)唯一",
	"tb_customer_identity.credential":      "登录凭据(预留)",
	"tb_customer_identity.unionid":         "微信unionid(多端归并,非唯一)",
	"tb_customer_identity.last_login_time": "最近登录时间",

	// ---- tb_wallet 储值账户 ----
	"tb_wallet.customer_id":    "会员ID(主键,1:1)",
	"tb_wallet.balance":        "余额(分,只由事务内DAO增减,守卫不为负)",
	"tb_wallet.total_recharge": "累计充值(分)",
	"tb_wallet.total_consume":  "累计消费(分)",
	"tb_wallet.total_gift":     "累计赠送(分)",

	// ---- tb_wallet_txn 储值流水 ----
	"tb_wallet_txn.txn_id":        "流水ID(自增主键)",
	"tb_wallet_txn.customer_id":   "会员ID",
	"tb_wallet_txn.biz_type":      "业务类型 recharge/consume/refund/gift/adjust",
	"tb_wallet_txn.amount":        "变动金额(分,带符号,正入负出)",
	"tb_wallet_txn.balance_after": "变动后余额(分,逐行累加必须等于当前余额)",
	"tb_wallet_txn.order_no":      "关联订单号",
	"tb_wallet_txn.payment_no":    "关联支付单号",
	"tb_wallet_txn.reason":        "变动原因",
	"tb_wallet_txn.operator":      "后台操作人(顾客自助操作为空)",

	// ---- tb_recharge 充值单 ----
	"tb_recharge.recharge_id":      "充值单ID(自增主键)",
	"tb_recharge.recharge_no":      "充值单号(R开头,与订单D开头靠前缀分流回调)",
	"tb_recharge.customer_id":      "会员ID",
	"tb_recharge.amount":           "充值金额(分)",
	"tb_recharge.status":           "0待支付 1已入账(回调条件更新做幂等)",
	"tb_recharge.channel":          "支付渠道",
	"tb_recharge.channel_trade_no": "渠道交易号",
	"tb_recharge.pay_time":         "支付时间",

	// ---- tb_remember_token 记住登录令牌 ----
	"tb_remember_token.token_id":       "令牌ID(自增主键)",
	"tb_remember_token.user_id":        "员工ID",
	"tb_remember_token.token":          "令牌SHA-256(库内不落明文,查库裁决过期)",
	"tb_remember_token.token_prefix":   "令牌前8位(设备管理页识别)",
	"tb_remember_token.expire_time":    "过期时间(7/30天免登录)",
	"tb_remember_token.last_used_time": "最近使用时间",
	"tb_remember_token.ua":             "环境指纹(User-Agent)",

	// ---- tb_image 图片内容 ----
	"tb_image.img_id":       "图片ID(自增主键)",
	"tb_image.img_name":     "文件名(唯一,业务表以 /uploads/ 路径引用)",
	"tb_image.content_type": "MIME类型",
	"tb_image.img_data":     "图片二进制(单张上限64MB)",
	"tb_image.file_size":    "字节数",

	// ---- tb_visit_log 访问明细 ----
	"tb_visit_log.visit_id":    "明细ID(自增主键)",
	"tb_visit_log.visit_date":  "访问日期YYYY-MM-DD",
	"tb_visit_log.channel":     "渠道 h5/miniprogram/admin",
	"tb_visit_log.visitor_key": "访客去重键 m:会员 c:前端ID a:匿名哈希",
	"tb_visit_log.is_member":   "是否会员 1是 0否",
	"tb_visit_log.customer_id": "会员ID",
	"tb_visit_log.entry":       "入口标识(H5为桌台码,小程序一期留空)",
	"tb_visit_log.ip_hash":     "IP哈希前32位(UV去重用,不可反推)",
	"tb_visit_log.ua_hash":     "UA哈希前32位(UV去重用,不可反推)",
	"tb_visit_log.ip":          "明文IP(排查展示用,随明细90天清理)",
	"tb_visit_log.city":        "城市(ip2region离线库解析,缺文件为空)",
	"tb_visit_log.os":          "操作系统(归一展示值,小程序直传最准)",
	"tb_visit_log.browser":     "浏览器(归一展示值)",
	"tb_visit_log.device":      "设备类型(手机/平板/PC)",
	"tb_visit_log.model":       "机型(安卓UA可解出型号,iOS为空)",

	// ---- tb_visitor 访客注册表 ----
	"tb_visitor.visitor_key":      "访客键(主键,天然去重)",
	"tb_visitor.first_visit_date": "首访日期(等于统计日即当日新访客)",
	"tb_visitor.last_visit_date":  "最近访问日期",
	"tb_visitor.visit_count":      "累计访问次数",
	"tb_visitor.is_member":        "是否会员 1是 0否",
	"tb_visitor.customer_id":      "会员ID",

	// ---- tb_visit_daily 访问日聚合 ----
	"tb_visit_daily.id":         "行ID(自增主键)",
	"tb_visit_daily.visit_date": "统计日期YYYY-MM-DD",
	"tb_visit_daily.channel":    "渠道(all=跨渠道去重的全站合计行)",
	"tb_visit_daily.pv":         "浏览量(PV)",
	"tb_visit_daily.uv":         "独立访客数(UV)",
	"tb_visit_daily.new_uv":     "新访客数",
	"tb_visit_daily.member_uv":  "会员访客数",
}

// createTableRe 从建表语句首行取表名。
var createTableRe = regexp.MustCompile(`^CREATE TABLE IF NOT EXISTS (\w+) \(`)

// columnComment 按「表名.列名 → 列名」两级查找注释:先精确到表,
// 未登记时回落到通用列(如 create_time / update_time,各表语义一致)。
// 生成(applySQLComments)与覆盖率测试共用,避免两处查找规则漂移。
func columnComment(table, col string) (string, bool) {
	if c, ok := columnComments[table+"."+col]; ok {
		return c, true
	}
	c, ok := columnComments[col]
	return c, ok
}

// applySQLComments 给单条(已展开方言占位符的)MySQL 建表语句注入表/列注释。
// 未登记注释的列保持原样 —— 覆盖率由 TestSQLCommentsCoverEveryColumn 强制。
// 幂等:已带 COMMENT 的行不重复处理,可安全重复调用。
func applySQLComments(stmt string) string {
	m := createTableRe.FindStringSubmatch(stmt)
	if m == nil {
		return stmt
	}
	table := m[1]
	lines := strings.Split(stmt, "\n")
	for i, line := range lines {
		if strings.Contains(line, "COMMENT") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, ")") {
			continue // 注释行、语句结尾行不处理
		}
		fields := strings.Fields(trimmed)
		cmt, ok := columnComment(table, fields[0])
		if !ok {
			continue
		}
		cmt = " COMMENT '" + escapeSQLComment(cmt) + "'"
		if strings.HasSuffix(trimmed, ",") {
			idx := strings.LastIndex(line, ",")
			lines[i] = line[:idx] + cmt + ","
		} else {
			lines[i] = line + cmt
		}
	}
	out := strings.Join(lines, "\n")
	if tc := tableComments[table]; tc != "" {
		out += " COMMENT='" + escapeSQLComment(tc) + "'"
	}
	return out
}

// escapeSQLComment 转义注释文本里的单引号与反斜杠(MySQL 字面量转义规则)。
func escapeSQLComment(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "'", "''")
}
