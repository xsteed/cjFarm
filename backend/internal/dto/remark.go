package dto

import "dining-system/internal/po"

// Remark 对应 tb_remark 表(菜品备注选项,API 出参)。
type Remark struct {
	RemarkID   int    `json:"remarkId"`
	OptionName string `json:"optionName"`
	SortOrder  int    `json:"sortOrder"`
	DelFlag    string `json:"delFlag"`
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`
}

// FromRemark 将持久化对象转为 API 出参。
func FromRemark(p po.Remark) Remark {
	return Remark{
		RemarkID:   p.RemarkID,
		OptionName: p.OptionName,
		SortOrder:  p.SortOrder,
		DelFlag:    p.DelFlag,
		CreateTime: p.CreateTime,
		UpdateTime: p.UpdateTime,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (r Remark) ToPO() po.Remark {
	return po.Remark{
		RemarkID:   r.RemarkID,
		OptionName: r.OptionName,
		SortOrder:  r.SortOrder,
		DelFlag:    r.DelFlag,
		CreateTime: r.CreateTime,
		UpdateTime: r.UpdateTime,
	}
}
