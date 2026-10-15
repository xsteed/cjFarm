package po

import "strings"

// 打印机接入方式。
const (
	PrinterProviderTCP  = "tcp"  // 网络热敏机: 后端拼 ESC/POS 走 IP:9100
	PrinterProviderFeie = "feie" // 飞鹅云打印机: 后端走开放平台 HTTP 接口推单
	// PrinterProviderAgent 本地打印代理: 后端只把票据入队,由门店内网常驻的
	// 代理程序出站拉单后再向 IP:9100 直发。后端部署在云服务器时,这是
	// 复用门店已有网络打印机的唯一通道(tcp 通道在云端够不到门店内网)。
	// 完整部署说明见 docs/print-agent.md。
	PrinterProviderAgent = "agent"
)

// 打印机类型。
const (
	PrinterTypeKitchen = 1 // 厨房单
	PrinterTypeGuest   = 2 // 顾客小票
)

// Printer 对应 tb_printer 表。
type Printer struct {
	PrinterID   int
	PrinterName string
	PrinterType int    // 1=厨房单 2=顾客小票
	Provider    string // tcp=网络直连 / feie=飞鹅云
	IP          string
	Port        int
	FeieSN      string // 飞鹅打印机编号(机身标签上的 SN)
	PaperWidth  int    // 32=58mm 48=80mm
	Copies      int    // 打印份数(1-5)
	// CategoryIDs 按菜品分类分单:CSV 分类 ID,空串表示收全部菜品。
	// 只有厨房单有意义(顾客小票必须含全部菜品,否则金额对不上)。
	CategoryIDs string
	Status      int // 0=停用 1=启用
	DelFlag     string
	CreateTime  string
	UpdateTime  string
}

// IsFeie 报告该打印机是否走飞鹅云接口。
func (p *Printer) IsFeie() bool { return p.Provider == PrinterProviderFeie }

// IsAgent 报告该打印机是否走本地打印代理(云后端入队 + 门店代理取单)。
func (p *Printer) IsAgent() bool { return p.Provider == PrinterProviderAgent }

// IsDirect 报告该打印机是否由后端直连 IP:9100 发送(要求后端与打印机同局域网)。
func (p *Printer) IsDirect() bool { return !p.IsFeie() && !p.IsAgent() }

// EffectiveCopies 返回实际打印份数(兜底 1,上限 5,避免误填 100 份把纸打光)。
func (p *Printer) EffectiveCopies() int {
	if p.Copies < 1 {
		return 1
	}
	if p.Copies > 5 {
		return 5
	}
	return p.Copies
}

// ---- 飞鹅 SN 规范化(保存前与老库升级共用同一套口径) ----

// NormalizeFeieSN 规范化飞鹅打印机编号:去掉首尾空白与机身标签上的前缀。
//
// 机身标签印的是「SN 961828266」,商户照抄进输入框就带着前缀入库。飞鹅接口按 SN 精确
// 匹配,带着前缀推过去等于推给一台不存在的机器,而飞鹅很可能仍返回 ret=0 ——
// 表现就是「日志显示成功、纸却没出来」,事后极难排查。
// 只剥标签前缀、不做剔字符:SN 本体里有异常字符时交给 IsFeieSN 明确报错,
// 免得把商户真实填写的编号悄悄改坏。
func NormalizeFeieSN(sn string) string {
	s := strings.TrimSpace(sn)
	if len(s) >= 2 && strings.EqualFold(s[:2], "sn") {
		if rest := strings.TrimSpace(strings.TrimLeft(s[2:], " :：\t")); rest != "" {
			s = rest
		}
	}
	return strings.TrimSpace(s)
}

// IsFeieSN 校验飞鹅 SN 形态:只允许数字/字母(机身标签通常是 9 位数字)。
// 带空格、冒号或中文时直接拒绝 —— 这类脏值会被飞鹅按「不存在的编号」处理。
func IsFeieSN(sn string) bool {
	if sn == "" {
		return false
	}
	for _, r := range sn {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}
