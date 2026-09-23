//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableUTF8Console 把控制台代码页切到 UTF-8(CP65001),避免中文日志乱码;
// 仅当 stdout 是终端时才有意义,失败静默忽略(重定向到文件时本就按 UTF-8 写)。
func enableUTF8Console() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")

	var mode uint32
	h := syscall.Handle(os.Stdout.Fd())
	if r, _, _ := getConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return
	}
	_, _, _ = setConsoleOutputCP.Call(65001)
}
