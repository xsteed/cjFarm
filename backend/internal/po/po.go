// Package po 定义与数据表一一对应的持久化对象、领域常量与金额换算工具。本包不依赖任何其他内部包。
package po

// 软删除标记取值（tb_* 表 del_flag 列）。
const (
	DelFlagOK      = "0" // 正常
	DelFlagDeleted = "1" // 已删除
)
