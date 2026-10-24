//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// acquireInstanceLock 用「独占打开」实现单实例互斥。
//
// Windows 没有 flock,但 CreateFile 的共享模式传 0 即可:第二个实例打开同一个
// 文件会得到 ERROR_SHARING_VIOLATION。进程退出时句柄关闭、锁自动释放,
// 不会留下需要人工清理的陈旧锁文件。
//
// 返回 (file, true, nil) 表示拿到锁;(nil, false, nil) 表示已有实例持有。
func acquireInstanceLock(path string) (*os.File, bool, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	h, err := syscall.CreateFile(p,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // 不共享:第二个实例会失败
		nil,
		syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		// ERROR_SHARING_VIOLATION = 32。syscall 包没有导出这个常量(在 x/sys/windows
		// 里),这里直接按数值比较,避免为一行判断引入新依赖。
		const errSharingViolation = syscall.Errno(32)
		if errors.Is(err, errSharingViolation) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return os.NewFile(uintptr(h), path), true, nil
}
