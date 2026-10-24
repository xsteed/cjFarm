package service

import (
	"errors"
	"fmt"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// ============ 菜品 / 分类 / 备注 ============
//
// 菜单域业务逻辑:菜品 CRUD 与规格加载、分类管理、备注管理。
// handler 只负责参数解析与响应组装,筛选条件、校验与 PO/DTO 转换统一收口于此。

// DishList 分页查询菜品列表,批量补齐规格后组装为 API 出参。
func DishList(dishName, categoryID string, pageNum, pageSize int) (int, []dto.Dish, error) {
	total, rows, err := dao.ListDishes(dao.DishQuery{
		DishName:   dishName,
		CategoryID: categoryID,
	}, pageNum, pageSize)
	if err != nil {
		return 0, nil, err
	}
	ids := []int{}
	for _, d := range rows {
		ids = append(ids, d.DishID)
	}
	specMap, err := dao.LoadSpecsByDishIDs(ids)
	if err != nil {
		return 0, nil, err
	}
	out := make([]dto.Dish, 0, len(rows))
	for _, row := range rows {
		specs := make([]dto.Spec, 0, len(specMap[row.DishID]))
		for _, s := range specMap[row.DishID] {
			specs = append(specs, dto.FromSpec(s))
		}
		out = append(out, dto.FromDish(row.Dish, row.CategoryName, specs))
	}
	return total, out, nil
}

// DishGet 按 ID 查询单道菜品并补齐规格。
func DishGet(id int) (dto.Dish, error) {
	row, err := dao.GetDishByID(id)
	if err != nil {
		return dto.Dish{}, err
	}
	poSpecs, err := dao.LoadDishSpecs(row.DishID)
	if err != nil {
		return dto.Dish{}, err
	}
	specs := make([]dto.Spec, 0, len(poSpecs))
	for _, s := range poSpecs {
		specs = append(specs, dto.FromSpec(s))
	}
	return dto.FromDish(row.Dish, row.CategoryName, specs), nil
}

// SaveDish 校验并新增菜品(含规格),返回菜品 ID。
func SaveDish(d dto.Dish) (int64, error) {
	if d.CategoryID == 0 || strings.TrimSpace(d.DishName) == "" {
		return 0, errors.New("请选择分类并填写菜品名称")
	}
	if len(d.Specs) == 0 {
		return 0, errors.New("请至少添加一个规格")
	}
	specs := make([]po.Spec, 0, len(d.Specs))
	for _, s := range d.Specs {
		specs = append(specs, s.ToPO())
	}
	return dao.CreateDish(d.ToPO(), specs)
}

// UpdateDish 校验并更新菜品(含规格重建)。
func UpdateDish(d dto.Dish) error {
	if d.CategoryID == 0 || strings.TrimSpace(d.DishName) == "" {
		return errors.New("请选择分类并填写菜品名称")
	}
	if len(d.Specs) == 0 {
		return errors.New("请至少添加一个规格")
	}
	specs := make([]po.Spec, 0, len(d.Specs))
	for _, s := range d.Specs {
		specs = append(specs, s.ToPO())
	}
	return dao.UpdateDish(d.ToPO(), specs)
}

// DeleteDish 软删除菜品;删除失败向上透传,避免接口恒报「删除成功」。
func DeleteDish(id int) error {
	return dao.DeleteDish(id)
}

// CategoryList 查询未删除的分类列表。
func CategoryList() ([]dto.Category, error) {
	rows, err := dao.ListCategories()
	if err != nil {
		return nil, err
	}
	out := make([]dto.Category, 0, len(rows))
	for _, ct := range rows {
		out = append(out, dto.FromCategory(ct))
	}
	return out, nil
}

// SaveCategory 校验并新增分类,返回分类 ID。
func SaveCategory(ct dto.Category) (int64, error) {
	if strings.TrimSpace(ct.CategoryName) == "" {
		return 0, errors.New("请填写分类名称")
	}
	return dao.InsertCategory(ct.ToPO())
}

// UpdateCategory 校验并更新分类;写库失败向上透传,避免接口恒报「修改成功」。
func UpdateCategory(ct dto.Category) error {
	if strings.TrimSpace(ct.CategoryName) == "" {
		return errors.New("请填写分类名称")
	}
	return dao.UpdateCategory(ct.ToPO())
}

// DeleteCategory 软删除分类;删除失败向上透传,避免接口恒报「删除成功」。
//
// 删除前拦截「分类下还有菜品」:顾客端菜单按分类分组构建,分类被删后其下菜品会
// 从菜单里静默消失(命中不了分组即被丢弃),而下单校验只查菜品自身的 del_flag,
// 旧客户端/缓存仍能点进来,形成「菜单上看不到、却能下单」的割裂;管理端列表用
// LEFT JOIN,分类名变空串,看起来「还在」,问题极难定位。与 DeleteTable 同口径:
// 先清引用再删本体。
func DeleteCategory(id int) error {
	n, err := dao.CountActiveDishes(id)
	if err != nil {
		return fmt.Errorf("检查分类下菜品失败: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("该分类下还有 %d 道菜品,请先移动或删除菜品后再删除分类", n)
	}
	return dao.DeleteCategory(id)
}

// RemarkList 查询未删除的备注列表。
func RemarkList() ([]dto.Remark, error) {
	rows, err := dao.ListRemarks()
	if err != nil {
		return nil, err
	}
	out := make([]dto.Remark, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.FromRemark(r))
	}
	return out, nil
}

// SaveRemark 校验并新增备注,返回备注 ID。
func SaveRemark(r dto.Remark) (int64, error) {
	if strings.TrimSpace(r.OptionName) == "" {
		return 0, errors.New("请填写备注名称")
	}
	return dao.InsertRemark(r.ToPO())
}

// UpdateRemark 校验并更新备注;写库失败向上透传,避免接口恒报「修改成功」。
func UpdateRemark(r dto.Remark) error {
	if strings.TrimSpace(r.OptionName) == "" {
		return errors.New("请填写备注名称")
	}
	return dao.UpdateRemark(r.ToPO())
}

// DeleteRemark 软删除备注;删除失败向上透传,避免接口恒报「删除成功」。
func DeleteRemark(id int) error {
	return dao.DeleteRemark(id)
}
