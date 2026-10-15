package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// ============ 桌台 ============
//
// 桌台域业务逻辑:桌台管理、稳定桌台码兜底补发、状态占用。
// handler 只负责参数解析与响应组装。

// TableList 分页查询桌台列表,并对缺失稳定码的历史数据即时补发。
// 占用中的桌台附带用餐耗时(openTime/diningMinutes),一次 ListActiveOrders 覆盖
// 全部桌台,不引入 N+1;耗时统计失败不阻断桌台列表本身。
func TableList(keyword string, pageNum, pageSize int) (int, []dto.Table, error) {
	total, rows, err := dao.ListTables(dao.TableQuery{Keyword: keyword}, pageNum, pageSize)
	if err != nil {
		return 0, nil, err
	}
	dining := tableDiningByTable()
	out := make([]dto.Table, 0, len(rows))
	for _, t := range rows {
		// 兜底:历史数据若缺稳定码则即时补发,保证二维码可生成。
		if t.TableCode == "" {
			if code, err := store.EnsureTableCode(t.TableID); err == nil {
				t.TableCode = code
			}
		}
		item := dto.FromTable(t)
		if d, ok := dining[t.TableID]; ok {
			item.OpenTime = d.openTime
			item.DiningMinutes = d.minutes
		}
		out = append(out, item)
	}
	return total, out, nil
}

// tableDining 某桌当前的用餐耗时快照:openTime 为该桌最早一笔进行中订单的开台时间,
// minutes 为距现在的分钟数。
type tableDining struct {
	openTime string
	minutes  int
}

// tableDiningByTable 统计每桌已用餐分钟数。
// 开台口径与「未结账超时告警」一致(stale_service 的 staleAlertOf):取订单
// CreateTime,换算 now-CreateTime。一桌可有多笔进行中订单(付款后再开新单),
// 取最早一笔 —— 客人坐下的时刻才是用餐起点。
// guardBizDate 带营业日隔离:跨日遗留单不算正在用餐,与桌台占用状态同口径,
// 避免给已被自动清台、只剩遗留账的桌台显示几十小时的离谱时长。
func tableDiningByTable() map[int]tableDining {
	out := map[int]tableDining{}
	orders, err := dao.ListActiveOrders(StaleGuardBizDate())
	if err != nil {
		// 查询失败只损失耗时展示,桌台列表照常返回。
		logger.CtxWarnf(context.Background(), "[table] 统计桌台用餐耗时失败: %v", err)
		return out
	}
	now := time.Now()
	for _, o := range orders {
		// 起点优先取落库的用餐开始时间(下单写入),历史行回落到 create_time。
		startAt := o.CreateTime
		if o.MealStartTime != nil && *o.MealStartTime != "" {
			startAt = *o.MealStartTime
		}
		d, ok := out[o.TableID]
		// 时间均为 conf.TimeLayout 同格式字符串,可直接按字典序取最早。
		if ok && startAt >= d.openTime {
			continue
		}
		d = tableDining{openTime: startAt}
		if t, err := time.ParseInLocation(conf.TimeLayout, startAt, time.Local); err == nil {
			d.minutes = int(now.Sub(t).Minutes())
		}
		out[o.TableID] = d
	}
	return out
}

// SaveTable 校验并新增桌台,返回桌台 ID;插入后补发稳定桌台码。
func SaveTable(ctx context.Context, t dto.Table) (int64, error) {
	if strings.TrimSpace(t.TableNo) == "" || strings.TrimSpace(t.TableName) == "" {
		return 0, errors.New("请填写桌号和桌台名称")
	}
	id, err := dao.InsertTable(t.ToPO())
	if err != nil {
		return 0, err
	}
	// 兜底:若随机码插入冲突(唯一索引),重试补发,确保新桌台必有稳定码。
	if _, err := store.EnsureTableCode(int(id)); err != nil {
		logger.CtxWarnf(ctx, "[table] 补发桌台稳定码失败(table %d): %v", id, err)
	}
	return id, nil
}

// UpdateTable 校验并更新桌台基础信息(桌号/桌名/容纳人数/排序)。
// 占用状态与人工可用性状态均不在此处修改:前者由订单流转维护,后者走 SetTableLock。
func UpdateTable(t dto.Table) error {
	if strings.TrimSpace(t.TableNo) == "" || strings.TrimSpace(t.TableName) == "" {
		return errors.New("请填写桌号和桌台名称")
	}
	return dao.UpdateTable(t.ToPO())
}

// SetTableLock 设置桌台人工可用性状态:0 正常 / 1 停用(不可用) / 2 预约。
//
// 「停用」表示该桌无法接待(如维修、区域关闭),若仍有进行中订单会让顾客无处可去,
// 因此与删除一样先拦截;「预约」允许与进行中订单并存(预订的是稍后的时段)。
func SetTableLock(id, lockStatus int, lockRemark string) error {
	if id == 0 {
		return errors.New("参数错误")
	}
	if !po.ValidTableLockStatus(lockStatus) {
		return errors.New("无效的桌台状态")
	}
	if lockStatus == po.TableLockDisabled {
		// 与顾客端同一套营业日隔离:跨日遗留单不该阻止店家停用这张桌 ——
		// 想收拾的恰恰是这种桌,却因为一笔早该处理的旧单卡住,只会把人逼去改库。
		if _, ok := dao.GetActiveOrderByTable(id, StaleGuardBizDate()); ok {
			return errors.New("该桌台存在进行中订单，请先完成或取消订单后再停用")
		}
	}
	lockRemark = strings.TrimSpace(lockRemark)
	if r := []rune(lockRemark); len(r) > 60 {
		lockRemark = string(r[:60])
	}
	return dao.SetTableLock(id, lockStatus, lockRemark)
}

// 强制清台时对「未结账订单」的落点。两种方式的对账含义相反,必须显式二选一。
const (
	ReleaseActionSettle = "settle" // 补结账:钱已在店里收过、只是系统漏点收款,按正常收款入账
	ReleaseActionCancel = "cancel" // 作废:客人跑单/误开单,本单不计收入
)

// ReleaseOutcome 一次强制清台的结果,供 handler 组装审计摘要与响应文案。
type ReleaseOutcome struct {
	TableID    int
	TableName  string
	Settled    int   // 补结账的未支付订单数
	Canceled   int   // 作废的未支付订单数
	Finished   int   // 已收款但出餐未完成的订单数(仅归档,不动账务)
	TotalCents int64 // 补结账累计金额(分)
	// 被处理的订单号:handler 据此关掉顾客手里可能还开着的在线支付二维码,
	// 否则客人离店后再扫码会付进一笔「桌台已清空」的订单。
	OrderNos []string
}

// finishPaidOrder 把「已收款但仍在进行中」的订单直接归档为已完成(4)。
//
// 不用 FinishOrder:强制清台在调用前已经批量读过各单的当前状态(curStatus),
// 这里沿用那次快照 + 带状态守卫的 dao 写入,一次清台里的多张单不会被中间的并发
// 操作插进来;FinishOrder 会各自重读状态并单开事务,不适合在同一个循环里批量走。
func finishPaidOrder(orderID, curStatus int, operator string) error {
	tableID, err := dao.GetOrderTable(orderID)
	if err != nil {
		return fmt.Errorf("订单不存在")
	}
	guard := StaleGuardBizDate()
	err = store.WithTx(func(tx *sql.Tx) error {
		n, err := dao.FinishOrderTx(tx, orderID, operator, curStatus)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("订单状态已变更，请刷新后重试")
		}
		return releaseTableIfIdle(tx, tableID, guard)
	})
	if err != nil {
		return err
	}
	dao.HandleUrgesByOrder(orderID, operator)
	return nil
}

// ReleaseTable 强制清台:收走该桌全部进行中订单,并把桌台置为空闲。
//
// 为什么需要它:未接在线支付时「收款」全靠店家手动点,漏点一次,订单就永远停在
// (1/2/3, pay_status=0),桌台永远占用 —— 下一桌扫码会被直接带进上一桌的账单。
// 而 UpdateTable 又刻意不改 status(防止把正在用餐的桌台改成空闲、导致看板丢单),
// 店家在界面上无从补救。这个接口就是那把人工解锁的钥匙。
//
// action 不能静默取默认值:补结账把钱记进营收,作废则不记,选错即账目差错。
// 整个操作逐单处理而非包在一个事务里 —— 结算/取消各自内部已保证原子性,硬凑大
// 事务反而会让单笔失败拖垮整桌;失败即刻返回,已处理的部分留痕可查。
func ReleaseTable(ctx context.Context, tableID int, action, operator string) (ReleaseOutcome, error) {
	if tableID <= 0 {
		return ReleaseOutcome{}, errors.New("参数错误")
	}
	if action != ReleaseActionSettle && action != ReleaseActionCancel {
		return ReleaseOutcome{}, errors.New("请选择清台方式：补结账或作废订单")
	}
	table, err := dao.GetTableByID(tableID)
	if err != nil {
		return ReleaseOutcome{}, errors.New("桌台不存在")
	}
	tableNo, tableName := table.TableNo, table.TableName
	out := ReleaseOutcome{TableID: tableID, TableName: tableName}

	// 收走哪些单,口径必须与顾客端看到的完全一致(StaleGuardBizDate):
	//
	//   - 跨日清台关闭(guard 为空):该桌所有进行中订单一次收走。这里不能按营业日
	//     挑挑拣拣 —— 留下一笔未结账旧单,会造出「桌台显示空闲、客人点完菜却因那笔
	//     旧单被拒绝下单」的矛盾状态,比误结一笔更难排查。
	//   - 跨日清台开启(guard=今天):只收本营业日的单。更早的遗留单已被读取层隔离,
	//     既不挡新客人下单,也不该被这次「强制」顺手改掉账务 —— 它们留给店家在
	//     「订单管理」里逐笔补结账或作废。
	//
	// 少了这层对齐,开启清台后一桌「昨日旧单 + 今日新单」会被一并补结账,
	// 正在用餐那位客人的单就被记成已收款了。
	orders := dao.ListActiveOrdersByTable(tableID, StaleGuardBizDate())
	if len(orders) == 0 {
		// 无进行中订单却仍显示占用(历史脏数据):仅重置占用状态,不动任何账务。
		// 同样走 releaseTableIfIdle(而非无条件置空闲):快照与写入之间存在
		// 「此刻顾客扫码开新单」的竞态窗口,无条件置空闲会把新单抹成空闲桌。
		if err := store.WithTx(func(tx *sql.Tx) error {
			return releaseTableIfIdle(tx, tableID, StaleGuardBizDate())
		}); err != nil {
			return out, err
		}
		logger.CtxInfof(ctx, "[table] 强制清台 table=%d(%s) 无进行中订单,仅重置占用状态 operator=%s", tableID, tableNo, operator)
		return out, nil
	}

	for _, o := range orders {
		out.OrderNos = append(out.OrderNos, o.OrderNo)
		switch {
		case o.PayStatus == 1:
			// 已收款但还在出餐:归档即可,不影响账务。
			if err := finishPaidOrder(o.OrderID, o.OrderStatus, operator); err != nil {
				return out, fmt.Errorf("订单 %s 归档失败：%w", o.OrderNo, err)
			}
			out.Finished++
		case action == ReleaseActionCancel:
			if _, err := CancelOrder(o.OrderID, "强制清台作废", operator); err != nil {
				return out, fmt.Errorf("订单 %s 作废失败：%w", o.OrderNo, err)
			}
			out.Canceled++
		default:
			res, err := SettleOrder(o.OrderID, SettleTypeNormal, "", "强制清台补结账", operator)
			if err != nil {
				return out, fmt.Errorf("订单 %s 补结账失败：%w", o.OrderNo, err)
			}
			out.Settled++
			out.TotalCents += res.PaidCents
		}
	}

	// 兜底置空闲:仅当该桌确实已无进行中订单时才置空闲(带营业日隔离,跨日遗留单不算)。
	// 不能无条件置空闲 —— 清台过程中顾客扫码开新单会先占用桌台,无条件置空闲会把刚开的新单
	// 抹成「空闲桌」:看板显示空闲而实际有未结账订单,下一桌扫码还会被「已有进行中订单」拒绝。
	// 这里复用与各订单处理一致的 releaseTableIfIdle,使判断与写入在同一事务内原子完成。
	if err := store.WithTx(func(tx *sql.Tx) error {
		return releaseTableIfIdle(tx, tableID, StaleGuardBizDate())
	}); err != nil {
		return out, err
	}
	logger.CtxInfof(ctx, "[table] 强制清台 table=%d(%s) action=%s 结算%d/作废%d/归档%d 金额%d分 operator=%s",
		tableID, tableNo, action, out.Settled, out.Canceled, out.Finished, out.TotalCents, operator)
	return out, nil
}

// DeleteTable 校验无进行中订单后逻辑删除桌台。
//
// 删除前先查该桌是否存在进行中订单(1已下单/2制作中/3已上齐):
// 顾客端 CustomerTable 只按 del_flag='0' 过滤桌台,若把正在用餐的桌台删掉,
// 进行中订单会失去可进入的桌台入口,加菜/收款/结账全部卡死,故先拦截并透传提示。
func DeleteTable(id int) error {
	if _, ok := dao.GetActiveOrderByTable(id, StaleGuardBizDate()); ok {
		return errors.New("该桌台存在进行中订单,请先完成或取消订单")
	}
	return dao.DeleteTable(id)
}
