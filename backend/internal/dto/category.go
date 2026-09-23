package dto

import "dining-system/internal/po"

// Category 对应 tb_category 表(API 出参)。
type Category struct {
	CategoryID   int    `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	SortOrder    int    `json:"sortOrder"`
	DelFlag      string `json:"delFlag"`
	CreateTime   string `json:"createTime"`
	UpdateTime   string `json:"updateTime"`
}

// FromCategory 将持久化对象转为 API 出参。
func FromCategory(p po.Category) Category {
	return Category{
		CategoryID:   p.CategoryID,
		CategoryName: p.CategoryName,
		SortOrder:    p.SortOrder,
		DelFlag:      p.DelFlag,
		CreateTime:   p.CreateTime,
		UpdateTime:   p.UpdateTime,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (c Category) ToPO() po.Category {
	return po.Category{
		CategoryID:   c.CategoryID,
		CategoryName: c.CategoryName,
		SortOrder:    c.SortOrder,
		DelFlag:      c.DelFlag,
		CreateTime:   c.CreateTime,
		UpdateTime:   c.UpdateTime,
	}
}
