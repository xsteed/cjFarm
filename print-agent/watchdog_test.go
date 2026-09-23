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
	done := make(chan struct{})
	startWatchdog(done)

	// 立即关闭退出通道,goroutine 应从 select 的 done 分支正常退出,不触发 os.Exit。
	close(done)
	// 留一点时间让 goroutine 调度退出;若走到 os.Exit 分支测试进程会直接退出。
	time.Sleep(10 * time.Millisecond)
}

// TestBroadcastSignalsReachesAllConsumers 钉住「退出信号必须广播」这条。
//
// 曾经的写法是把同一个 signal 通道同时交给主循环与看门狗 —— 通道是**单播**的,
// 看门狗先取走信号后只是安静 return,主循环永远收不到,于是 SIGTERM/SIGINT 之后
// 进程根本不退出:stop.sh 报「已停止」而代理还在跑,残留实例会与自启动的那个
// 抢同一批任务,把同一张小票打两遍。
func TestBroadcastSignalsReachesAllConsumers(t *testing.T) {
	sig := make(chan os.Signal, 1)
	done := broadcastSignals(sig)
	sig <- os.Interrupt

	// 两个消费者都必须醒 —— 共用通道时第二个必然收不到,这正是本用例要挡的回归。
	for i := 1; i <= 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("第 %d 个消费者没收到退出广播(共用通道时看门狗会抢走信号)", i)
		}
	}
}

// TestBroadcastSignalsIgnoresExtraSignals 多来几个信号也不该 panic(close 只能一次)。
func TestBroadcastSignalsIgnoresExtraSignals(t *testing.T) {
	sig := make(chan os.Signal, 1)
	done := broadcastSignals(sig)
	sig <- os.Interrupt
	<-done
	sig <- os.Interrupt // 第二个信号由 broadcastSignals 的 goroutine 收掉后退出
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done 应保持已关闭状态")
	}
}

// TestEnableUTF8ConsoleNoop 非 Windows 下该函数为空实现,冒烟调用确认不 panic。
func TestEnableUTF8ConsoleNoop(t *testing.T) {
	enableUTF8Console()
}
