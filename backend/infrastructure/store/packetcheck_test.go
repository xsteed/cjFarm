package store

import (
	"path/filepath"
	"testing"

	"dining-system/infrastructure/conf"
)

// TestVerifyMaxAllowedPacketSQLiteNoop 确认 SQLite 后端下自检直接跳过:
// SQLite 没有 max_allowed_packet 概念,不应发起该查询,更不应 panic 影响启动。
func TestVerifyMaxAllowedPacketSQLiteNoop(t *testing.T) {
	t.Setenv(conf.EnvUploadDir, filepath.Join(t.TempDir(), "uploads"))
	Init(filepath.Join(t.TempDir(), "packet.db"))
	t.Cleanup(func() { _ = DB.Close() })
	if IsMySQL() {
		t.Fatal("测试环境应为 SQLite 后端")
	}
	verifyMaxAllowedPacket() // 不应 panic
}

// TestRequiredMaxAllowedPacketCoversImage 阈值必须留出裕量:
// 单图上限 64MB,一条 INSERT 除图片字节外还有语句/协议开销,阈值应不小于 128MB
// (即单图上限的两倍),以覆盖开销并与客户端 DSN 的 maxAllowedPacket 默认值一致。
func TestRequiredMaxAllowedPacketCoversImage(t *testing.T) {
	const imageLimit = 64 << 20
	if requiredMaxAllowedPacket < 2*imageLimit {
		t.Fatalf("requiredMaxAllowedPacket=%d 应不小于单图上限的两倍(%d)", requiredMaxAllowedPacket, 2*imageLimit)
	}
}
