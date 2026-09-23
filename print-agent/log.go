package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// 日志输出:stdout + 可选日志文件双写。
//
// stdout 服务于前台调试与 launchd/systemd 的采集;日志文件服务于
// Windows 任务计划(SYSTEM 账号运行,stdout 无人接收)与事后排障。
// 门店排障统一看日志文件(默认 --install 时写程序同目录 print-agent.log)。

var (
	logMu   sync.Mutex
	logFile *os.File
	// verbose 是否输出调试级日志。常驻模式下每 3 秒就是一轮,空闲心跳这类
	// 高频信息默认关掉,只在排障(--verbose / --once)时打开。
	verbose bool
)

// setVerbose 开关调试级日志。
func setVerbose(on bool) {
	verbose = on
}

// logvf 输出调试级日志(带 [调试] 前缀);非 verbose 模式直接丢弃。
func logvf(format string, a ...interface{}) {
	if !verbose {
		return
	}
	logf("[调试] "+format, a...)
}

// initLogFile 打开日志文件(追加写,0644)。路径为空表示不落盘。
// 失败返回错误,由调用方降级为仅控制台并告警——日志文件是旁路,不能挡住启动。
func initLogFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	logMu.Lock()
	logFile = f
	logMu.Unlock()
	return nil
}

// logf 输出带时间戳的日志:先控制台、再日志文件(打开失败时仅控制台)。
func logf(format string, a ...interface{}) {
	line := fmt.Sprintf("%s "+format+"\n", append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, a...)...)
	fmt.Print(line)
	logMu.Lock()
	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
	logMu.Unlock()
}

// fatalf 打印错误并退出(非 0 退出码让守护视为失败并触发重启)。
func fatalf(format string, a ...interface{}) {
	logf("[错误] "+format, a...)
	os.Exit(1)
}
