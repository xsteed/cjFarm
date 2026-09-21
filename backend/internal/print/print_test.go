package print

import "testing"

func TestValidatePrinterAddr(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		port    int
		wantErr bool
	}{
		{"局域网打印机正常", "192.168.1.100", 9100, false},
		{"私网10段正常", "10.0.0.5", 9100, false},
		{"公网IP正常", "8.8.8.8", 9100, false},
		{"IPv4回环拒绝", "127.0.0.1", 9100, true},
		{"IPv6回环拒绝", "::1", 9100, true},
		{"未指定0.0.0.0拒绝", "0.0.0.0", 9100, true},
		{"云元数据地址拒绝", "169.254.169.254", 80, true},
		{"链路本地IPv6拒绝", "fe80::1", 9100, true},
		{"非法IP拒绝", "not-an-ip", 9100, true},
		{"空IP拒绝", "", 9100, true},
		{"带空格IP拒绝", "  127.0.0.1  ", 9100, true},
		{"端口0拒绝", "192.168.1.1", 0, true},
		{"端口越界拒绝", "192.168.1.1", 65536, true},
		{"负端口拒绝", "192.168.1.1", -1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePrinterAddr(c.ip, c.port)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePrinterAddr(%q,%d) err=%v, wantErr=%v", c.ip, c.port, err, c.wantErr)
			}
		})
	}
}
