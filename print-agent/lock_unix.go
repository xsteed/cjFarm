//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// acquireInstanceLock 对 path 加「非阻塞独占文件锁」,实现单实例互斥。
//
// 为什么需要:后端取单有 60 秒租约,租约内不会重复下发;但若两个实例同时常驻,
// A 取单后打印超时(>60s),租约过期,B 会把同一条任务再取一次 —— 同一张小票
// 打两遍。两个实例的 print-agent.state 也可能不同步,本地去重一并失效。
//
// 返回 (file, true, nil) 表示拿到锁,file 必须持有到进程退出(内核随进程回收,
// 不会留下需要人工清理的陈旧锁);(nil, false, nil) 表示已有实例持有。
func acquireInstanceLock(path string) (*os.File, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// EWOULDBLOCK(=EAGAIN)表示锁已被其它进程持有,属正常竞争而非错误。
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}
