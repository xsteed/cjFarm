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
// 内容超过单次上限时按行拆成多单依次推送;任一段失败即中止并返回已累计的云端单号。
func sendViaFeie(c *feie.Client, p po.Printer, lines []string) (string, error) {
	sn := strings.TrimSpace(p.FeieSN)
	if sn == "" {
		return "", newPermError("未填写飞鹅打印机编号(SN)")
	}
	times := p.EffectiveCopies()
	chunks := splitFeieContent(lines)
	lastID := ""
	for i, chunk := range chunks {
		id, err := c.PrintMsg(sn, chunk, times)
		if err != nil {
			if i > 0 {
				// 前几段已经送出去了,把云端单号带上,便于商家判断「打了一半」。
				return lastID, fmt.Errorf("第 %d/%d 段推送失败: %w", i+1, len(chunks), err)
			}
			return "", err
		}
		lastID = id
	}
	return lastID, nil
}

// escapeFeie 转义文本里的尖括号。
// 飞鹅 content 是「标签 + 文本」混排的,菜名或备注里一旦出现 < 会被当成标签解析,
// 官方文档也要求把 < > 转义为实体。
func escapeFeie(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// feieLinePiece 一行文本在飞鹅 content 里的落地形态:
// 控制行(二维码/放大桌号)按各自的配对标签落地,加粗强调行套 <BOLD>,其余是转义后的纯文本。
func feieLinePiece(l string) string {
	if qr, ok := SplitQRLine(l); ok {
		return "<QR>" + escapeFeie(qr) + "</QR>"
	}
	if big, ok := SplitBigLine(l); ok {
		// 放大行(夹单桌号)正是需要 <B> 倍宽的场景:内容短、居中,不存在
		// 等宽列对齐问题(常规正文行才刻意避开 <B>,见文件头注释)。
		return "<C><B>" + escapeFeie(big) + "</B></C>"
	}
	if BoldLine(l) {
		// <BOLD> 是官方「字体加粗」,不改变字符宽度 —— 与 ESC/POS 的 ESC E 重打
		// 等价,等宽列对齐不受影响。不要用 <B>:它的官方语义是「放大一倍」。
		return "<BOLD>" + escapeFeie(l) + "</BOLD>"
	}
	return escapeFeie(l)
}

// feieLineChunks 飞鹅云通道的行分段区间:单次推送的字节上限。
//
// 必须按「落地后的字节数」而非原始文本长度来累计:<BR> 比换行符长 3 字节,
// <QR>/<C><B> 标签与尖括号转义也会把一行撑大 —— 按原始长度算会算错切点。
func feieLineChunks(lines []string) []chunkSpan {
	return lineChunks(lines, feie.MaxBytes, len("<BR>"), func(l string) int { return len(feieLinePiece(l)) })
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
		out = append(out, strings.Join(pieces, "<BR>"))
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}
