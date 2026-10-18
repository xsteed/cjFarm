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

	"dining-system/application/service"
	"dining-system/domain/po"
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

// center 按显示宽度把文本居中(左侧补空格,超出则原样返回)。
//
// 只用于「正常字号」的居中行(顾客小票放大兜底与单据名副标题)。
// 放大行不能靠补空格居中:倍宽后每字符占两格,补出来的空格会跟着变宽,
// 居中须交给机器指令(ESC a / 飞鹅 <C>),见 encodeBigLine。
func center(s string, width int) string {
	w := displayWidth(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", (width-w)/2) + s
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
	// 右段单独一行并右对齐;若右段自身超宽则折行。
	if rw <= width {
		out = append(out, lpad(right, width))
	} else {
		// 超宽右段优先在空格处断:结账时间是「2026-09-21 13:05:00」这类带空格的
		// 定长值,硬按字符折会把「13:05」与「:00」拆到两行,读起来像两个数。
		out = append(out, splitBigLines(right, width)...)
	}
	return out
}

// money 金额格式化(元,两位小数)。
func money(v float64) string { return fmt.Sprintf("%.2f", v) }

// itemQtyLabel 菜品左侧文案:「菜名 (规格) x2」;赠送菜品前缀【赠】。
func itemQtyLabel(it po.OrderItem) string {
	label := it.DishName
	if strings.TrimSpace(it.SpecName) != "" {
		label += " (" + it.SpecName + ")"
	}
	if it.IsGift == 1 {
		label = "【赠】" + label
	}
	return label + " x" + strconv.Itoa(it.Quantity)
}

// itemAmountText 菜品金额列文本:赠菜返回空串(不打金额)。
//
// 赠菜不计入菜品金额与合计(原价只累加进订单的赠送额),在菜品行上打 0.00 或原价
// 都会让客人拿着它去和合计对账却永远对不上;是否赠送由左侧的「【赠】」标记表达。
func itemAmountText(it po.OrderItem) string {
	if it.IsGift == 1 {
		return ""
	}
	return money(po.ToYuan(it.Amount))
}

// appendItemRemark 把单菜备注作为缩进子行追加(如「加辣」)。
func appendItemRemark(lines []string, it po.OrderItem, width int) []string {
	if strings.TrimSpace(it.Remark) == "" {
		return lines
	}
	return appendRemarkLines(lines, "  * "+it.Remark, width)
}

// appendRemarkLines 追加一段备注(按纸宽折行),逐行标记加粗。
//
// 票面形态与逐行拼接完全一致(续行不重复「* 」「整单备注: 」前缀),唯一变化是
// 折行后的续行一起加粗 —— 备注是后厨最容易看漏的地方,半段加粗比不加粗更容易被误读成
// 「这句不重要」。
func appendRemarkLines(lines []string, text string, width int) []string {
	for _, l := range splitLines(text, width) {
		lines = append(lines, boldMarkLine(l))
	}
	return lines
}

// appendKitchenItemRemark 厨房单的单菜备注:形态与顾客小票一致(「  * 加辣」),
// 但按厨房单的备注规则放大(见 appendKitchenRemarkLines)。
func appendKitchenItemRemark(lines []string, it po.OrderItem, width int) []string {
	if strings.TrimSpace(it.Remark) == "" {
		return lines
	}
	return appendKitchenRemarkLines(lines, "  * "+it.Remark, width)
}

// appendKitchenRemarkLines 追加一段厨房单备注:与菜品行同款,放大两倍出纸。
//
// 备注是后厨最容易看漏、做错菜的地方,仅加粗不够醒目 —— 与菜品行用同一套放大行
// 机制(bigL: 左对齐放大):ESC/POS 落地 GS ! 倍宽倍高 + ESC E 加粗,飞鹅落地
// <B> 放大一倍,预览按大字渲染(见 SplitBigLeftLine)。
// 放大后每字符占两格,故先按倍宽行宽(w/2)折行再逐行放大,行宽不超纸宽;
// 折行续行不重复前缀,与 appendRemarkLines 同一形态,只是强调方式从加粗换成放大。
func appendKitchenRemarkLines(lines []string, text string, width int) []string {
	for _, l := range splitLines(text, width/2) {
		lines = append(lines, bigLeftMarkLine(l))
	}
	return lines
}

// splitBigLines 把文本按 max 半角格数折成多行(供放大行使用):
//   - 优先在空格处断行(菜名 / 规格 / 份数是天然断点),不把「(大份)」拆成两半;
//   - 单个词自身超宽(长菜名没有空格)时按字符硬折,与 splitLines 同规则。
//
// 为什么不交给打印机自动折行:倍宽行放不下时机器会按行宽硬折,断点不可控,
// 「x1」可能被折到任意位置;主动折行后每行都独立放大居中,断点落在词与词之间。
func splitBigLines(text string, max int) []string {
	if max < 1 {
		max = 1
	}
	out := make([]string, 0, 2)
	cur := ""
	for _, word := range strings.Fields(text) {
		if cur == "" {
			cur = word
		} else if displayWidth(cur)+1+displayWidth(word) <= max {
			cur += " " + word
		} else {
			out = append(out, cur)
			cur = word
		}
		// 单个词自身超宽:硬折,尾段留作 cur 继续并后面的词。
		for displayWidth(cur) > max {
			parts := splitLines(cur, max)
			out = append(out, parts[:len(parts)-1]...)
			cur = parts[len(parts)-1]
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// ticketHeader 单据公共抬头:店名 + 单据名 + 单号 + 桌台/人数/时间 + 分隔线。
// noLabel 为单号前缀(顾客小票用「订单号」、厨房单用「单号」),留空则不打印单号行。
//
// 品牌区:店名统一放大居中加粗(见 shopNameLines),顾客小票与厨房单一致 ——
// 品牌是票据上最该被一眼认出的信息,各类单子各打一套观感反而显得杂乱。
// 单据名(【顾客小票】/【结账小票】/【厨房单】等)紧随其后:顾客小票弱化居中(副标题),
// 厨房单放大左对齐(后厨靠它一眼分清票种与拆单序号,见函数体内 kitchen 分支)。
//
// kitchen 为真时按「厨打单」惯例精简票头:
//   - 桌号放大居中,且台名与桌号重复时不再另起一行(见 tableNoLines);
//   - 单号用可口头报读的短号,并与取餐号并排一行;
//   - 人数与下单时间并排一行,时间省到分。
//
// 顾客小票保持完整订单号、完整时间、各自独立成行,取餐号与桌号同行(58mm 也放得下;
// 台名过长放不下时退回单独一行)—— 先付模式下顾客结完账仍在等上菜,凭票上的取餐号
// 对号、核对出菜进度,它不再只是「当面对号」的临时凭证;完整订单号太长,不与取餐号并排。
// shopName 为店名快照(空串表示按当前配置读取),见 shopNameLines。
// large 表示整张票已放大(见 guestTicketConfig.large):店名不再额外放大一档
// —— 正文已经是 2 倍,再套一层没有更大字号可用,居中即可。
func ticketHeader(o po.Order, width int, title, noLabel, shopName string, kitchen, large bool) []string {
	var lines []string
	lines = append(lines, shopNameLines(shopName, width, large)...)
	if strings.TrimSpace(title) != "" {
		if kitchen {
			// 厨房单单据名放大:与菜品/备注同一套放大行机制(bigL: 左对齐放大),
			// 票种与拆单序号(【厨房单】1/2)远远可认。左对齐而非居中:一叠夹单
			// 逐张核对序号时视线不必左右找起点,与整单菜品行同一理由。
			// 超宽兜底(常规标题【厨房单】n/m 不会发生):退回正常字号,由
			// BoldLine 形态反推保持加粗 —— 硬放大必然折坏标题,反而认不出票种。
			if t := strings.TrimSpace(title); displayWidth(t) <= width/2 {
				lines = append(lines, bigLeftMarkLine(t))
			} else {
				lines = append(lines, pad(title, width))
			}
		} else {
			// 顾客小票的单据名副标题用机器居中(见 centerMarkPrefix),不再空格补位。
			lines = append(lines, centerMarkLine(title))
		}
	}
	// 取餐号是单据上唯一能口头传递的标识:后厨据此叫号,先付模式下顾客也凭它在
	// 小票上报号、核对上菜。与单号互不替代 —— 单号用于事后追溯,取餐号用于当面交互;
	// 未分配(历史订单/手工补录)时不打印,避免露出无意义的 "0"。
	noLine := ""
	if noLabel != "" && strings.TrimSpace(o.OrderNo) != "" {
		noLine = noLabel + ": " + orderNoFor(o.OrderNo, kitchen)
	}
	pickup := ""
	if o.PickupNo > 0 {
		pickup = "取餐号: " + strconv.Itoa(o.PickupNo)
	}
	people := "人数: " + strconv.Itoa(o.PersonCount)
	createTime := "下单: " + createTimeFor(o.CreateTime, kitchen)

	if kitchen {
		// 两个短标识并排一行(58mm 上 13 + 2 + 9 = 24 格,留有余量);
		// 超宽时交给 splitLines 折行,不会溢出纸宽。
		// 整行加粗:取餐号是后厨叫号、当面报号的唯一口头标识,单号是商家后台
		// 对账的入口,票面上都是要一眼找到的信息 —— 票头其余元素(单据名/桌号/
		// 菜品/备注)已放大,这一行保持正常字号但加粗,信息层级仍清晰。
		if merged := strings.TrimSpace(joinFields(noLine, pickup)); merged != "" {
			for _, l := range splitLines(merged, width) {
				lines = append(lines, boldMarkLine(l))
			}
		}
		lines = append(lines, tableNoLines(o, width, true)...)
		// 人数在左、下单时间右对齐:同类信息(几个人、几点下的)并一行,右边时间好找。
		lines = append(lines, twoCol(people, createTime, width)...)
		return append(lines, hr(width))
	}

	if noLine != "" {
		lines = append(lines, pad(noLine, width))
	}
	// 取餐号并入桌号行:「取餐号: 12  桌号: 03  大厅03桌」在 58mm(32 格)也放得下,
	// 80mm 则避免多出一条只剩几个字的稀疏行;台名过长并排放不下时退回单独成行,
	// 宁可票头多一行也不折行 —— 票头字段折行比整票长一行更难读。
	tableLines := tableNoLines(o, width, false)
	if pickup != "" {
		if merged := joinFields(pickup, strings.TrimSpace(tableLines[0])); displayWidth(merged) <= width {
			tableLines[0] = pad(merged, width)
		} else {
			tableLines = append([]string{pad(pickup, width)}, tableLines...)
		}
	}
	lines = append(lines, tableLines...)
	return append(lines, pad(people, width), pad(createTime, width), hr(width))
}

// shopNameLines 店名行:票据品牌门面,各类单据统一放大居中并加粗。
//
// 放大走 bigMarkLine(ESC/POS 的 GS ! 倍宽倍高 + ESC a 居中 + ESC E 加粗、飞鹅 <C><B>、
// 预览大字加粗居中)。倍宽后一行只剩一半格数(58mm 16 格、80mm 24 格),店名放得下才放大
// —— 放不下(长店名)退回正常字号居中:硬放大必然折成两截,比不放大更难看。
// 退回的这行用 boldMarkLine 显式标记加粗;不要再让 BoldLine 拿店名去反推,
// 那会让每一行都去查一次配置表(见 BoldText 的说明)。
//
// shopName 是调用方给的快照:同一次下单所有票的店名完全一致,逐票重新查库纯属浪费
// —— 一菜一单会把「一道菜一次」放大成几十次单行查询。空串表示未提供,按当前配置读取。
func shopNameLines(shopName string, width int, large bool) []string {
	name := strings.TrimSpace(shopName)
	if name == "" {
		name = strings.TrimSpace(service.GetSetting("shop_name"))
	}
	if name == "" {
		return []string{pad("", width)}
	}
	if large {
		// 整张票已是 2 倍字号:店名再走 bigMarkLine 也只是同一个字号,
		// 于是改用机器居中(发送层落地为居中放大),不靠补位空格。
		return []string{centerMarkLine(name)}
	}
	if displayWidth(name) <= width/2 {
		return []string{bigMarkLine(name)}
	}
	return []string{boldMarkLine(center(name, width))}
}

// joinFields 把非空字段用两个空格并成一行(空字段直接跳过)。
func joinFields(fields ...string) string {
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, "  ")
}

// orderNoFor 单据上的单号:厨打单用短号,其余用完整单号。
//
// 后厨不需要 20 位流水号:短号正是管理端订单列表/打印记录里始终展示的那个标识
// (po.ShortOrderNo),后厨口头报读、商家在后台核对都用同一串字符,少一个翻译环节。
// 完整单号仍可由后台「打印记录」按短号反查,追溯能力不受影响。
func orderNoFor(orderNo string, kitchen bool) string {
	if !kitchen {
		return orderNo
	}
	return po.ShortOrderNo(orderNo)
}

// createTimeFor 单据上的下单时间:厨打单省到分(「09-21 12:00」),其余保持完整时间。
//
// 后厨看时间只为判断「这单压了多久、要不要催」,秒与年份没有信息量;
// 保留 MM-DD 是为了跨零点以后仍能分清是哪天的单。格式非预期时原样返回,
// 绝不因一个格式异常让票面上缺时间。
func createTimeFor(createTime string, kitchen bool) string {
	if !kitchen {
		return createTime
	}
	s := strings.TrimSpace(createTime)
	// 期望 "YYYY-MM-DD HH:MM[:SS]" 形态。
	if len(s) < 16 || s[4] != '-' || s[7] != '-' || s[10] != ' ' {
		return createTime
	}
	return s[5:10] + " " + s[11:16]
}

// tableNoLines 桌号区渲染。
//
// 常规单据一行等宽文本「桌号: 08  大厅08桌」;厨房单(kitchen)把桌号单独放大居中
// 打一行 —— 后厨出纸多、桌台靠翻纸找,桌号是唯一需要远远认出的字段,
// 与成熟产品的「厨打单桌号加大」一致。
//
// 为什么不连台名一起放大:58mm 纸双倍宽只剩 16 个半角位,「桌号: 08  大厅08桌」
// 有 18 位,一并放大必然折行、反而看不全;故只放大「桌号 + 编号」这截短文本。
// 台名里已含桌号(「大厅03桌」)时不再另起一行,避免同一个桌号在票上出现两次。
func tableNoLines(o po.Order, width int, kitchen bool) []string {
	no := strings.TrimSpace(o.TableNo)
	name := strings.TrimSpace(o.TableName)
	// 无桌号(打包外带/手工补录)时保持原样等宽行,放大一行「桌号 」没有意义。
	if !kitchen || no == "" {
		return []string{pad("桌号: "+o.TableNo+"  "+o.TableName, width)}
	}
	out := []string{bigMarkLine("桌号 " + no)}
	// 台名多数就是「区域 + 桌号」(大厅03桌):放大桌号行已经写明桌号,再补一行台名
	// 等于同一个桌号出现两次,后厨看着乱、票面还长一行。只在台名不含桌号
	// (包间名、特殊位置)时才补一行 —— 信息不丢,重复不显。
	if name != "" && !strings.Contains(name, no) {
		out = append(out, pad(name, width))
	}
	return out
}

// KitchenTicketOptions 厨房单渲染选项。
type KitchenTicketOptions struct {
	// Title 单据名:区分「厨房单」与加菜时的「加菜单」——后厨看到加菜单就知道
	// 只需补做这几道,不必重新核对整桌。拆单时调用方已在标题后拼出序号(如「【厨房单】2/5」)。
	Title string
	// ShowPrice 带出每道菜的小计,便于后厨或传菜核对(默认关,后厨只看菜名与数量)。
	ShowPrice bool
	// ShopName 店名快照:整批票共用一份,避免逐票重查配置表(空串则按当前配置读取)。
	// 调用方(打印路径)传快照;预览/补打这类一次性渲染不传也无妨。
	ShopName string
	// SingleDish 这张票只含一道菜(拆单/按份拆出来的夹单):
	//   - 菜名一律放大,超宽(58mm 带规格/【赠】)时按倍宽行宽折成多行放大,夹在菜上、
	//     传菜口远远可认;
	//   - 不编菜品序号、不出「共 N 道 M 份」汇总 —— 恒为 1 道 1 份,纯属浪费纸面。
	SingleDish bool
	// LastTicket 本台打印机这一批拆单小票的最后一张(仅拆单时有意义):
	// 票尾细线之下多加一行「====结束====」粗线 —— 一叠夹单逐张切纸后散落在一起,
	// 细线「----」每张都有、无从分辨;最后一张多出的这一行粗线并嵌「结束」,
	// 后厨凭它确认「单子到齐了」。
	LastTicket bool
}

// RenderKitchenTicket 渲染厨房单(厨打单/夹单),版式对齐成熟 POS 的通行做法:
//
//   - 票头:单据名与桌号放大、单号用可口头报读的短号、时间省到分(见 ticketHeader);
//   - 菜品区:整单逐道编号(便于划单报数),拆单则单道菜放大;赠菜带【赠】;
//   - 做法/忌口:单菜备注缩进成子行,与整单备注一并放大加粗 —— 后厨最容易做错的地方
//     (见 appendKitchenRemarkLines);
//   - 票尾:整单给一行「共 N 道 M 份」,出纸是否齐全一眼可比对;
//     拆单批次的最后一张在票尾细线之下多加一行「====结束====」,标记整叠夹单到此为止(见 LastTicket)。
func RenderKitchenTicket(o po.Order, items []po.OrderItem, paperWidth int, opts KitchenTicketOptions) []string {
	w := LineWidth(paperWidth)
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "【厨房单】"
	}
	lines := ticketHeader(o, w, title, "单号", opts.ShopName, true, false)
	for i, it := range items {
		label := itemQtyLabel(it)
		row := itemNo(i, label, opts.SingleDish)
		switch {
		case opts.ShowPrice:
			// 两列行(菜名 + 金额)放大后等宽对齐会崩,不放大。
			lines = append(lines, twoCol(row, itemAmountText(it), w)...)
		case opts.SingleDish:
			// 夹单一律放大:夹单的意义就是菜名远远可认,回退正常字号等于整张票没有重点。
			// 菜名(带规格/【赠】)超过倍宽行宽时主动折成多行放大,断点落在词间
			// (见 splitBigLines),而不是让打印机按行宽硬折。
			// 每一行都要带 big: 标记 —— 少了它下游只当普通行打印,折行白折(菜名并没有放大)。
			for _, l := range splitBigLines(row, w/2) {
				lines = append(lines, bigMarkLine(l))
			}
		case displayWidth(row) <= w/2:
			// 整单清单:放大后每字符占两格,宽度减半;放不下会折行,反而比正常字号
			// 更难读 —— 与店名行同一套保护(58mm 纸基本都会回退成正常字号)。
			// 整单清单逐条编号、左对齐放大,编号留在行首便于后厨划单。
			lines = append(lines, bigLeftMarkLine(row))
		default:
			lines = append(lines, splitLines(row, w)...)
		}
		lines = appendKitchenItemRemark(lines, it, w)
	}
	if !opts.SingleDish && len(items) > 0 {
		// 汇总紧跟菜品区收尾,不再插分隔线:两者是同一块内容(菜品清单及其总数),
		// 中间加线只会让票面多一行空白。
		lines = append(lines, splitLines(itemSummary(items), w)...)
	}
	if strings.TrimSpace(o.OrderRemark) != "" {
		lines = append(lines, hr(w))
		lines = appendKitchenRemarkLines(lines, "整单备注: "+o.OrderRemark, w)
	}
	// 票尾分隔线保持细线不变;拆单批次的最后一张在其下再加一行「====结束====」粗线
	// (见 KitchenTicketOptions.LastTicket)—— 粗线即「这是最后一张」的视觉标记。
	// 粗线走居中控制行由机器居中(等宽估算的「铺满一行」在真实字宽下可能差出半格),
	// 并按 BoldLine 的粗线行规则加粗(ESC E 重打 / 飞鹅 <BOLD> / 预览 font-weight):
	// 单纯一排等号笔画偏细,加粗才够「粗线」的观感。
	lines = append(lines, hr(w))
	if opts.LastTicket && opts.SingleDish {
		lines = append(lines, centerMarkLine(lastTicketMark(w)))
	}
	// 票尾垫一行空行再交给切纸:热敏机的切纸动作在末行打印完之后走纸裁断,末行内容
	// 离刀口太近会被拦腰截断 —— 整单备注移到票尾后最明显,备注/分隔线正处在刀口上。
	// 空行把票脚垫高一行,无论有没有整单备注,票尾都不再贴着刀口。
	lines = append(lines, "")
	return lines
}

// itemNo 菜品行文案:整单逐道编号(「1. 凉拌青瓜 (份) x2」),拆单不编号。
//
// 编号是后厨划单的最低成本手段:报数、清点、催菜时都能只报序号;
// 拆单时每张票只有一道菜,编号恒为 1,加了只会占行宽。
func itemNo(idx int, label string, singleDish bool) string {
	if singleDish {
		return label
	}
	return strconv.Itoa(idx+1) + ". " + label
}

// itemSummary 菜品汇总行(「共 2 道 3 份」):道数用于核对有没有漏打,
// 份数用于备料与出餐分量核对(同一道菜点 3 份就是 3 份的量)。
func itemSummary(items []po.OrderItem) string {
	qty := 0
	for _, it := range items {
		qty += it.Quantity
	}
	return fmt.Sprintf("共 %d 道 %d 份", len(items), qty)
}

// lastTicketMark 拆单最后一张的票尾标记行:一整行等号粗线,中间嵌「结束」。
//
// 光有粗线只能看出「这张不一样」,远看仍要凑近读标题序号才知道是最后一张;
// 中间嵌「结束」两字后,粗线本身就是一句「整叠到此为止」,不用再和别的票比。
// 「结束」左右各留一个空格再让等号贴边:夹着等号的「结束」笔画会与等号糊在一起,
// 留缝后远看更清楚。文本共 6 格(两个全角字 + 两个半角空格),两侧等号均分剩余宽度 ——
// 纸宽(32/48)恒为偶数,两边必然等长;万一出现奇数纸宽,余数归右侧,视觉上仍居中。
func lastTicketMark(w int) string {
	const text = " 结束 "
	side := (w - displayWidth(text)) / 2
	return strings.Repeat("=", side) + text + strings.Repeat("=", w-displayWidth(text)-side)
}

// guestTicketConfig 顾客小票的模板配置快照。
// 渲染只消费快照,不直接读库——既保证一张票内配置一致,也让「示例预览」
// 能注入未保存的表单值(所见即所改)。
type guestTicketConfig struct {
	footer       string // 页脚文案,空 = 不打印
	showDiscount bool
	// large 整张结账小票放大一倍(倍宽倍高)。铺开的是「整张票都是这个字号」,
	// 因此行宽必须按半个纸宽重排,否则机器会把算好的两列对齐硬折行打散。
	// 只有飞鹅云通道消费它(打印路径按通道二次收窄,见 print.job.render);
	// 直连/代理仍出原版式 —— 那两条通道的字库与纸宽假设不同,不做跨通道联动。
	large bool
}

// currentGuestTicketConfig 从配置读取当前快照(实际打印路径)。
func currentGuestTicketConfig() guestTicketConfig {
	return guestTicketConfig{
		footer:       guestFooter(),
		showDiscount: guestShowDiscount(),
		large:        guestLargeFont(),
	}
}

// RenderGuestTicket 渲染结账小票(trigger=settle):整单明细 + 金额分项 + 结算信息,
// 使用当前库配置。它是收银凭证,顾客拿它对账、留底。
func RenderGuestTicket(o po.Order, items []po.OrderItem, paperWidth int) []string {
	return renderGuestTicketWith(o, items, paperWidth, currentGuestTicketConfig(), true)
}

// RenderGuestOrderTicket 渲染点菜单(trigger=order,下单即出):整单明细 + 一行
// 「当前合计」。与结账小票共用同一套渲染,只差三处 —— 标题、金额区措辞、
// 不出优惠与结算块:
//   - 标题「【顾客小票】」:与【结账小票】一眼可分,顾客桌上两张纸不再认混;
//   - 金额区只有「当前合计」:下单后还会加菜,这个数是预估口径,刻意不用
//     「合计」措辞 —— 与加菜小票同一套规避(见 RenderGuestAppendTicket);
//   - 优惠与结算块不打印:下单时钱还没收,支付方式/结账时间无从谈起;
//     优惠行留给结账小票,两张票的金额区因此各有分工;
//   - 页脚(「谢谢惠顾」送客收尾)不打印:客人还没走,只有结账小票带页脚。
func RenderGuestOrderTicket(o po.Order, items []po.OrderItem, paperWidth int) []string {
	return renderGuestTicketWith(o, items, paperWidth, currentGuestTicketConfig(), false)
}

// renderGuestTicketWith 按给定模板配置与形态渲染顾客小票(配置可由预览注入)。
// settled 为真渲染结账形态,为假渲染点菜单形态(差异见 RenderGuestOrderTicket)。
func renderGuestTicketWith(o po.Order, items []po.OrderItem, paperWidth int, cfg guestTicketConfig, settled bool) []string {
	w := LineWidth(paperWidth)
	// 整张放大只作用于结账小票(见 guestTicketConfig.large):倍宽倍高后每行只剩
	// 一半格数,故排版按半个纸宽算 —— 否则 pad/twoCol 算出的 48 格行会被机器硬折,
	// 「菜名 —— 金额」的两列对齐全散。
	large := cfg.large && settled
	if large {
		w /= 2
	}
	title := "【结账小票】"
	if !settled {
		title = "【顾客小票】"
	}
	lines := ticketHeader(o, w, title, "订单号", "", false, large)
	for _, it := range items {
		lines = append(lines, twoCol(itemQtyLabel(it), itemAmountText(it), w)...)
		lines = appendItemRemark(lines, it, w)
	}
	// 茶位费跟菜品同区(分隔线之上):它和菜品一样是「按行计费」的收费项(人数×单价),
	// 与菜名挨在一起读起来才连贯;分隔线以下只留优惠与合计这类整单口径的收尾项。
	// 金额为 0(本单不收 / 门店未开启)时不打,避免出现「茶位费(4人) 0.00」这种像收错了的行。
	if o.SeatFee > 0 {
		lines = append(lines, twoCol(fmt.Sprintf("茶位费(%d人)", o.PersonCount), money(po.ToYuan(o.SeatFee)), w)...)
	}
	lines = append(lines, hr(w))
	// 金额区两种形态各出一版:
	//   - 结账:优惠(真正从总额里减掉的钱)+「合计」;赠送不再单列 —— 赠菜已在
	//     菜品区用【赠】逐道标注,下面再写一遍是重复。
	//   - 点菜单:只有「当前合计」,优惠与结算信息都留给结账小票。
	if settled {
		if o.DiscountAmount > 0 && cfg.showDiscount {
			lines = append(lines, twoCol("优惠", "-"+money(po.ToYuan(o.DiscountAmount)), w)...)
		}
		lines = append(lines, twoCol("合计", money(po.ToYuan(o.TotalAmount)), w)...)
	} else {
		lines = append(lines, twoCol("当前合计", money(po.ToYuan(o.TotalAmount)), w)...)
	}
	lines = append(lines, hr(w))
	if strings.TrimSpace(o.OrderRemark) != "" {
		lines = appendRemarkLines(lines, "备注: "+o.OrderRemark, w)
		lines = append(lines, hr(w))
	}
	if !settled {
		// 点菜单止步于此:下单时未结算,免单/挂账标注与结算信息块都不可能出现,
		// 显式跳过而不是依赖「无值不出行」—— 万一调用方把已结算订单误当点菜单重放,
		// 这里宁可少打,也不该让一张「点菜单」带出支付方式来。
		// 页脚(送客收尾)也不打:下单时人刚坐下,「谢谢惠顾」只有客人离场
		// 时才该出现,留给结账小票(见 appendGuestTicketTail)。
		return trimTrailingHr(lines, w)
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
			lines = append(lines, pad("【挂账】待收 "+money(po.ToYuan(o.CreditAmount)), w))
			if o.SettleRemark != "" {
				lines = append(lines, splitLines("挂账人: "+o.SettleRemark, w)...)
			}
			lines = append(lines, pad("(挂账未结,请尽快至前台核销)", w))
		case 2:
			lines = append(lines, pad("【挂账已结】"+money(po.ToYuan(o.CreditAmount)), w))
		}
	}
	lines = append(lines, guestSettleLines(o, w)...)
	lines = appendGuestTicketTail(lines, w, cfg)
	if large {
		return enlargeGuestLines(lines, w)
	}
	return lines
}

// enlargeGuestLines 把结账小票的每一行升级为「放大行」,并按半纸宽 w 兜底折行。
//
// 整张票同一字号后,层级只能靠对齐表达:店名与单据名居中放大,正文(含合计/备注)
// 左对齐放大 —— 与正常字号票的观感一一对应(正文在左,标题居中)。
// 不在这里逐行判断「谁更重要」:字号已经是全局的,再叠 <BOLD>/<B> 只会让
// 同一张票出现两种字宽、把 pad 出的列对齐弄坏。
//
// 为什么还要再折一次行:票头那几行(订单号 / 取餐号+桌号 / 下单时间)是按正常纸宽
// 写死的长字符串,在 48 格纸里放得下、在半纸宽(24 格)里放不下 —— 不主动折,
// 机器会按自己的断点硬折,「取餐号: 12  桌号: 03 …」可能断在数字中间。
// 折行由渲染层做,断点可控,与菜品行的处理一致。
//
// 切纸/二维码是控制行而非正文,原样透传(套上放大标签会被当文本打出来)。
func enlargeGuestLines(lines []string, w int) []string {
	out := make([]string, 0, len(lines))
	appendLine := func(text string, centered bool) {
		parts := []string{text}
		if displayWidth(text) > w {
			// 超宽优先在空格处断(「订单号:」与单号分两行、时间不拦腰截断),
			// 无空格可断的长串才按字符硬折。
			parts = splitBigLines(text, w)
		}
		for _, part := range parts {
			if centered {
				out = append(out, bigMarkLine(part))
			} else {
				out = append(out, bigLeftMarkLine(part))
			}
		}
	}
	for _, l := range lines {
		if SplitCutLine(l) {
			out = append(out, l)
			continue
		}
		if _, ok := SplitQRLine(l); ok {
			out = append(out, l)
			continue
		}
		if big, ok := SplitBigLine(l); ok {
			// 店名本来就是放大行,保持居中放大。
			appendLine(big, true)
			continue
		}
		if ctr, ok := SplitCenterLine(l); ok {
			// 单据名副标题:正常字号下居中,整张放大后仍是居中放大。
			appendLine(ctr, true)
			continue
		}
		if b, ok := SplitBoldLine(l); ok {
			appendLine(b, false)
			continue
		}
		appendLine(l, false)
	}
	return out
}

// trimTrailingHr 裁掉行尾残留的分隔线:金额/备注区收尾的 hr 若成为最后一行,
// 看上去像一条孤零零的横线,而不是票据的自然结束。
func trimTrailingHr(lines []string, w int) []string {
	for len(lines) > 0 && lines[len(lines)-1] == hr(w) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// appendGuestTicketTail 渲染顾客小票的送客收尾:结算信息块之后是可配置页脚。
// 这是「客人要走了」的收尾,只有结账小票走它;点菜单与加菜小票都不走 ——
// 人还在店里,见 renderGuestTicketWith 与 RenderGuestAppendTicket。
func appendGuestTicketTail(lines []string, w int, cfg guestTicketConfig) []string {
	// 页脚文案可配置(管理端「系统配置 → 小票打印」),清空则整段省略(含分隔线)。
	if cfg.footer != "" {
		// 有整单备注时,备注块自己已收过一条分隔线;这里再补一条会连出两条横线。
		if len(lines) == 0 || lines[len(lines)-1] != hr(w) {
			lines = append(lines, hr(w))
		}
		lines = append(lines, splitLines(cfg.footer, w)...)
	} else {
		// 页脚省略时,裁掉尾部残留的分隔线(备注/金额区后的 hr 会成为孤行收尾)。
		lines = trimTrailingHr(lines, w)
	}
	// 票尾二维码位置刻意留空,预留给小程序码:产品规划是以后在票尾打一枚
	// 小程序码,顾客扫码进小程序查看/重打小票。此前的小票二维码(普通 URL 的
	// GS ( k 码)已下线 —— 桌台上已有桌台点餐码,票上再打一枚是重复入口。
	//
	// 实现提醒:小程序码是微信接口生成的「图片」(葵花码),不是 QR 符号,
	// 现有 qrMark 控制行(qrMarkLine → encodeQR 的 GS ( k)带不动它;到时候要
	// 新增一条「位图」控制行(打印机走 GS v 0 / GS ( L 位图指令,注意 58mm
	// 可打印宽度与缩放),预览侧同样按图片渲染。qrMark 基础设施保留不删,
	// 就是给这一步留的位置。
	return lines
}

// RenderGuestAppendTicket 渲染加菜小票:只列本次新增的菜品,不重放整单。
//
// 客人刚加完菜,关心的是「这次加了什么、现在一共多少钱」,而不是再核对一遍
// 此前已经点过的菜。因此菜品区只出新增明细,金额只给一行「当前合计」
// (整单最新,含此前已点的全部菜品)—— 措辞刻意不用「合计」,避免被误读为
// 所列菜品的金额小计。优惠等整单口径的信息留在结账小票上。
//
// 不带送客收尾:页脚(「谢谢惠顾，欢迎再次光临！」)与扫码重打的二维码都是
// 「客人要走了」才该出现的东西 —— 加菜时人还在店里,见 appendGuestTicketTail。
func RenderGuestAppendTicket(o po.Order, items []po.OrderItem, paperWidth int) []string {
	w := LineWidth(paperWidth)
	lines := ticketHeader(o, w, "【加菜小票】", "订单号", "", false, false)
	for _, it := range items {
		lines = append(lines, twoCol(itemQtyLabel(it), itemAmountText(it), w)...)
		lines = appendItemRemark(lines, it, w)
	}
	lines = append(lines, hr(w))
	lines = append(lines, twoCol("当前合计", money(po.ToYuan(o.TotalAmount)), w)...)
	// 结算信息块在加菜场景恒为空(加菜仅限进行中且未支付的订单),不会打出空标签。
	lines = append(lines, guestSettleLines(o, w)...)
	return trimTrailingHr(lines, w)
}

// guestSettleLines 渲染「结算信息」块:支付方式 / 结账时间 / 实收 / 已退款。
//
// 这是结账小票区别于「下单时那张预估小票」的实质内容 —— 客人拿着它要能回答
// 「我怎么付的、什么时候付的、实际付了多少」。此前模板只标注免单与挂账,
// 正常收款的小票上看不出支付方式与结账时间,顾客留底和收银对账都说不清。
//
// 各行按「有值才打」:未结算的订单(下单即出票的场景)支付信息为空,不该
// 在票面上留一排空标签。
func guestSettleLines(o po.Order, w int) []string {
	lines := []string{}
	if payType := trimStrPtr(o.PayType); payType != "" {
		lines = append(lines, twoCol("支付方式", payType, w)...)
	}
	if t := trimStrPtr(o.SettleTime); t != "" {
		lines = append(lines, twoCol("结账时间", t, w)...)
	}
	if t := trimStrPtr(o.CreditSettleTime); t != "" {
		lines = append(lines, twoCol("核销时间", t, w)...)
	}
	// 实收只在与合计不一致时补行:免单/挂账待收已有专门标注,
	// 正常收款下实收 = 合计,重复一行只会让票面更啰嗦。
	// 发生部分退款时合计仍是原价,这一行才是顾客真正支付的金额。
	if o.PaidAmount > 0 && o.PaidAmount != o.TotalAmount {
		lines = append(lines, twoCol("实收", money(po.ToYuan(o.PaidAmount)), w)...)
	}
	if o.RefundAmount > 0 {
		lines = append(lines, twoCol("已退款", "-"+money(po.ToYuan(o.RefundAmount)), w)...)
	}
	return lines
}

// trimStrPtr 取 *string 的值并去空白;nil 视为空串。
//
// 订单的支付方式、结算时间等列在订单未结算时为 NULL,直接解引用会 panic。
func trimStrPtr(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// ============================================================================
// 二维码
// ============================================================================

// qrMarkPrefix 二维码标记行前缀。
//
// 渲染层只有 []string,二维码图形无法用文本表达,因此用一行「控制行」把意图
// 传给发送层:ESC/POS 通道转成 GS ( k 指令由打印机自行生成符号,飞鹅云通道
// 转成 <QR> 标签,预览通道渲染成真正的二维码图片。
//
// 发送层与预览层都必须消费(剥掉)该行,否则这串前缀会被当成正文打到纸上。
// 前缀里的 \x02 是不可见控制字符,正常票据文本不会出现,不会误判。
//
// 当前没有任何票据会产出该行(小票二维码已下线:桌台上已有桌台点餐码),
// 整套基础设施(生成/解析/ESC-POS/飞鹅/预览)保留 —— 票尾位置预留给小程序码,
// 见 appendGuestTicketTail 的实现提醒。
const qrMarkPrefix = "\x02qr:"

// qrMarkLine 生成二维码标记行。
func qrMarkLine(content string) string { return qrMarkPrefix + content }

// SplitQRLine 若该行是二维码标记行,返回二维码内容与 true;否则返回 false。
// 导出给 handler(预览)使用。
func SplitQRLine(line string) (string, bool) {
	if !strings.HasPrefix(line, qrMarkPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, qrMarkPrefix)), true
}

// ============================================================================
// 放大行
// ============================================================================

// bigMarkPrefix 放大行标记前缀。
//
// 与二维码同一套「控制行」思路:渲染层只有 []string 表达不了字号,就用一行控制行
// 把「这一行要放大居中加粗」的意图传给发送层(ESC/POS 用 GS ! 倍宽倍高 + ESC a 居中
// + ESC E 加粗,飞鹅云用 <C><B> 标签,预览按大字加粗居中渲染)。
// 发送层与预览层都必须消费并剥掉该前缀,否则控制字符会被当正文打到纸上。
const bigMarkPrefix = "\x02big:"

// bigMarkLine 生成放大行。
func bigMarkLine(text string) string { return bigMarkPrefix + text }

// SplitBigLine 若该行是放大标记行,返回待放大文本与 true;否则返回 false。
// 导出给 handler(预览)使用。
func SplitBigLine(line string) (string, bool) {
	if !strings.HasPrefix(line, bigMarkPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, bigMarkPrefix)), true
}

// ============================================================================
// 放大行(左对齐)
// ============================================================================

// bigLeftMarkPrefix 左对齐放大行标记前缀。
//
// 与居中放大的 bigMarkPrefix 分开,是因为两种场景要的观感不同:
//   - 居中放大(big:):「一菜一单」的夹单是一票一道菜,居中放大最像招牌,远远可认;
//   - 左对齐放大(bigL:):整单的菜品清单是逐条编号的多行,一旦居中,编号就不在行首,
//     后厨划单时视线要在每行左右找起点。
//
// 落地:飞鹅 <B>(官方语义「放大一倍」,不带 <C> 即保持左对齐)、ESC/POS GS ! 倍宽倍高。
const bigLeftMarkPrefix = "\x02bigL:"

// bigLeftMarkLine 生成左对齐放大行。
func bigLeftMarkLine(text string) string { return bigLeftMarkPrefix + text }

// SplitBigLeftLine 若该行是左对齐放大行,返回待放大文本与 true;否则返回 false。
// 导出给 handler(预览)使用。
func SplitBigLeftLine(line string) (string, bool) {
	if !strings.HasPrefix(line, bigLeftMarkPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, bigLeftMarkPrefix)), true
}

// ============================================================================
// 居中行
// ============================================================================

// centerMarkPrefix 居中行标记前缀。
//
// 与放大行同一套「控制行」思路:单据名副标题这类短行用**机器指令**居中
// (ESC/POS 的 ESC a 1、飞鹅的 <C>、预览的 CSS 居中),不再用空格补位 ——
// 空格居中建立在「半角空格恰好是汉字一半宽」的假设上,机器实际字宽略有出入
// 整行就会偏移;部分云通道还会裁掉行首空格,补位直接失效。
// 与放大行的差别:不加粗、不放大,只改对齐。
const centerMarkPrefix = "\x02ctr:"

// centerMarkLine 生成居中行。
func centerMarkLine(text string) string { return centerMarkPrefix + text }

// SplitCenterLine 若该行是居中标记行,返回文本与 true;否则返回 false。
// 导出给发送层与 handler(预览)使用。
func SplitCenterLine(line string) (string, bool) {
	if !strings.HasPrefix(line, centerMarkPrefix) {
		return "", false
	}
	return strings.TrimPrefix(line, centerMarkPrefix), true
}

// ============================================================================
// 加粗行
// ============================================================================

// boldMarkPrefix 加粗行标记前缀。
//
// 为什么在 BoldLine 的「形态反推」之外还要一个显式标记:反推是单行自洽的,但备注过长
// 会被 splitLines 折行,而续行没有「* 」「整单备注: 」前缀,反推不出来 —— 同一段备注会
// 前半加粗、后半不加粗。折行发生在渲染层,只有渲染层知道哪几行本属于同一段备注,
// 因此由渲染层显式标记;发送层与预览层优先识别标记,BoldLine 的形态反推仅作兜底。
//
// 与 big:/bigL: 的差别:加粗不改变字宽,不参与折行与宽度计算,故不需要 TrimSpace。
const boldMarkPrefix = "\x02b:"

// boldMarkLine 生成加粗行。
func boldMarkLine(text string) string { return boldMarkPrefix + text }

// SplitBoldLine 若该行是加粗标记行,返回待加粗文本与 true;否则返回 false。
// 导出给发送层与 handler(预览)使用。
func SplitBoldLine(line string) (string, bool) {
	if !strings.HasPrefix(line, boldMarkPrefix) {
		return "", false
	}
	return strings.TrimPrefix(line, boldMarkPrefix), true
}

// ============================================================================
// 切纸行
// ============================================================================

// cutMarkPrefix 切纸控制行标记。
//
// 与放大行/二维码同一套「控制行」思路:渲染层只有 []string 表达不了「这里主动切一刀」。
// 飞鹅通道把同一台打印机的多张票合并成一次推送时,用它作为票与票之间的分隔
// (落地为飞鹅的 <CUT> 标签),纸面上因此仍是 N 张各自切开的票。
// 只有「会合并推送」的飞鹅发送层消费它;直连/本地代理每张票本就独立发送、自带切纸。
const cutMarkPrefix = "\x03cut"

// cutMarkLine 生成一行切纸控制行。
func cutMarkLine() string { return cutMarkPrefix }

// SplitCutLine 判断一行是否为切纸控制行。导出给预览层剥掉控制字符用。
func SplitCutLine(line string) bool { return line == cutMarkPrefix }

// ============================================================================
// 强调
// ============================================================================

// BoldLine 报告一行是否按「票据排版惯例」加粗强调。
//
// 这是「形态反推」:仅凭行文本就能判断,不必让渲染层逐行标记。但它对折行无能为力 ——
// 长备注的续行没有前缀(见 boldMarkPrefix)。因此消费方必须**先** SplitBoldLine 认显式标记、
// **再**退回本函数兜底,否则折行续行会漏掉加粗。
//
// 规则基于模板生成行的固定形态(而非渲染结构),因此可被所有消费方共用:
//   - 单据标题行:整行恰为「【…】」形态(【厨房单】/【加菜单】/【加菜小票】等),
//     以及拆单后带序号的同款标题(【厨房单】2/5 —— 序号与原标题同行,省纸且一眼看得出第几张);
//   - 合计行:以「合计」或「当前合计」开头的两列行(后者是加菜小票的金额行);
//   - 备注行:「整单备注: …」「备注: …」与单菜备注子行(「* 加辣」)。
//     备注是后厨最容易看漏、也最容易做错菜的地方,与成熟厨打单一样统一加粗;
//   - 粗线行:整行由等号(可夹「结束」)组成的票尾标记线(拆单最后一张),加粗才够醒目。
//
// **本函数不读配置、不碰数据库**:它按行被调用(一次落地就要逐行走一遍),任何
// 「读一次配置」的兜底判断都会随行数放大成几十次单行查询。店名行因此改由渲染层
// 用 boldMarkLine 显式标记(见 shopNameLines),不再在这里拿店名反推。
//
// 各通道的落地:
//   - ESC/POS(直连 + 本地代理):ESC E 重打加粗,不占额外宽度,列对齐不受影响;
//   - 飞鹅云:<BOLD> 标签(官方语义「字体加粗」,同样不改变字符宽度);
//     不可用 <B> —— 它的官方语义是「放大一倍」,会把等宽列对齐打乱;
//   - 预览:按行标记渲染 font-weight。
//
// 菜名/备注里偶现的【】都会带前后缀(数量/备注前缀),不会整行恰为标题形态。
func BoldLine(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	// 顾客小票两种形态的单据名(【顾客小票】/【结账小票】)作为店名下的一行副标题弱化:
	// 店名已放大居中作主视觉,这里再【】加粗只会抢眼。厨房单/加菜单的单据名仍加粗
	// —— 后厨靠它一眼分清票种。
	if t == "【顾客小票】" || t == "【结账小票】" {
		return false
	}
	if strings.HasPrefix(t, "【") && strings.HasSuffix(t, "】") {
		return true
	}
	// 拆单序号标题「【厨房单】2/5」:标题后紧跟 n/m 序号,仍按标题加粗。
	if i := strings.Index(t, "】"); i > 0 && isSeqSuffix(t[i+len("】"):]) {
		return true
	}
	if strings.HasPrefix(t, "合计") || strings.HasPrefix(t, "当前合计") {
		return true
	}
	// 票尾粗线行:整行由等号(可夹「 结束 」)组成,如「==== 结束 ====」(拆单最后一张的
	// 收尾标记,见 KitchenTicketOptions.LastTicket):单纯一排等号笔画偏细,
	// 够不上「粗线」的观感;按加粗(ESC E 重打)输出才醒目。
	// 该形态不会与正文撞车 —— 菜名/备注都不会整行只有等号、「结束」与留缝空格。
	if strings.Contains(t, "结束") && strings.Trim(t, "=结束 ") == "" {
		return true
	}
	// 备注行:整单备注 / 顾客小票备注 / 单菜备注子行(「* 加辣」)。
	// 前缀由模板固定生成,菜名或备注内容本身不会整行撞上这三个前缀。
	return strings.HasPrefix(t, "整单备注:") || strings.HasPrefix(t, "备注:") || strings.HasPrefix(t, "* ")
}

// BoldText 返回该行最终要打印的文本与是否加粗 —— 加粗判定的唯一入口。
//
// 消费方(ESC/POS 编码、飞鹅落地、预览)都必须走它,顺序固定为「显式标记优先、
// 形态反推兜底」。顺序若散落在各处,漏判只是时间问题 —— 备注折行续行就是第一个出事的。
func BoldText(line string) (string, bool) {
	if marked, ok := SplitBoldLine(line); ok {
		return marked, true
	}
	return line, BoldLine(line)
}

// isSeqSuffix 判断标题后的拆单序号后缀(如 "2/5")。
// 必须严格是 n/m 形态(正整数、前不超过后),避免把「【A】说明」这类正文误判为标题。
func isSeqSuffix(s string) bool {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return false
	}
	idx, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	total, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	return err1 == nil && err2 == nil && idx > 0 && total > 0 && idx <= total
}

// RenderTestTicket 渲染测试页:一眼能看出「这台机器是谁、走哪条通道」。
func RenderTestTicket(p po.Printer) []string {
	w := LineWidth(p.PaperWidth)
	route := "IP: " + addrOf(p.IP, p.Port)
	if p.IsFeie() {
		route = "飞鹅云 SN: " + p.FeieSN
	}
	return []string{
		pad("测试打印", w),
		hr(w),
		pad("店铺: "+service.GetSetting("shop_name"), w),
		pad("打印机: "+p.PrinterName, w),
		pad("通道: "+route, w),
		pad("类型: "+printerTypeName(p.PrinterType)+"  纸宽: "+paperWidthName(p.PaperWidth), w),
		pad("份数: "+strconv.Itoa(p.EffectiveCopies()), w),
		pad("时间: "+service.Now(), w),
		hr(w),
	}
}

// printerTypeName 打印机类型中文名。
func printerTypeName(t int) string {
	if t == po.PrinterTypeGuest {
		return "顾客小票"
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
