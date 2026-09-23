// Package dto 定义 API 出入参结构体与 PO 转换函数。依赖方向:dto 仅依赖 po;store 不得依赖本包。金额字段在本包一律以元(float64)表示,转换只发生在 FromPO/ToPO 内。
package dto

import "dining-system/internal/po"

// Table 对应 tb_table 表(API 出参)。
type Table struct {
	TableID    int     `json:"tableId"`
	TableNo    string  `json:"tableNo"`
	TableName  string  `json:"tableName"`
	Capacity   int     `json:"capacity"`
	Status     int     `json:"status"` // 0 空闲 1 占用
	SortOrder  int     `json:"sortOrder"`
	DelFlag    string  `json:"delFlag"`
	CreateBy   string  `json:"createBy"`
	CreateTime string  `json:"createTime"`
	UpdateBy   string  `json:"updateBy"`
	UpdateTime string  `json:"updateTime"`
	Remark     *string `json:"remark"`
	TableCode  string  `json:"tableCode"`
}

// FromTable 将持久化对象转为 API 出参。
func FromTable(p po.Table) Table {
	return Table{
		TableID:    p.TableID,
		TableNo:    p.TableNo,
		TableName:  p.TableName,
		Capacity:   p.Capacity,
		Status:     p.Status,
		SortOrder:  p.SortOrder,
		DelFlag:    p.DelFlag,
		CreateBy:   p.CreateBy,
		CreateTime: p.CreateTime,
		UpdateBy:   p.UpdateBy,
		UpdateTime: p.UpdateTime,
		Remark:     p.Remark,
		TableCode:  p.TableCode,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (t Table) ToPO() po.Table {
	return po.Table{
		TableID:    t.TableID,
		TableNo:    t.TableNo,
		TableName:  t.TableName,
		Capacity:   t.Capacity,
		Status:     t.Status,
		SortOrder:  t.SortOrder,
		DelFlag:    t.DelFlag,
		CreateBy:   t.CreateBy,
		CreateTime: t.CreateTime,
		UpdateBy:   t.UpdateBy,
		UpdateTime: t.UpdateTime,
		Remark:     t.Remark,
		TableCode:  t.TableCode,
	}
}
