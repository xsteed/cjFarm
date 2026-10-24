package dto

import "dining-system/domain/po"

// Dish 对应 tb_dish 表(API 出参)。CategoryName 与 Specs 为运行时补算,不入库。
type Dish struct {
	DishID       int     `json:"dishId"`
	CategoryID   int     `json:"categoryId"`
	CategoryName string  `json:"categoryName"`
	DishName     string  `json:"dishName"`
	DishImage    string  `json:"dishImage"`
	Description  string  `json:"description"`
	Status       int     `json:"status"`    // 1 上架 0 下架
	SoldOut      int     `json:"soldOut"`   // 0 未售罄 1 售罄
	Signature    int     `json:"signature"` // 0 普通 1 招牌菜
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
		SoldOut:      p.SoldOut,
		Signature:    p.Signature,
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

// DishMove 菜品拖拽排序入参:把 DishID 移动到 PrevDishID 之后 / NextDishID 之前。
// 两者都为 0 表示移动到分类末尾。CategoryID 是落点所属分类:与菜品当前分类不同
// 即为「拖进别的分类」(同步改 category_id),缺省保持原分类。
type DishMove struct {
	DishID     int `json:"dishId"`
	CategoryID int `json:"categoryId"`
	PrevDishID int `json:"prevDishId"`
	NextDishID int `json:"nextDishId"`
}

// DishSort 拖拽排序结果:菜品 ID 与本轮重排后的排序值。
type DishSort struct {
	DishID    int `json:"dishId"`
	SortOrder int `json:"sortOrder"`
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
		SoldOut:     d.SoldOut,
		Signature:   d.Signature,
		SortOrder:   d.SortOrder,
		DelFlag:     d.DelFlag,
		CreateBy:    d.CreateBy,
		CreateTime:  d.CreateTime,
		UpdateBy:    d.UpdateBy,
		UpdateTime:  d.UpdateTime,
		Remark:      d.Remark,
	}
}
