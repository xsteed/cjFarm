// 飞鹅云打印通道(发送层胶水)。
//
// 职责切分:
//   - 接口协议本身在 infrastructure/adapter/feie(外部服务适配器:签名、调用、解码);
//   - 本文件只做两件事:
//     1) 从系统配置构造适配器客户端(商户改配置即刻生效,无需重启);
//     2) 把渲染好的文本行按飞鹅通道的排版规则拼装、分段后推给适配器。
//
// 票据排版:content 用 <BR> 换行,官方另支持 <C></C> 居中、<B></B> 放大、
// <QR></QR> 二维码等标签。本实现只做「纯文本行 + <BR>」——排版已在
// ticket.go 里用等宽对齐算好,套标签反而会把列对齐打乱。
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

// splitFeieContent 把文本行拼成 content,并按字节上限切分。
func splitFeieContent(lines []string) []string {
	out := []string{}
	cur := ""
	for _, l := range lines {
		// 二维码标记行用飞鹅的 <QR> 标签落地(前提是官方支持的标签),
		// 不能走纯文本分支——否则会把控制前缀原样打到纸上。
		piece := escapeFeie(l)
		if qr, ok := SplitQRLine(l); ok {
			piece = "<QR>" + escapeFeie(qr) + "</QR>"
		}
		next := piece
		if cur != "" {
			next = cur + "<BR>" + piece
		}
		if len(next) > feie.MaxBytes && cur != "" {
			out = append(out, cur)
			cur = piece
			continue
		}
		cur = next
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}
