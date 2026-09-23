package main

import (
	"encoding/json"
	"testing"
)

func TestValidatePrinterAddr(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		port    int
		wantMsg string // 为空表示应当放行
	}{
		{name: "loopback v4", ip: "127.0.0.1", port: 9100, wantMsg: "禁止使用回环或未指定地址作为打印机 IP"},
		{name: "unspecified v4", ip: "0.0.0.0", port: 9100, wantMsg: "禁止使用回环或未指定地址作为打印机 IP"},
		{name: "cloud metadata", ip: "169.254.169.254", port: 9100, wantMsg: "禁止使用链路本地地址(含云元数据地址)作为打印机 IP"},
		{name: "link local v6", ip: "fe80::1", port: 9100, wantMsg: "禁止使用链路本地地址(含云元数据地址)作为打印机 IP"},
		{name: "invalid ip", ip: "abc", port: 9100, wantMsg: "打印机 IP 非法"},
		{name: "port zero", ip: "192.168.1.8", port: 0, wantMsg: "打印机端口非法(1-65535)"},
		{name: "port too large", ip: "192.168.1.8", port: 70000, wantMsg: "打印机端口非法(1-65535)"},

		{name: "lan v4", ip: "192.168.1.8", port: 9100},
		{name: "lan v4 2", ip: "10.0.0.5", port: 9000},
		{name: "normal v6", ip: "2001:db8::1", port: 9100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePrinterAddr(tt.ip, tt.port)
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("validatePrinterAddr(%q, %d)=%v, want nil", tt.ip, tt.port, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validatePrinterAddr(%q, %d)=nil, want %q", tt.ip, tt.port, tt.wantMsg)
			}
			if err.Error() != tt.wantMsg {
				t.Fatalf("validatePrinterAddr(%q, %d)=%q, want %q", tt.ip, tt.port, err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestPingBodyContainsBuildVersion(t *testing.T) {
	orig := version
	version = "1.0.0"
	defer func() { version = orig }()

	raw, err := json.Marshal(pingBody("agent-1"))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["buildVersion"] != "1.0.0" {
		t.Fatalf("buildVersion=%v, want 1.0.0", body["buildVersion"])
	}
	// version(协议版本)与 buildVersion(软件版本)是两个不同字段,不能混用。
	if body["version"] != float64(agentProtocolVersion) {
		t.Fatalf("version=%v, want %d", body["version"], agentProtocolVersion)
	}
}

func TestPullBodyContainsBuildVersion(t *testing.T) {
	orig := version
	version = "2.3.4"
	defer func() { version = orig }()

	raw, err := json.Marshal(pullBody("agent-2", 10, 25))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["buildVersion"] != "2.3.4" {
		t.Fatalf("buildVersion=%v, want 2.3.4", body["buildVersion"])
	}
	if body["version"] != float64(agentProtocolVersion) {
		t.Fatalf("version=%v, want %d", body["version"], agentProtocolVersion)
	}
}
