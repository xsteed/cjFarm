package main

import (
	"os"
	"strings"
	"sync"
)

// ============================================================================
// 本地去重状态(幂等投递)
// ============================================================================
//
// 目的:把「回执丢失 → 云端重发 → 同一张票打两遍」从「可能发生」变成「基本不可能」。
// 原理:每条任务带一个永不变更的 deliveryId,代理打印成功后立刻把它写进本地状态文件
// 并 fsync,之后才回执。下次取到相同 deliveryId(说明上次回执丢了、任务被重发),
// 直接跳过打印、回执成功即可。
//
// 为什么落盘而不是放内存:代理进程可能被重启/断电,内存记录会丢;落盘才能在重启后
// 依然去重。残余窗口只剩「打印成功 → 写盘」之间的毫秒级断电,这已是无法在应用层
// 进一步消除的极限(再往下需要打印机端的事务回执,不现实)。

// jobStateMaxKeep 状态文件最多保留的幂等号条数。超出后截断最老的一半,
// 避免门店长期运行后文件无界增长;10000 条足够覆盖「一周内的回执重试」。
const jobStateMaxKeep = 10000

// jobState 已成功打印的幂等号集合(带持久化)。
type jobState struct {
	mu    sync.Mutex
	path  string
	f     *os.File
	seen  map[string]struct{}
	order []string // 按写入顺序,用于超限时截断最老的
}

// loadJobState 从文件加载去重状态并保持追加句柄。
// 文件不可写时降级为纯内存(仍然工作,只是重启后失去去重能力),并打一条告警。
func loadJobState(path string) *jobState {
	s := &jobState{path: path, seen: map[string]struct{}{}}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				s.seen[line] = struct{}{}
				s.order = append(s.order, line)
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		logf("[警告] 状态文件不可写(%v),重启后将失去重复打印去重能力", err)
		return s
	}
	s.f = f
	return s
}

// has 报告某幂等号是否已在本机成功打印过。
func (s *jobState) has(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[id]
	return ok
}

// markDone 记录一次成功打印:追加写文件并 fsync(先落盘,调用方才回执)。
func (s *jobState) markDone(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return
	}
	s.seen[id] = struct{}{}
	s.order = append(s.order, id)
	if s.f != nil {
		_, _ = s.f.WriteString(id + "\n")
		_ = s.f.Sync() // 落盘后再回执,断电也不丢
	}
	s.maybeCompactLocked()
}

// maybeCompactLocked 超出上限时截断最老的一半并整体重写(调用方须持有锁)。
func (s *jobState) maybeCompactLocked() {
	if len(s.order) <= jobStateMaxKeep {
		return
	}
	keep := s.order[len(s.order)-jobStateMaxKeep/2:]
	s.order = keep
	s.seen = make(map[string]struct{}, len(keep))
	for _, k := range keep {
		s.seen[k] = struct{}{}
	}
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	if err := os.WriteFile(s.path, []byte(strings.Join(keep, "\n")+"\n"), 0o600); err == nil {
		if f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			s.f = f
		}
	}
}
