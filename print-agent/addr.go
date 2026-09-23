package main

import (
	"errors"
	"net"
	"strings"
)

// validatePrinterAddr 校验打印机目标地址,防止被篡改的打印任务把代理变成内网扫描/写入跳板:
//   - IP 必须合法;
//   - 禁止回环/未指定/链路本地(单播+组播)地址——覆盖 127.0.0.1、0.0.0.0、169.254.x.x(含云元数据 169.254.169.254);
//   - 端口必须为 1-65535。
//
// 内网地址(10.x/172.16-31.x/192.168.x)是打印机的正常所在,故默认放行;
// 若打印机网段固定,可在部署侧进一步用 CIDR 白名单收紧。
//
// 规则与 backend/internal/print/escpos.go:ValidatePrinterAddr 对齐,改动需同步。
func validatePrinterAddr(ip string, port int) error {
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
