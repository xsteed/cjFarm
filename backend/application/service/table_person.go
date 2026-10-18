package service

// 入座人数的「桌级共享记忆」（茶位费门店的选人数面板）。
//
// 为什么需要：选人数面板原先只记在各自手机本地（H5 sessionStorage / 小程序本地存储），
// 同桌另一位客人扫码进来仍会被拦一次，还可能选出另一个人数。人数本质上是「这张桌」
// 的属性而不是「这部手机」的，所以选完要上报到服务端，同桌共享。
//
// 为什么放内存、不落库：它只是一个**预填默认值**——权威值是下单请求里带的
// personCount（下单时固化进订单并随之清除）。进程重启丢失的只是「下一次进桌的
// 预填」，最坏情况是下一位客人重新选一次，不会产生任何账务影响。
//
// 并发语义（刻意不加锁）：
//   - 后写覆盖先写：两位客人同时开着面板、先后确认不同人数时，后确认者成为
//     面板默认；但每位客人下单时用的都是自己设备上的 personCount，互不影响。
//   - 同刻并发下单：由「一桌只允许一笔未结账单」的既有约束串行化，
//     胜出订单的人数即为准（与共享值无关）。
//   - 下单成功即清除：人数已固化进订单，共享值不再有意义；60 分钟 TTL 兜底
//     清掉「选了却一直没下单」的残留，不把上一批客人的默认带给下一批。

import (
	"sync"
	"time"
)

var (
	tablePersonMu      sync.Mutex
	tablePersonByTable = make(map[int]tablePersonEntry)
	// 与客户端「未开单购物车/人数记忆」一致的 60 分钟窗口；
	// var 而非 const 是为了让测试能收短 TTL。
	tablePersonTTL = 60 * time.Minute
)

type tablePersonEntry struct {
	count int
	at    time.Time
}

// SetTablePendingPerson 记录某桌的入座人数（1..20，越界静默丢弃 —— 人数来自
// 顾客端，按异常输入兜底处理而不是报错）。后写覆盖先写，见文件头并发语义。
func SetTablePendingPerson(tableID, count int) {
	if tableID <= 0 || count < 1 || count > 20 {
		return
	}
	tablePersonMu.Lock()
	defer tablePersonMu.Unlock()
	// 写入时顺手清扫过期条目：共享值是短命数据，避免 map 随桌台数无界增长
	now := time.Now()
	for id, e := range tablePersonByTable {
		if now.Sub(e.at) > tablePersonTTL {
			delete(tablePersonByTable, id)
		}
	}
	tablePersonByTable[tableID] = tablePersonEntry{count: count, at: now}
}

// GetTablePendingPerson 取某桌的共享入座人数；无记录或已过期返回 0（客户端视为「没人选过」）。
func GetTablePendingPerson(tableID int) int {
	if tableID <= 0 {
		return 0
	}
	tablePersonMu.Lock()
	defer tablePersonMu.Unlock()
	e, has := tablePersonByTable[tableID]
	if !has || time.Since(e.at) > tablePersonTTL {
		return 0
	}
	return e.count
}

// ClearTablePendingPerson 清除某桌的共享人数（下单成功时调用：人数已固化进订单）。
func ClearTablePendingPerson(tableID int) {
	if tableID <= 0 {
		return
	}
	tablePersonMu.Lock()
	defer tablePersonMu.Unlock()
	delete(tablePersonByTable, tableID)
}
