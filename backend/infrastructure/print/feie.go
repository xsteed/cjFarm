// 飞鹅云打印通道(发送层胶水)。
//
// 职责切分:
//   - 接口协议本身在 infrastructure/adapter/feie(外部服务适配器:签名、调用、解码);
//   - 本文件只做两件事:
//     1) 从系统配置构造适配器客户端(商户改配置即刻生效,无需重启);
//     2) 把渲染好的文本行按飞鹅通道的排版规则拼装、分段后推给适配器。
//
// 票据排版:content 用 <BR> 换行,官方另支持 <C></C> 居中、<BOLD></BOLD> 字体加粗、
// <B></B> 放大一倍、<QR></QR> 二维码等标签。
//
// 本实现只对三类行套标签:二维码(<QR>)、放大居中的桌号(<C><B>)、加粗强调行(<BOLD>)。
// 其余行保持纯文本 —— 排版已在 ticket.go 里用等宽对齐算好,而任何改变字符宽度的
// 标签(<B> 放大一倍、<W> 变宽一倍、<CB> 居中放大)都会把它打乱。
// <BOLD> 只加粗、不改宽度,与 ESC E 语义一致,故可用于强调行。
package print

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/adapter/feie"
)

// NewFeieClient 用系统配置里的飞鹅账号构造适配器客户端。
func NewFeieClient() (*feie.Client, error) {
	user := strings.TrimSpace(service.GetSetting("feie_user"))
	ukey := strings.TrimSpace(service.GetSetting("feie_ukey"))
	if user == "" || ukey == "" {
		return nil, errors.New("未配置飞鹅账号:请在「系统配置 → 小票打印」填写飞鹅账号与 UKEY")
	}
	return feie.NewClient(feie.Config{
		User:   user,
		Ukey:   ukey,
		APIURL: strings.TrimSpace(service.GetSetting("feie_api_url")),
	}), nil
}

// FeieConfigured 报告飞鹅账号是否已配置完整(供前端提示与状态判断)。
func FeieConfigured() bool {
	return strings.TrimSpace(service.GetSetting("feie_user")) != "" &&
		strings.TrimSpace(service.GetSetting("feie_ukey")) != ""
}

// ============================================================================
// 发送
// ============================================================================

// sendViaFeie 把渲染好的文本行推给飞鹅云。
// 内容超过单次上限时按行拆成多单依次推送;任一段失败即中止。
// 第二个返回值是云端返回的 JSON 原文(逐段更新为最后一段的原文),供调用方写日志/台账。
func sendViaFeie(c *feie.Client, p po.Printer, lines []string) (orderID, raw string, err error) {
	sn := strings.TrimSpace(p.FeieSN)
	if sn == "" {
		return "", "", newPermError("未填写飞鹅打印机编号(SN)")
	}
	times := p.EffectiveCopies()
	chunks := splitFeieContent(lines)
	for i, chunk := range chunks {
		id, chunkRaw, cerr := c.PrintMsgRaw(sn, chunk, times)
		if chunkRaw != "" {
			raw = chunkRaw
		}
		if cerr != nil {
			if i > 0 {
				// 前几段已经送出去了,把云端单号带上,便于商家判断「打了一半」。
				return orderID, raw, fmt.Errorf("第 %d/%d 段推送失败: %w", i+1, len(chunks), cerr)
			}
			return "", raw, cerr
		}
		orderID = id
	}
	return orderID, raw, nil
}

// escapeFeie 转义文本里的尖括号。
// 飞鹅 content 是「标签 + 文本」混排的,菜名或备注里一旦出现 < 会被当成标签解析,
// 官方文档也要求把 < > 转义为实体。
func escapeFeie(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// feieLinePiece 一行文本在飞鹅 content 里的落地形态:
// 控制行(切纸/二维码/居中放大/左对齐放大/加粗标记)按各自的标签落地,
// BoldLine 形态反推的强调行套 <BOLD>,其余是转义后的纯文本。
// 顺序不能乱:控制行必须先判,否则控制前缀会被当正文转义打出来。
func feieLinePiece(l string) string {
	if SplitCutLine(l) {
		// 切纸是纯控制标签,绝不能走转义分支 —— 否则 &lt;CUT&gt; 会被当正文打到纸上。
		return feieCutTag
	}
	if qr, ok := SplitQRLine(l); ok {
		return "<QR>" + escapeFeie(qr) + "</QR>"
	}
	if big, ok := SplitBigLine(l); ok {
		// 放大行(店名/夹单桌号/一菜一单的菜名)正是需要 <B> 倍宽的场景:内容短、居中,
		// 不存在等宽列对齐问题(常规正文行才刻意避开 <B>,见文件头注释)。
		return "<C><B>" + escapeFeie(big) + "</B></C>"
	}
	if bigL, ok := SplitBigLeftLine(l); ok {
		// 左对齐放大(整单菜品清单):<B> 是官方「放大一倍」,不带 <C> 即保持左对齐,
		// 逐条编号因此在行首对齐。行宽由渲染层保证「放大后仍放得下」。
		return "<B>" + escapeFeie(bigL) + "</B>"
	}
	if ctr, ok := SplitCenterLine(l); ok {
		// 机器居中行(单据名副标题):<C> 是官方「居中」,由打印机自己对齐,
		// 不依赖补位空格(部分云通道会裁行首空格)。是否加粗沿用形态反推。
		if _, bold := BoldText(ctr); bold {
			return "<C><BOLD>" + escapeFeie(ctr) + "</BOLD></C>"
		}
		return "<C>" + escapeFeie(ctr) + "</C>"
	}
	// 加粗判定走唯一入口:渲染层的显式标记优先(备注折行续行靠它),BoldLine 形态反推兜底。
	// <BOLD> 是官方「字体加粗」,不改变字符宽度 —— 与 ESC/POS 的 ESC E 重打等价,
	// 等宽列对齐不受影响。不要用 <B>:它的官方语义是「放大一倍」。
	if bold, ok := BoldText(l); ok {
		return "<BOLD>" + escapeFeie(bold) + "</BOLD>"
	}
	return escapeFeie(l)
}

// feieSep / feieCutTag 是飞鹅 content 里的行分隔符与主动切纸标签。
// 二者都只定义一次:拼接、字节统计共用同一来源 —— 写成字面量迟早会漂移。
const (
	feieSep    = "<BR>"
	feieCutTag = "<CUT>"
)

// feieSepBytes 行分隔符占用的字节数。
const feieSepBytes = len(feieSep)

// feieLineCost 一行在飞鹅 content 里的落地字节数。
//
// 装箱(feieTicketBytes)与切段(feieLineChunks)必须共用这一个函数:两处口径一旦漂移,
// 「按票装箱」算出的边界就会与实际切段对不上,后果正是它要防止的那件事 ——
// 把一张票切成两半送到后厨手里。
func feieLineCost(l string) int { return len(feieLinePiece(l)) }

// feieLineChunks 飞鹅云通道的行分段区间:单次推送的字节上限。
//
// 必须按「落地后的字节数」而非原始文本长度来累计:<BR> 比换行符长 3 字节,
// <QR>/<C><B> 标签与尖括号转义也会把一行撑大 —— 按原始长度算会算错切点。
func feieLineChunks(lines []string) []chunkSpan {
	return lineChunks(lines, feie.MaxBytes, feieSepBytes, feieLineCost)
}

// splitFeieContent 把文本行拼成 content,并按字节上限切分。
//
// 区间由 feieLineChunks 计算,与预览共用同一份切段逻辑。
func splitFeieContent(lines []string) []string {
	out := make([]string, 0, 1)
	for _, sp := range feieLineChunks(lines) {
		pieces := make([]string, 0, sp.End-sp.Start)
		for _, l := range lines[sp.Start:sp.End] {
			pieces = append(pieces, feieLinePiece(l))
		}
		out = append(out, strings.Join(pieces, feieSep))
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// feieCutSepBytes 两张票之间的落地开销:<BR> + <CUT> + <BR>。
// 切纸控制行自己也是一个 piece,故它前后各有一条 <BR> 分隔符。
const feieCutSepBytes = feieSepBytes + len(feieCutTag) + feieSepBytes

// feieTicketBytes 一张票的文本行在飞鹅 content 里的落地字节数(不含票间分隔)。
// 与 feieLineChunks 共用 feieSepBytes / feieLineCost,口径必然一致。
func feieTicketBytes(lines []string) int {
	n := 0
	for i, l := range lines {
		if i > 0 {
			n += feieSepBytes
		}
		n += feieLineCost(l)
	}
	return n
}

// packFeiePush 从 tickets[start:] 里装出一次推送,返回本次包含的票区间 [start, end) 与拼好的文本行。
//
// 装箱只在「票与票之间」切:一张票被切到两段里,后厨就会收到半张票 —— 那比多推一次更糟。
// 单张票自身就超过单次上限时(超长整单)它单独成箱,由 splitFeieContent 按行切,
// 这种长单本来就必须分成多单,与合并前的行为一致。
func packFeiePush(tickets [][]string, start int) (int, []string) {
	// 无论后面能装几张,第一张一定装得下(超长票也单独成箱),据此预分配避免逐票扩容。
	lines := make([]string, 0, len(tickets[start]))
	size := 0
	end := start
	for i := start; i < len(tickets); i++ {
		add := feieTicketBytes(tickets[i])
		if i > start {
			add += feieCutSepBytes
			if size+add > feie.MaxBytes {
				break
			}
			lines = append(lines, cutMarkLine()) // 票与票之间主动切纸
		}
		lines = append(lines, tickets[i]...)
		size += add
		end = i + 1
	}
	return end, lines
}

// ============================================================================
// 出纸确认(Open_queryOrderState)
// ============================================================================

// 飞鹅回执确认的节奏。声明为变量是为了让测试把等待压到毫秒级(真实打印机走纸
// 本来就要几秒,这些值只有在测试里需要被改写)。
var (
	// feieConfirmFirstDelay 首次确认前的等待。官方状态接口有约 5 秒缓存,查早了
	// 拿到的是推送瞬间的旧状态,必然误报「未打印」。
	feieConfirmFirstDelay = 8 * time.Second
	// feieConfirmRetryDelay 复核间隔:打印机走纸、或飞鹅向离线打印机补吐缓存任务,都要几秒。
	feieConfirmRetryDelay = 15 * time.Second
	// feieConfirmAttempts 含首次在内的确认次数(8s + 15s + 15s ≈ 38 秒的观察窗口)。
	feieConfirmAttempts = 3
)

// confirmFeiePrinted 对一次「飞鹅已受理」的推送做落地确认:回查云端订单状态,
// 若观察窗口内始终「未打印」,告警并把该次推送对应的台账行回写成失败。
//
// 为什么需要它:ret=0 只代表任务进了云端队列,打印机是否真出纸是另一回事 ——
// 缺纸/卡纸/离线时飞鹅会把任务缓存住,商家看到的就是「日志成功、纸没出来」。
// 受理 → 出纸之间的这段盲区,只有回执能填上。
//
// 确认失败时**不自动补打**,只回写台账:缓存中的任务在打印机恢复后会被飞鹅补吐,
// 贸然重推可能打两份。回写的 detail 里写明这一点,是否补打由商户决定。
//
// logIDs 是本次推送写下的台账主键(推送成功时才确认);客户端由调用方在派发时
// 构造好传入 —— 确认协程醒得晚,不该等它醒了再去碰全局连接与配置。
func confirmFeiePrinted(j job, labels []string, remoteID string, logIDs []int, client *feie.Client) {
	if remoteID == "" || client == nil {
		return
	}
	log := j.jobLog()
	sn := strings.TrimSpace(j.p.FeieSN)
	labelText := strings.Join(labels, "、")
	var lastRaw string
	for attempt := 1; attempt <= feieConfirmAttempts; attempt++ {
		if attempt == 1 {
			time.Sleep(feieConfirmFirstDelay)
		} else {
			time.Sleep(feieConfirmRetryDelay)
		}
		printed, raw, qerr := client.QueryOrderState(sn, remoteID)
		if strings.TrimSpace(raw) != "" {
			lastRaw = raw
		}
		if qerr != nil {
			log.Warnf("打印: 出纸确认查询失败 打印机[%s] %s[%s](云端单号 %s): %v%s",
				j.p.PrinterName, j.o.OrderNo, labelText, remoteID, qerr, feieDetailSuffix(j.p, raw))
			continue
		}
		if printed {
			log.Infof("打印: 出纸确认已打印 打印机[%s] %s[%s](云端单号 %s)",
				j.p.PrinterName, j.o.OrderNo, labelText, remoteID)
			return
		}
	}
	detail := fmt.Sprintf("飞鹅回执 %d 次确认均未打印(任务会缓存在飞鹅云端,打印机恢复后可能自动补打;如长时间未出纸请人工补打)", feieConfirmAttempts)
	if lastRaw != "" {
		detail += ";返回: " + truncate(lastRaw, 300)
	}
	log.Warnf("打印: %s 打印机[%s] %s[%s](云端单号 %s)", detail,
		j.p.PrinterName, j.o.OrderNo, labelText, remoteID)
	// 该次推送覆盖的每张票都回写成失败:商户在「打印记录」里直接看到红色失败项,
	// 而不是一条永远停在「成功」的假账。
	for _, id := range logIDs {
		service.UpdatePrintLogResult(id, po.PrintStatusFailed, detail, -1)
	}
}
