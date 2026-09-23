// 进程稳定性兜底:看门狗 + 活动戳。
//
// 进程级守护(launchd KeepAlive / Windows 任务计划 RestartOnFailure /
// systemd Restart=always)只对「进程退出」生效——程序自己退出的、被杀的、
// panic 崩溃的都能被拉起;唯独「假死」(进程活着但主循环卡住不干活)守护
// 无从察觉。这里补一条看门狗:主循环每轮打一次活动戳,超过 stuckMaxIdle
// 仍无活动即判定假死,主动以非零码退出,交给外部守护拉起。
//
// 阈值取值:打印任务全部带 5s 拨号/写入超时,长轮询最多 hold 30s,
// 一轮最坏情况也就分把钟 —— 5 分钟无活动只可能是进程卡死。
package main

import (
	"os"
	"sync/atomic"
	"time"
)

// stuckMaxIdle 判定假死的最大空闲时长。
const stuckMaxIdle = 5 * time.Minute

// lastActiveNano 主循环最近一次完成一轮的时间戳(纳秒)。
// atomic 保证主循环与看门狗 goroutine 之间的可见性。
var lastActiveNano atomic.Int64

// markActive 记录一次主循环活动(每轮 runCycle 完成后调用)。
func markActive() {
	lastActiveNano.Store(time.Now().UnixNano())
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
				last := lastActiveNano.Load()
				if last == 0 || time.Since(time.Unix(0, last)) > stuckMaxIdle {
					logf("[崩溃] 看门狗判定进程假死(超过 %s 无活动),主动退出交由守护拉起", stuckMaxIdle)
					os.Exit(1)
				}
			}
		}
	}()
}
