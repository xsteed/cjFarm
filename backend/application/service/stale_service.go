package service

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// ============ 桌台遗留订单防护 ============
//
// 未接在线支付时「收款」全靠店家手动点。漏点一次,订单就永远停在
// (1/2/3, pay_status=0),桌台永远占用 —— 下一桌客人扫码会被直接带进上一桌的账单。
//
// 这里提供两道独立防线,各自有开关,由店家按自己的营业习惯决定开不开:
//
//  1. 跨日自动清台(stale_release_enabled):超过一个营业日仍未结账的订单,其桌台
//     自动恢复空闲;顾客端读取、下单防重、桌台释放判定同时按营业日隔离这些旧单。
//     只动桌台状态,不动订单与账务(理由见 ReleaseStaleTables)。
//
//  2. 未结账超时告警(stale_alert_enabled + stale_alert_hours):当天开台超过 N 小时
//     还没收款的桌,在看板上标红提醒。它不改变任何状态,只是把「可能要忘」这件事
//     推到店员眼前 —— 多数漏结单在成为跨日遗留单之前就能被这道提醒拦下来。
//
// 营业日按自然日 00:00 切换(与 biz_date 的既有口径一致,见 customer_service)。
// 通宵营业的店如果嫌切得太早,可以只开告警、不开自动清台。

// 配置键(默认值定义在 store.settingDefaults)。
const (
	SettingStaleReleaseEnabled = "stale_release_enabled"
	SettingStaleAlertEnabled   = "stale_alert_enabled"
	SettingStaleAlertHours     = "stale_alert_hours"
	// SettingBizDateCutoffHour 营业日切换时刻。虽然服务于本模块的判定边界,
	// 但它同时决定「下单时写哪个 biz_date」,因此也归口在下单链路(customer_service)。
	SettingBizDateCutoffHour = "biz_date_cutoff_hour"
)

// 告警阈值的合法区间与默认值(小时)。
// 上限 72:再长就跨到三天前,那已经是「跨日遗留」而非「今天忘了结」。
const (
	StaleAlertHoursMin = 1
	StaleAlertHoursMax = 72
	StaleAlertHoursDef = 4
)

// 营业日切换时刻的合法区间与默认值(小时)。0 表示按自然日 00:00 切换,与历史行为一致。
// 上限 12:再晚就跨进了第二天的午市,那已经不是「同一场营业」了。
const (
	BizDateCutoffMin = 0
	BizDateCutoffMax = 12
	BizDateCutoffDef = 0
)

// BizDayCutoffHour 营业日切换时刻(小时)。缺失/非法一律回落 0(自然日)。
func BizDayCutoffHour() int {
	n, err := strconv.Atoi(strings.TrimSpace(dao.GetSetting(SettingBizDateCutoffHour)))
	if err != nil || n < BizDateCutoffMin || n > BizDateCutoffMax {
		return BizDateCutoffDef
	}
	return n
}

// BizDateOf 计算某一时刻属于哪个营业日('YYYY-MM-DD')。
//
// 切换时刻之前算作前一天的营业日:cutoff=4 时,10-14 的 02:00 仍属于 10-13 这一场营业。
// 抽成纯函数,便于单测与复用,也避免各处各写一份 time.Add。
func BizDateOf(t time.Time, cutoffHour int) string {
	return t.Add(-time.Duration(cutoffHour) * time.Hour).Format("2006-01-02")
}

// CurrentBizDate 当前营业日。下单写 biz_date、跨日清台判定、超时告警归属都走它,
// 保证同一时刻全系统对「今天是哪个营业日」只有一种答案。
func CurrentBizDate() string {
	return BizDateOf(time.Now(), BizDayCutoffHour())
}

// NormalizeBizDateCutoffHour 归一化切换时刻:非法或越界回落默认值。
func NormalizeBizDateCutoffHour(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < BizDateCutoffMin || n > BizDateCutoffMax {
		logger.Warnf("[stale] 营业日切换时刻 %q 非法(应在 %d~%d 之间),已回落默认值 %d",
			v, BizDateCutoffMin, BizDateCutoffMax, BizDateCutoffDef)
		return strconv.Itoa(BizDateCutoffDef)
	}
	return strconv.Itoa(n)
}

// staleGuardInterval 跨日清台的巡检间隔。
//
// 清台本身不紧急:顾客端读取时已经实时隔离旧单(见 StaleGuardBizDate),不会串单,
// 巡检只负责把看板上的占用状态归位。10 分钟足够,再短只是徒增查询。
const staleGuardInterval = 10 * time.Minute

// StaleReleaseEnabled 跨日自动清台是否开启(默认关)。
func StaleReleaseEnabled() bool { return SettingFlag(SettingStaleReleaseEnabled) == "1" }

// StaleAlertEnabled 未结账超时告警是否开启(默认关)。
func StaleAlertEnabled() bool { return SettingFlag(SettingStaleAlertEnabled) == "1" }

// StaleAlertHours 超时告警阈值(小时)。缺失/非法值一律回落默认值:
// 这个值直接决定「多久算超时」,读到一个脏值就静默变成 0 会让看板瞬间刷满告警。
func StaleAlertHours() int {
	n, err := strconv.Atoi(strings.TrimSpace(dao.GetSetting(SettingStaleAlertHours)))
	if err != nil || n < StaleAlertHoursMin || n > StaleAlertHoursMax {
		return StaleAlertHoursDef
	}
	return n
}

// StaleGuardBizDate 返回当前生效的「营业日隔离值」。
//
// 跨日清台开关开启时返回今天的营业日(即隔离更早的订单),关闭时返回空串(不过滤)。
// 顾客端扫码、下单防重、桌台释放判定全部走它 —— 同一时刻全系统口径必须一致:
// 只隔离一半的后果是「新客人看不见旧单,但新单结账时桌台释放不掉」。
//
// 每次调用读一次配置:配置改动能立即生效,不必等重启。扫码路径本就要查几次库,
// 多这一次读可忽略。
func StaleGuardBizDate() string {
	if !StaleReleaseEnabled() {
		return ""
	}
	return CurrentBizDate()
}

// StaleReleaseOutcome 一次跨日清台的结果。
type StaleReleaseOutcome struct {
	Tables int // 被恢复空闲的桌台数
	Orders int // 这些桌台上遗留的订单数(仅统计,不修改)
}

// ReleaseStaleTables 跨日自动清台:把「上一营业日及更早仍有进行中订单」的桌台恢复空闲。
//
// 只动桌台状态,订单状态与账务一律不碰。遗留单到底收没收钱、该补结账还是作废,只有
// 店家知道:自动补结账会虚增营收,自动作废会漏记收入,两个方向的错都比「桌上留一张
// 待处理的单」更贵。订单照旧留在订单管理里,店家可正常结账或取消。
//
// 清台后配合 StaleGuardBizDate 的读取隔离,顾客扫码不会再被带进那些旧单。
func ReleaseStaleTables(ctx context.Context) (StaleReleaseOutcome, error) {
	var out StaleReleaseOutcome
	today := CurrentBizDate()
	tableIDs := dao.ListStaleTableIDs(today)
	if len(tableIDs) == 0 {
		return out, nil
	}
	for _, id := range tableIDs {
		// 事务外快照仅用于日志计数(该桌遗留了几笔单);清台判定以事务内为准。
		orders := dao.ListActiveOrdersByTable(id, "")
		stale := 0
		for _, o := range orders {
			// biz_date 为空的行无从判定营业日,保守当作「该桌还有人」:宁可让店家人工处理,
			// 也不能把一张正在用餐的桌判成空闲,否则下一桌客人坐进去就成了两拨人一桌。
			if o.BizDate == "" || o.BizDate >= today {
				break
			}
			stale++
		}
		// 一桌多单场景下旧单与新单可以并存:该桌还有本营业日的订单就不清。
		if stale == 0 || len(orders) > stale {
			continue
		}
		// 已经空闲的桌台直接跳过(仅优化:值没变也计数会让每轮巡检都报告同样的
		// 清台动作,日志被「清台 N 张」刷屏却看不出哪一次才是真变化)。
		if cur, err := dao.GetTableStatus(id); err != nil || cur == 0 {
			continue
		}
		// 「判断 + 写入」在同一事务内原子完成:先对桌台行做自赋值 UPDATE 取行写锁
		// (TouchTableTx),把并发的顾客下单(OccupyTableTx 同样要拿这行的写锁)挡在
		// 锁外;再数本营业日的进行中订单,为 0 才置空闲。旧实现在事务外
		// 「读快照 → 内存判定 → 无条件置空闲」,窗口期内顾客扫码开的新单会被随后
		// 的置空闲抹成「空闲桌」:看板显示空闲而桌上挂着未结账单。
		if err := store.WithTx(func(tx *sql.Tx) error {
			if err := dao.TouchTableTx(tx, id); err != nil {
				return err
			}
			// 带营业日隔离(biz_date 为空按未隔离处理,与上方快照口径一致):
			// 跨日遗留单不算「还在出餐」,否则桌台永远释放不掉。
			n, err := dao.CountUnfinishedOrdersTx(tx, id, today)
			if err != nil {
				return err
			}
			if n > 0 {
				return nil // 该桌还有本营业日的进行中订单,不清
			}
			return dao.SetTableStatusTx(tx, id, 0)
		}); err != nil {
			return out, err
		}
		out.Tables++
		out.Orders += stale
		logger.CtxInfof(ctx, "[stale] 跨日清台:桌台 %d 恢复空闲(遗留订单 %d 笔未改动,请在订单管理中补结账或作废)", id, stale)
	}
	return out, nil
}

// ListStaleOrderAlerts 未结账待处理清单:当天开台超时的 + 跨日遗留的。
//
// 两类放在同一个列表里,因为对店家来说要做的动作是同一件事 —— 去把那桌的单结掉。
// 拆成两个入口只会让其中一类被漏看,而跨日遗留那类恰恰最容易被忘:桌台已经空闲,
// 看板上什么都看不到,订单却还挂在「进行中」。
// 两类内容各归各的开关管,不是一刀切:
//   - 当天超时唤醒「未结账超时告警」;
//   - 跨日遗留由「跨日自动清台」负责 —— 它正是清台的直接后果(桌台被恢复空闲、
//     订单沉进列表深处)。只开清台不开告警时,这条提醒必须照常出现;否则清台就等于
//     把账悄悄藏了起来。
func ListStaleOrderAlerts() dto.StaleOrderAlerts {
	alertOn, releaseOn := StaleAlertEnabled(), StaleReleaseEnabled()
	res := dto.StaleOrderAlerts{
		Enabled: alertOn || releaseOn,
		Hours:   StaleAlertHours(),
		Items:   []dto.StaleOrderAlert{},
	}
	now := time.Now()
	bizDate := CurrentBizDate()
	// 当天开台超时(按阈值)排前面:这些桌还占着台,客人可能还没走,最急。
	if alertOn {
		before := now.Add(-time.Duration(res.Hours) * time.Hour).Format(conf.TimeLayout)
		for _, o := range dao.ListStaleUnpaidOrders(bizDate, before) {
			res.Items = append(res.Items, staleAlertOf(o, now, false))
		}
	}
	// 跨日遗留排后面:桌台已被清空,只剩账要了结,不致于火烧眉毛。
	if releaseOn {
		for _, o := range dao.ListLegacyUnpaidOrders(bizDate) {
			res.Items = append(res.Items, staleAlertOf(o, now, true))
		}
	}
	return res
}

// staleAlertOf 把订单转成提醒项;legacy 标记是否属于跨日遗留。
func staleAlertOf(o po.Order, now time.Time, legacy bool) dto.StaleOrderAlert {
	item := dto.StaleOrderAlert{
		OrderID:     o.OrderID,
		OrderNo:     o.OrderNo,
		ShortNo:     po.DisplayNo(o.PickupNo, o.OrderNo),
		TableID:     o.TableID,
		TableNo:     o.TableNo,
		TableName:   o.TableName,
		OrderStatus: o.OrderStatus,
		PersonCount: o.PersonCount,
		CreateTime:  o.CreateTime,
		TotalAmount: po.ToYuan(o.TotalAmount),
		Legacy:      legacy,
	}
	if t, err := time.ParseInLocation(conf.TimeLayout, o.CreateTime, time.Local); err == nil {
		item.Minutes = int(now.Sub(t).Minutes())
	}
	return item
}

// StartStaleOrderGuard 启动后台巡检:按配置开关执行跨日自动清台。
// 照 print.StartAgentCleanup 的既有范本(单轮 panic 只跳过本轮,不让协程退出)。
func StartStaleOrderGuard() {
	go func() {
		for {
			// 先干一轮再睡:进程刚起来时正该把停机期间累积的遗留单处理掉,
			// 先睡一个周期会让第一张桌白等 10 分钟(跨日后的重启尤其明显)。
			runStaleGuardOnce()
			time.Sleep(staleGuardInterval)
		}
	}()
}

// runStaleGuardOnce 执行一轮清台巡检。开关关闭时直接返回,不做任何查询。
func runStaleGuardOnce() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[stale] 跨日清台单轮 panic(已跳过本轮): %v", r)
		}
	}()
	if !StaleReleaseEnabled() {
		return
	}
	out, err := ReleaseStaleTables(context.Background())
	if err != nil {
		logger.Warnf("[stale] 跨日清台失败: %v", err)
		return
	}
	if out.Tables > 0 {
		logger.Infof("[stale] 跨日清台完成:释放桌台 %d 张,涉及遗留订单 %d 笔(订单未改动)", out.Tables, out.Orders)
	}
}

// NormalizeStaleAlertHours 归一化告警阈值:非法或越界一律回落默认值。
//
// 与 normEnumValue 同风格 —— 保存接口是整表提交,不该因为一个数字越界就整份拒绝,
// 但库里必须永远留着可用的合法值(读到脏值会让看板立刻刷满或一条不报)。
func NormalizeStaleAlertHours(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < StaleAlertHoursMin || n > StaleAlertHoursMax {
		logger.Warnf("[stale] 超时告警小时数 %q 非法(应在 %d~%d 之间),已回落默认值 %d",
			v, StaleAlertHoursMin, StaleAlertHoursMax, StaleAlertHoursDef)
		return strconv.Itoa(StaleAlertHoursDef)
	}
	return strconv.Itoa(n)
}
