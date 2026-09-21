// 票据渲染:把订单渲染成「等宽文本行」。
//
// 渲染层刻意只产出 []string,不碰任何厂商协议 —— 同一份文本行既能被
// ESC/POS 发送器按 GBK 编码直发(TCP),也能被飞鹅发送器用 <BR> 拼接后推给云平台。
// 排版靠 pad/两列对齐做「伪等宽」,两种打印机都能对齐。
package print

import (
	"fmt"
	"strconv"
	"strings"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// LineWidth 根据纸宽(32=58mm, 48=80mm)返回每行可容纳的半角字符数(估测值)。
// 58mm 纸约 32 个半角字符,80mm 纸约 48 个半角字符。
func LineWidth(paperWidth int) int {
	if paperWidth == 32 {
		return 32
	}
	return 48
}

// isWide 判断字符是否按 2 个半角宽度计(中日韩文字与全角标点)。
func isWide(r rune) bool { return r > 0x2E80 }

// displayWidth 计算字符串的显示宽度。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// pad 右补空格,把文本对齐到指定宽度(超出则原样返回)。
func pad(s string, width int) string {
	if w := displayWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// lpad 左补空格(右对齐)。
func lpad(s string, width int) string {
	if w := displayWidth(s); w < width {
		return strings.Repeat(" ", width-w) + s
	}
	return s
}

// splitLines 按显示宽度将长文本折行。
func splitLines(s string, width int) []string {
	var lines []string
	var cur []rune
	curW := 0
	for _, r := range s {
		rw := 1
		if isWide(r) {
			rw = 2
		}
		if curW+rw > width {
			lines = append(lines, string(cur))
			cur = nil
			curW = 0
		}
		cur = append(cur, r)
		curW += rw
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// hr 返回一条分隔线。
func hr(width int) string { return strings.Repeat("-", width) }

// twoCol 把左右两段文本排成一行:放得下就同行对齐,放不下就左段折行、右段单独右对齐。
// 用于「菜品 —— 金额」这类对不齐就会很难看的行。
func twoCol(left, right string, width int) []string {
	lw, rw := displayWidth(left), displayWidth(right)
	if rw == 0 {
		return splitLines(left, width)
	}
	if lw+1+rw <= width {
		return []string{left + strings.Repeat(" ", width-lw-rw) + right}
	}
	out := splitLines(left, width)
	// 右段单独一行并右对齐;若右段自身超宽则退化为普通折行。
	if rw <= width {
		out = append(out, lpad(right, width))
	} else {
		out = append(out, splitLines(right, width)...)
	}
	return out
}

// money 金额格式化(元,两位小数)。
func money(v float64) string { return fmt.Sprintf("%.2f", v) }

// itemQtyLabel 菜品左侧文案:「菜名 (规格) x2」。
func itemQtyLabel(it model.OrderItem) string {
	label := it.DishName
	if strings.TrimSpace(it.SpecName) != "" {
		label += " (" + it.SpecName + ")"
	}
	return label + " x" + strconv.Itoa(it.Quantity)
}

// appendItemRemark 把单菜备注作为缩进子行追加(如「加辣」)。
func appendItemRemark(lines []string, it model.OrderItem, width int) []string {
	if strings.TrimSpace(it.Remark) == "" {
		return lines
	}
	return append(lines, splitLines("  * "+it.Remark, width)...)
}

// ticketHeader 单据公共抬头:店名 + 单据名 + 单号 + 桌台/人数/时间 + 分隔线。
// noLabel 为单号前缀(食客小票用「订单号」、厨房单用「单号」),留空则不打印单号行。
func ticketHeader(o model.Order, width int, title, noLabel string) []string {
	lines := []string{
		pad(store.GetCfg("shop_name"), width),
		pad(title, width),
	}
	if noLabel != "" && strings.TrimSpace(o.OrderNo) != "" {
		lines = append(lines, pad(noLabel+": "+o.OrderNo, width))
	}
	return append(lines,
		pad("桌号: "+o.TableNo+"  "+o.TableName, width),
		pad("人数: "+strconv.Itoa(o.PersonCount), width),
		pad("下单: "+o.CreateTime, width),
		hr(width),
	)
}

// RenderKitchenTicket 渲染厨房单。
//
// title 用于区分「厨房单」与加菜时的「加菜单」——后厨看到加菜单就知道
// 只需要补做这几道,不必重新核对整桌。
// showPrice 打开时带出单价小计,便于后厨或传菜核对(默认关闭,后厨只看菜名数量)。
func RenderKitchenTicket(o model.Order, items []model.OrderItem, paperWidth int, showPrice bool, title string) []string {
	w := LineWidth(paperWidth)
	if strings.TrimSpace(title) == "" {
		title = "【厨房单】"
	}
	lines := ticketHeader(o, w, title, "单号")
	for _, it := range items {
		if showPrice {
			lines = append(lines, twoCol(itemQtyLabel(it), money(it.Amount), w)...)
		} else {
			lines = append(lines, splitLines(itemQtyLabel(it), w)...)
		}
		lines = appendItemRemark(lines, it, w)
	}
	if strings.TrimSpace(o.OrderRemark) != "" {
		lines = append(lines, hr(w))
		lines = append(lines, splitLines("整单备注: "+o.OrderRemark, w)...)
	}
	lines = append(lines, hr(w))
	return lines
}

// RenderGuestTicket 渲染食客小票(含金额、结算方式标注)。
func RenderGuestTicket(o model.Order, paperWidth int) []string {
	w := LineWidth(paperWidth)
	lines := ticketHeader(o, w, "【食客小票】", "订单号")
	for _, it := range o.Items {
		lines = append(lines, twoCol(itemQtyLabel(it), money(it.Amount), w)...)
		lines = appendItemRemark(lines, it, w)
	}
	lines = append(lines, hr(w))
	lines = append(lines, twoCol("菜品金额", money(o.DishAmount), w)...)
	lines = append(lines, twoCol("餐位费", money(o.SeatFee), w)...)
	if o.DiscountAmount > 0 {
		lines = append(lines, twoCol("优惠", "-"+money(o.DiscountAmount), w)...)
	}
	lines = append(lines, twoCol("合计", money(o.TotalAmount), w)...)
	lines = append(lines, hr(w))
	if strings.TrimSpace(o.OrderRemark) != "" {
		lines = append(lines, splitLines("备注: "+o.OrderRemark, w)...)
		lines = append(lines, hr(w))
	}
	// 免单 / 挂账:小票需明确标注结算方式,避免客人或收银对账时产生歧义。
	switch o.SettleType {
	case "free":
		lines = append(lines, pad("【免单】实收 0.00", w))
		if o.SettleRemark != "" {
			lines = append(lines, splitLines("免单原因: "+o.SettleRemark, w)...)
		}
	case "credit":
		switch o.CreditStatus {
		case 1:
			lines = append(lines, pad("【挂账】待收 "+money(o.CreditAmount), w))
			if o.SettleRemark != "" {
				lines = append(lines, splitLines("挂账人: "+o.SettleRemark, w)...)
			}
			lines = append(lines, pad("(挂账未结,请尽快至前台核销)", w))
		case 2:
			lines = append(lines, pad("【挂账已结】"+money(o.CreditAmount), w))
		}
	}
	lines = append(lines, hr(w))
	lines = append(lines, pad("谢谢惠顾,欢迎再次光临", w))
	return lines
}

// RenderTestTicket 渲染测试页:一眼能看出「这台机器是谁、走哪条通道」。
func RenderTestTicket(p model.Printer) []string {
	w := LineWidth(p.PaperWidth)
	route := "IP: " + addrOf(p.IP, p.Port)
	if p.IsFeie() {
		route = "飞鹅云 SN: " + p.FeieSN
	}
	return []string{
		pad("测试打印", w),
		hr(w),
		pad("店铺: "+store.GetCfg("shop_name"), w),
		pad("打印机: "+p.PrinterName, w),
		pad("通道: "+route, w),
		pad("类型: "+printerTypeName(p.PrinterType)+"  纸宽: "+paperWidthName(p.PaperWidth), w),
		pad("份数: "+strconv.Itoa(p.EffectiveCopies()), w),
		pad("时间: "+store.Now(), w),
		hr(w),
	}
}

// printerTypeName 打印机类型中文名。
func printerTypeName(t int) string {
	if t == model.PrinterTypeGuest {
		return "食客小票"
	}
	return "厨房单"
}

// paperWidthName 纸宽中文名。
func paperWidthName(v int) string {
	if v == 32 {
		return "58mm"
	}
	return "80mm"
}
