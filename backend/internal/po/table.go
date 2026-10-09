package po

// 桌台人工状态(lock_status):与订单自动流转的占用状态(Status)相互独立。
//   - 正常:可正常扫码点餐;
//   - 停用:设备/区域不可用(如维修),拒绝新点餐;
//   - 预约:已被预订,拒绝路人扫码点餐,等预订客到店后由店员恢复「正常」再下单。
const (
	TableLockNormal   = 0 // 正常
	TableLockDisabled = 1 // 停用(不可用)
	TableLockReserved = 2 // 预约
)

// ValidTableLockStatus 判定桌台人工状态取值是否合法。
func ValidTableLockStatus(s int) bool {
	return s >= TableLockNormal && s <= TableLockReserved
}

// Table 对应 tb_table 表。
type Table struct {
	TableID   int
	TableNo   string
	TableName string
	Capacity  int
	Status    int // 0 空闲 1 占用(由订单自动流转,勿手工置位)
	// LockStatus/LockRemark 为人工设置的可用性状态:0 正常 1 停用 2 预约,与 Status 正交。
	LockStatus int
	LockRemark string
	SortOrder  int
	DelFlag    string
	CreateBy   string
	CreateTime string
	UpdateBy   string
	UpdateTime string
	Remark     *string
	// TableCode 桌台二维码稳定码:创建时生成且永不变更,二维码内容使用该码,
	// 保证「一次印刷长期有效」,同时避免自增 ID 被枚举遍历。
	TableCode string
}
