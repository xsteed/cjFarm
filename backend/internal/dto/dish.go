package dto

import "dining-system/internal/po"

// Dish 对应 tb_dish 表(API 出参)。CategoryName 与 Specs 为运行时补算,不入库。
type Dish struct {
	DishID       int     `json:"dishId"`
	CategoryID   int     `json:"categoryId"`
	CategoryName string  `json:"categoryName"`
	DishName     string  `json:"dishName"`
	DishImage    string  `json:"dishImage"`
	Description  string  `json:"description"`
	Status       int     `json:"status"` // 1 上架 0 下架
	SortOrder    int     `json:"sortOrder"`
	DelFlag      string  `json:"delFlag"`
	CreateBy     string  `json:"createBy"`
	CreateTime   string  `json:"createTime"`
	UpdateBy     string  `json:"updateBy"`
	UpdateTime   string  `json:"updateTime"`
	Remark       *string `json:"remark"`
	Specs        []Spec  `json:"specs"`
}

// FromDish 将持久化对象与联表补算结果组装为 API 出参。
func FromDish(p po.Dish, categoryName string, specs []Spec) Dish {
	return Dish{
		DishID:       p.DishID,
		CategoryID:   p.CategoryID,
		CategoryName: categoryName,
		DishName:     p.DishName,
		DishImage:    p.DishImage,
		Description:  p.Description,
		Status:       p.Status,
		SortOrder:    p.SortOrder,
		DelFlag:      p.DelFlag,
		CreateBy:     p.CreateBy,
		CreateTime:   p.CreateTime,
		UpdateBy:     p.UpdateBy,
		UpdateTime:   p.UpdateTime,
		Remark:       p.Remark,
		Specs:        specs,
	}
}

// ToPO 将 API 出参转回持久化对象,CategoryName/Specs 不入库。
func (d Dish) ToPO() po.Dish {
	return po.Dish{
		DishID:      d.DishID,
		CategoryID:  d.CategoryID,
		DishName:    d.DishName,
		DishImage:   d.DishImage,
		Description: d.Description,
		Status:      d.Status,
		SortOrder:   d.SortOrder,
		DelFlag:     d.DelFlag,
		CreateBy:    d.CreateBy,
		CreateTime:  d.CreateTime,
		UpdateBy:    d.UpdateBy,
		UpdateTime:  d.UpdateTime,
		Remark:      d.Remark,
	}
}
