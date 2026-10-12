// ESC/POS 直连发送器。
//
// 通过 TCP 连接打印机的 IP:Port(默认 9100),发送 ESC/POS 指令打印单据。
// 中文统一按 GBK 编码(常见国产热敏打印机默认支持 GBK/GB18030)。
//
// 适用面:后端与打印机在同一个局域网。一旦后端部署到云服务器,这条路径
// 就够不到门店打印机了 —— 那种场景请把打印机配成飞鹅云打印(见 feie.go)。
package print

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
)

// ValidatePrinterAddr 校验打印机目标地址,防止把打印功能当 SSRF 跳板:
//   - IP 必须合法;
//   - 禁止回环/未指定/链路本地(单播+组播)地址——覆盖 127.0.0.1、0.0.0.0、169.254.x.x(含云元数据 169.254.169.254);
//   - 端口必须为正整数且不大于 65535。
//
// 内网地址(10.x/172.16-31.x/192.168.x)是打印机的正常所在,故默认放行;
// 若打印机网段固定,可在部署侧进一步用 CIDR 白名单收紧。
func ValidatePrinterAddr(ip string, port int) error {
	p := net.ParseIP(strings.TrimSpace(ip))
	if p == nil {
		return errors.New("打印机 IP 非法")
	}
	if p.IsLoopback() || p.IsUnspecified() {
		return errors.New("禁止使用回环或未指定地址作为打印机 IP")
	}
	if p.IsLinkLocalUnicast() || p.IsLinkLocalMulticast() {
		return errors.New("禁止使用链路本地地址(含云元数据地址)作为打印机 IP")
	}
	if port <= 0 || port > 65535 {
		return errors.New("打印机端口非法(1-65535)")
	}
	return nil
}

// gbk 将 UTF-8 字符串编码为 GBK 字节。
func gbk(s string) []byte {
	enc := simplifiedchinese.GBK.NewEncoder()
	out, _, err := transform.Bytes(enc, []byte(s))
	if err == nil {
		return out
	}

	// GBK 无法表示 emoji、生僻字等字符。失败时不能回退 UTF-8 字节,
	// 否则打印机会按 GBK 解码出乱码;改为逐字替换不可映射字符。
	fallback := make([]byte, 0, len(s))
	replaced := false
	for _, r := range s {
		part, _, rerr := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(string(r)))
		if rerr != nil {
			fallback = append(fallback, '?')
			replaced = true
			continue
		}
		fallback = append(fallback, part...)
	}
	if replaced {
		logger.Warnf("打印: 文本行含 GBK 无法表示的字符,已替换为 ?: %q", truncateRunesForLog(s, 80))
	}
	return fallback
}

func truncateRunesForLog(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// EncodeTicket 把渲染好的文本行组装成 ESC/POS 字节流(含初始化、GBK 文本、切纸)。
//
// 导出是为了让「本地打印代理」通道复用同一份协议实现:代理通道下字节在
// 取单接口里现场编码后以 base64 下发,代理程序因此完全不需要理解打印协议。
// 见 agent.go 的设计说明。
func EncodeTicket(lines []string) []byte { return encodeESCPOS(lines) }

// encodeESCPOS 组装一份单据的 ESC/POS 字节流,入参为已对齐/折行后的文本行。
//
// 强调行(店名/标题/合计,见 BoldLine)用 ESC E 重打加粗:重打不占额外宽度,
// 与渲染层的等宽对齐完全兼容。放大行(夹单桌号,见 SplitBigLine)单独走 GS !。
func encodeESCPOS(lines []string) []byte {
	var b []byte
	b = append(b, 0x1B, 0x40) // ESC @ 初始化
	for _, ln := range lines {
		if qr, ok := SplitQRLine(ln); ok {
			b = append(b, encodeQR(qr)...)
			continue
		}
		if big, ok := SplitBigLine(ln); ok {
			b = append(b, encodeBigLine(big)...)
			continue
		}
		bold := BoldLine(ln)
		if bold {
			b = append(b, 0x1B, 0x45, 0x01) // ESC E 1 开启加粗
		}
		b = append(b, gbk(ln)...)
		b = append(b, '\n')
		if bold {
			b = append(b, 0x1B, 0x45, 0x00) // ESC E 0 关闭加粗
		}
	}
	b = append(b, 0x1D, 0x56, 0x42, 0x00) // GS V m n: 切纸(部分切)
	return b
}

// encodeBigLine 输出一行「放大居中」文本:GS ! 0x11 让字符倍宽倍高,ESC a 1 居中。
//
// 每行打完立即还原字号与对齐 —— 不还原的话后面所有行都会变成双倍宽,
// 渲染层辛苦算好的等宽对齐全废。GS ! 是 Epson 系通用指令,国产热敏机普遍兼容;
// 万一某机型不支持,最坏结果是该行以正常字号左对齐打出,不会乱码。
func encodeBigLine(text string) []byte {
	b := make([]byte, 0, len(text)*2+16)
	b = append(b, 0x1B, 0x61, 0x01) // ESC a 1:居中
	b = append(b, 0x1D, 0x21, 0x11) // GS ! 0x11:倍宽 + 倍高
	b = append(b, gbk(text)...)
	b = append(b, '\n')
	b = append(b, 0x1D, 0x21, 0x00) // GS ! 0x00:还原字号
	b = append(b, 0x1B, 0x61, 0x00) // ESC a 0:还原左对齐
	return b
}

// qrModuleSize 二维码模块大小(点)。6 在 58mm(384 点)与 80mm(576 点)
// 纸上都能稳定扫出,再大在 58mm 上会超出可打印宽度。
const qrModuleSize = 0x06

// qrMaxDataBytes 二维码数据区上限(字节)。
//
// GS ( k 的容量与机型、纠错级别相关,约 700 字节上下;超限时指令会被打印机
// 忽略甚至吐乱码。小票二维码只放一个短链接,远够用,超限就干脆不出码。
const qrMaxDataBytes = 700

// encodeQR 生成一段 ESC/POS 二维码指令(GS ( k 系列)。
//
// 交给打印机内置的二维码生成器,而不是后端算位图:后端算位图要引入额外依赖,
// 且各家位图指令(GS v 0 / GS ( L / ESC *)差异比分歧更大的 GS ( k 还大;
// GS ( k 是 Epson 系通用指令,国产热敏机基本都兼容。
func encodeQR(content string) []byte {
	data := []byte(content)
	if len(data) == 0 || len(data) > qrMaxDataBytes {
		return nil
	}
	// 存数据指令的长度字段 = cn + fn + m + 数据字节数
	n := len(data) + 3
	b := make([]byte, 0, n+32)
	b = append(b, 0x1B, 0x61, 0x01)                                        // ESC a 1:居中
	b = append(b, 0x1D, 0x28, 0x6B, 0x04, 0x00, 0x31, 0x41, 0x32, 0x00)    // GS ( k:选 QR 模型 2
	b = append(b, 0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x43, qrModuleSize)  // GS ( k:模块大小
	b = append(b, 0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x45, 0x33)          // GS ( k:纠错等级 H(48+3)
	b = append(b, 0x1D, 0x28, 0x6B, byte(n), byte(n>>8), 0x31, 0x50, 0x30) // GS ( k:写入数据
	b = append(b, data...)
	b = append(b, 0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x51, 0x30) // GS ( k:打印
	b = append(b, 0x1B, 0x61, 0x00)                               // ESC a 0:还原左对齐
	b = append(b, '\n')
	return b
}

// sendViaTCP 向指定打印机发送 ESC/POS 指令。
//
// 连接前做地址校验兜底:即使数据在写入前被绕过 handler 直接入库,
// 也不会发起敏感连接。
func sendViaTCP(p po.Printer, lines []string) (string, error) {
	ip := strings.TrimSpace(p.IP)
	if ip == "" {
		return "", errors.New("打印机 IP 为空")
	}
	port := p.Port
	if port <= 0 {
		port = 9100
	}
	if err := ValidatePrinterAddr(ip, port); err != nil {
		return "", err
	}
	data := encodeESCPOS(lines)
	// 份数在直连场景靠「重复发送同一份指令」实现(ESC/POS 没有联数指令)。
	for i := 0; i < p.EffectiveCopies(); i++ {
		addr := net.JoinHostPort(ip, strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			return "", err
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, werr := conn.Write(data)
		_ = conn.Close()
		if werr != nil {
			return "", werr
		}
	}
	return addrOf(ip, port), nil
}

// addrOf 返回「IP:端口」用于日志展示。
func addrOf(ip string, port int) string {
	return net.JoinHostPort(ip, strconv.Itoa(port))
}

// ProbeTCP 只做一次 TCP 连通性探测,用于「测试连接」——不落地任何打印内容。
func ProbeTCP(p po.Printer) error {
	ip := strings.TrimSpace(p.IP)
	port := p.Port
	if port <= 0 {
		port = 9100
	}
	if err := ValidatePrinterAddr(ip, port); err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", addrOf(ip, port), 3*time.Second)
	if err != nil {
		return fmt.Errorf("无法连接 %s: %v", addrOf(ip, port), err)
	}
	_ = conn.Close()
	return nil
}
