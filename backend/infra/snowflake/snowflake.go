// Package snowflake 实现雪花算法(snowflake)的全局唯一 ID 生成。
//
// 用途:HTTP 中间件为每个请求生成 requestID;也可用于其它需要
// 「不依赖数据库、全局唯一、同节点内单调递增」的 ID 场景(如订单号)。
//
// 64 位布局(从左到右):
//
//	[1 位符号位(恒 0)] [41 位时间戳(毫秒,相对 epoch)] [10 位节点号] [12 位序列号]
//
// 容量:41 位时间戳相对 2024-01-01 纪元可用约 69 年;10 位节点号支持
// 最多 1024 个实例同时生成;单节点每毫秒最多 4096 个 ID。
//
// 依赖约束:本包不 import 任何项目内包(纯算法),否则会与 logger/middleware
// 形成循环依赖(logger 依赖 middleware、middleware 依赖本包)。节点号读取、
// 环境变量等装配逻辑由调用方(middleware 包)负责。
package snowflake

import (
	"fmt"
	"sync"
	"time"
)

const (
	// epochMs 自定义纪元:2024-01-01T00:00:00Z(毫秒)。比雪花算法的经典纪元
	// (2010-11-04)更贴近本项目启动时间,生成的 ID 数值更小、可用年限更长。
	epochMs int64 = 1704067200000

	nodeBits     = 10 // 节点号位数
	sequenceBits = 12 // 每毫秒序列号位数

	// MaxNodeID 节点号上限(2^10-1 = 1023),调用方校验节点号范围时使用。
	MaxNodeID int64 = 1<<nodeBits - 1
	// seqMask 序列号掩码(2^12-1 = 4095)。
	seqMask int64 = 1<<sequenceBits - 1

	// 各字段在 64 位整数中的偏移。
	timeShift = nodeBits + sequenceBits // 时间戳在高位,左移 22 位
	nodeShift = sequenceBits            // 节点号左移 12 位
)

// Node 是绑定固定节点号的雪花算法生成器,并发安全。
type Node struct {
	mu   sync.Mutex
	node int64
	last int64 // 最近一次生成的时间戳(相对 epoch 的毫秒数)
	seq  int64 // 当前毫秒内的序列号
}

// New 创建指定节点号的生成器,node 必须在 [0, 1023] 内。
//
// 多实例部署时每个实例必须分配不同节点号,否则可能产生重复 ID;
// 单实例部署用默认值 1 即可。
func New(node int64) (*Node, error) {
	if node < 0 || node > MaxNodeID {
		return nil, fmt.Errorf("snowflake: 节点号 %d 超出范围 [0, %d]", node, MaxNodeID)
	}
	return &Node{node: node}, nil
}

// Generate 生成下一个唯一 ID(正 int64,十进制 18~19 位)。
//
// 并发安全;同一节点内严格单调递增。时钟回拨(如 NTP 校时)时自旋等待
// 时钟追平再生成,期间阻塞调用方——正常场景只在毫秒级。
func (n *Node) Generate() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := n.curr()
	if now < n.last {
		// 时钟回拨:等到追平为止,保证同节点内时间戳不回退。
		now = n.waitUntil(n.last)
	}
	if now == n.last {
		n.seq = (n.seq + 1) & seqMask
		if n.seq == 0 {
			// 本毫秒 4096 个序列号耗尽,自旋到下一毫秒。
			now = n.waitUntil(n.last + 1)
		}
	} else {
		n.seq = 0
	}
	n.last = now

	return now<<timeShift | n.node<<nodeShift | n.seq
}

// Time 从 ID 还原其生成时间(UTC),用于排查「这个 ID 是什么时候生成的」。
func Time(id int64) time.Time {
	return time.UnixMilli(id>>timeShift + epochMs).UTC()
}

// curr 返回相对 epoch 的毫秒数。
func (n *Node) curr() int64 {
	return time.Now().UnixMilli() - epochMs
}

// waitUntil 阻塞直到相对时间不小于 target(时钟追平),返回追平后的当前值。
func (n *Node) waitUntil(target int64) int64 {
	for {
		if now := n.curr(); now >= target {
			return now
		}
		time.Sleep(time.Millisecond)
	}
}
