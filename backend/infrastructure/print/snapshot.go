package print

// ============================================================================
// 票面快照
//
// 为什么需要它:「打印记录」页的预览与「补打」此前都是拿**当前**的订单与**当前**的
// 店铺配置重算一遍票据(job.render)。渲染代码本身同源,数据却未必 ——
//   - 订单后来加过菜:重算出来的票上多几道当时没有的菜;
//   - 店名 / 页脚 / 二维码开关 / 纸宽后来改过:重算结果跟着变;
//   - 加菜小票更彻底:当时新增了哪些菜根本无从得知,只能按整单渲染。
// 于是「预览即所得」只对「刚打完、什么都没改」的记录成立。
//
// 台账每一行本就对应**一台打印机上的一张票**(拆单后每张一条,见 job.label),
// 所以把那张票渲染好的文本行随台账一起存下来,预览与补打就都变成回放:
// 存下来的就是打出来的。
//
// 为什么存文本行而不是最终字节:字节里是 GBK 编码与 ESC/POS 指令,与厂商绑定,
// 而文本行是直连 / 飞鹅 / 代理三条通道共用的中间形态 —— 发送层各自消费控制标记
// (\x02big: 放大行、\x02qr: 二维码行、\x03cut 切纸行),预览层则画成纸面。
//
// 存不下时怎么办:老记录(升级前)content 为空,读侧一律回落到重算,行为不倒退。
// ============================================================================

import "encoding/json"

// TicketContent 台账里的票面快照。
type TicketContent struct {
	// Label 这张票的可读标识(拆单厨房单为「【厨房单】2/4」,与票面抬头、台账 detail
	// 前缀同源)。补打时带回去,新台账才能继续分清「补的是第几张」。
	Label string `json:"label,omitempty"`
	// Lines 这张票的完整文本行;空行、控制标记都原样保留。
	Lines []string `json:"lines"`
}

// EncodeTicketContent 把票面快照编码为台账 content 字段。
//
// 编码失败(理论上不会发生:结构里只有 string/slice)返回空串 —— 台账是旁路数据,
// 宁可少一个快照,也不能让一次打印因为写台账失败而报错(调用方也不该感知)。
func EncodeTicketContent(c TicketContent) string {
	if len(c.Lines) == 0 {
		return ""
	}
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeTicketContent 解析台账 content 字段。
//
// 返回 false 的三种情况都按「没有快照」处理(调用方回落到重算):
// 老记录为空、JSON 损坏(人为改库/降级)、以及解析出来没有文本行。
func DecodeTicketContent(raw string) (TicketContent, bool) {
	if raw == "" {
		return TicketContent{}, false
	}
	var c TicketContent
	if err := json.Unmarshal([]byte(raw), &c); err != nil || len(c.Lines) == 0 {
		return TicketContent{}, false
	}
	return c, true
}
