package main

import (
	"os"
	"testing"
	"time"
)

func TestWdMarkActive(t *testing.T) {
	lastActive.Store(nil)
	markActive()
	got := lastActive.Load()
	if got == nil || got.IsZero() {
		t.Fatal("markActive 后活动戳应为非零时间")
	}
	// 存的是 time.Time(带单调时钟读数)而不是墙钟纳秒:看门狗据此判定假死时,
	// NTP 校时/人工改时间前跳不会把正常进程误判成卡死。
	if time.Since(*got) > time.Minute {
		t.Fatalf("活动戳不应是一个明显过旧的时间: %v", got)
	}
}

// TestWdStuckThresholdCoversWorstCase 看门狗阈值必须覆盖「单条任务最坏耗时」。
//
// 早先阈值是 5 分钟,而一轮要串行处理 limit(≤50) 条任务、每条 copies 份,走 CUPS 时
// 每次 lp 调用最坏 30 秒 —— 正常干活的进程会在第 5 分钟被判定假死自杀,而它手里正握着
// 尚未回执的 claimed 任务:租约到期后云端重发,已经出过的纸再打一遍。
func TestWdStuckThresholdCoversWorstCase(t *testing.T) {
	worstPerJob := cupsExecTimeout * time.Duration(maxJobCopies)
	if stuckMaxIdle <= worstPerJob {
		t.Fatalf("stuckMaxIdle(%s) 必须大于单条任务最坏耗时(%s × %d 份 = %s)",
			stuckMaxIdle, cupsExecTimeout, maxJobCopies, worstPerJob)
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

// TestBroadcastSignalsForceExitsOnSecond 第二个退出信号要强制退出。
//
// 第一个信号走「本轮结束后优雅退出」,在途的长轮询(pull 最长 hold 30s 上下)与正在
// 打印的任务不会被打断 —— 这是为了不制造「未回执 → 租约到期重发 → 重复出纸」。
// 但运维不能被卡住:连按两次 Ctrl-C、或 stop.sh 再次发信号时要立刻退出。
// 少了这条路径,外部只能升级为 pkill -KILL 硬杀,反而正好杀在打印中途。
func TestBroadcastSignalsForceExitsOnSecond(t *testing.T) {
	// 覆盖必须在启动 goroutine 之前:forceExit 是普通变量,先写后起协程才有
	// happens-before 关系,否则 -race 会判数据竞争。
	forced := make(chan struct{})
	orig := forceExit
	forceExit = func() { close(forced) }
	defer func() { forceExit = orig }()

	sig := make(chan os.Signal, 2)
	done := broadcastSignals(sig)
	sig <- os.Interrupt
	<-done
	// done 必须保持已关闭状态,且不因第二个信号 panic(close 只能一次)。
	sig <- os.Interrupt
	select {
	case <-forced:
	case <-time.After(time.Second):
		t.Fatal("第二个退出信号应触发强制退出")
	}

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
