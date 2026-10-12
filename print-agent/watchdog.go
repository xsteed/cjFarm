// 进程稳定性兜底:看门狗 + 活动戳。
//
// 进程级守护(launchd KeepAlive / Windows 任务计划 RestartOnFailure /
// systemd Restart=always)只对「进程退出」生效——程序自己退出的、被杀的、
// panic 崩溃的都能被拉起;唯独「假死」(进程活着但主循环卡住不干活)守护
// 无从察觉。这里补一条看门狗:主循环每轮打一次活动戳,超过 stuckMaxIdle
// 仍无活动即判定假死,主动以非零码退出,交给外部守护拉起。
//
// 阈值取值:主循环在**每条任务处理完**、每轮取单前后都会打活动戳(见 runCycle /
// runLoop),所以「长时间无活动」确实等于卡死。
//
// 注意不能按「一轮最坏耗时」来定阈值:一轮要串行处理 limit 条任务,每条还可能
// copies 份;走 CUPS 时每次 lp 调用最坏 30s(cups.go 的 cupsExecTimeout),
// 默认 limit=10 就已经到 5 分钟,limit=50/copies>1 时远超。
// 阈值定得比这个还小,就会把正常干活的进程判成假死自杀 —— 而它手里正握着
// 尚未回执的 claimed 任务,自杀 → 租约到期 → 云端重发 → 已经出过的纸再打一遍。
package main

import (
	"os"
	"sync/atomic"
	"time"
)

// stuckMaxIdle 判定假死的最大空闲时长。
//
// 取 30 分钟:远大于「单条任务最坏耗时」(CUPS 30s × 份数上限 20 = 10 分钟),
// 因此不会误杀;又足够短,真卡死时能在守护重启周期内自愈。
const stuckMaxIdle = 30 * time.Minute

// lastActive 主循环最近一次活动的时间戳。
//
// 存 time.Time 而不是 UnixNano:time.Now() 带单调时钟读数,time.Since 会优先用它,
// 于是 NTP 校时/人工改时间(墙钟前跳)不会把「一切正常的进程」判成假死 ——
// 早先按墙钟比较时,一次向前跳 5 分钟就足以触发自杀。
// atomic.Pointer 保证主循环与看门狗 goroutine 之间的可见性。
var lastActive atomic.Pointer[time.Time]

// markActive 记录一次主循环活动(每条任务处理后、每轮取单后都会调用)。
func markActive() {
	t := time.Now()
	lastActive.Store(&t)
}

// startWatchdog 启动假死看门狗 goroutine:每分钟检查一次活动戳,
// 超过 stuckMaxIdle 无活动则打日志并以退出码 1 自杀,由外部守护拉起。
//
// 参数是「广播式」的退出信号(见 broadcastSignals 的说明):**不能**把 signal 通道
// 直接传给多个消费者,通道是单播的,看门狗抢到就没人通知主循环了。
func startWatchdog(done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				last := lastActive.Load()
				if last == nil || time.Since(*last) > stuckMaxIdle {
					logf("[崩溃] 看门狗判定进程假死(超过 %s 无活动),主动退出交由守护拉起", stuckMaxIdle)
					os.Exit(1)
				}
			}
		}
	}()
}
