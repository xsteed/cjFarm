package main

import (
	"os"
	"testing"
	"time"
)

func TestWdMarkActive(t *testing.T) {
	lastActiveNano.Store(0)
	markActive()
	if lastActiveNano.Load() <= 0 {
		t.Fatal("markActive 后 lastActiveNano 应大于 0")
	}
}

func TestWdStartWatchdogExitsOnStop(t *testing.T) {
	stop := make(chan os.Signal, 1)
	startWatchdog(stop)

	// 立即关闭停止通道,goroutine 应从 select 的 stop 分支正常退出,不触发 os.Exit。
	close(stop)
	// 留一点时间让 goroutine 调度退出;若走到 os.Exit 分支测试进程会直接退出。
	time.Sleep(10 * time.Millisecond)
}

// TestEnableUTF8ConsoleNoop 非 Windows 下该函数为空实现,冒烟调用确认不 panic。
func TestEnableUTF8ConsoleNoop(t *testing.T) {
	enableUTF8Console()
}
