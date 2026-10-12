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
	// 断电可能把最后一行写成半截。半截行不是一个真实的幂等号,而且危害不只是
	// 「多一条垃圾记录」:后续 append 会与它粘成一行(abcd<newid>\n),新记录反而
	// 没被独立记录 —— 该单在下次重启后失去去重,可能被重打一次。
	// 因此不仅要丢弃它,还要把它**从文件里摘掉**(否则下次加载又会把它的前缀
	// 当有效记录读进来)。
	truncated := false
	if data, err := os.ReadFile(path); err == nil {
		text := string(data)
		if text != "" && !strings.HasSuffix(text, "\n") {
			truncated = true
			if i := strings.LastIndexByte(text, '\n'); i >= 0 {
				text = text[:i+1]
			} else {
				text = ""
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				s.seen[line] = struct{}{}
				s.order = append(s.order, line)
			}
		}
	}
	if truncated {
		// 用原子重写摘掉残行:失败也不影响后续追加(残行最多留到下次启动)。
		if werr := writeStateFileAtomically(path, s.order); werr != nil {
			logf("[警告] 修复状态文件残行失败 %s: %v(下次启动会再试)", path, werr)
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
		// 错误必须告警,不能丢弃:调用方紧接着就会回执成功,而「先落盘再回执」这条
		// 不重复出票的承诺正是靠这一步。静默失败的后果是断电/重启后该单被重打一遍,
		// 且现场没有任何线索。
		if _, werr := s.f.WriteString(id + "\n"); werr != nil {
			logf("[警告] 去重记录写入失败 %s: %v(该单在重启后可能被重打一次,请检查磁盘)", s.path, werr)
		} else if serr := s.f.Sync(); serr != nil {
			logf("[警告] 去重记录落盘失败 %s: %v(断电后该单可能被重打一次)", s.path, serr)
		}
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

	// 关键:先原子重写成功、再动现有句柄。
	// 旧实现是「先 Close + 置 nil,再 os.WriteFile」——os.WriteFile 内部是 O_TRUNC,
	// 一打开就截断:磁盘满 / I/O 错误时文件被清成 0 字节,历史幂等号全部丢失,
	// 而 s.f 已被置 nil,本进程此后**再也不落盘**(只剩第一句告警),可谓最坏组合。
	if err := writeStateFileAtomically(s.path, keep); err != nil {
		logf("[警告] 状态文件重写失败 %s: %v(继续用原文件追加去重记录,请关注磁盘状态)", s.path, err)
		return
	}
	// 重写成功:旧句柄指向的是已被 rename 掉的旧 inode,必须换到新文件上继续追加。
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		// 重写成功但追加句柄打不开:此后新增的幂等号只留内存、不落盘,
		// 重启后会丢失 —— 必须留痕,不能静默。
		logf("[警告] 状态文件重写后无法继续追加 %s: %v(此间新增记录重启后将丢失,可能重复出纸)", s.path, err)
		return
	}
	s.f = f
}

// writeStateFileAtomically 用「临时文件 + fsync + rename」原子替换状态文件。
//
// 直接 os.WriteFile 会在写之前就把目标截断(O_TRUNC),中途失败就得到一个空文件 ——
// 而去重记录一旦丢失,门店重启后云端重发旧任务就会重复出纸。rename 在同一文件系统内
// 是原子的,读者要么看到旧内容、要么看到新内容,不存在「被清空」的中间态。
func writeStateFileAtomically(path string, keep []string) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(strings.Join(keep, "\n") + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
