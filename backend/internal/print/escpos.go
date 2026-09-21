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

	"dining-system/internal/model"
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
	if err != nil {
		return []byte(s)
	}
	return out
}

// encodeESCPOS 组装一份单据的 ESC/POS 字节流,入参为已对齐/折行后的文本行。
func encodeESCPOS(lines []string) []byte {
	var b []byte
	b = append(b, 0x1B, 0x40) // ESC @ 初始化
	for _, ln := range lines {
		b = append(b, gbk(ln)...)
		b = append(b, '\n')
	}
	b = append(b, 0x1D, 0x56, 0x42, 0x00) // GS V m n: 切纸(部分切)
	return b
}

// sendViaTCP 向指定打印机发送 ESC/POS 指令。
//
// 连接前做地址校验兜底:即使数据在写入前被绕过 handler 直接入库,
// 也不会发起敏感连接。
func sendViaTCP(p model.Printer, lines []string) (string, error) {
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
func ProbeTCP(p model.Printer) error {
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
