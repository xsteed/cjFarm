package store

import "dining-system/internal/model"

// DishCols 菜品列表查询的 SELECT 列(含分类名,需 FROM tb_dish d LEFT JOIN tb_category c)。
const DishCols = `d.dish_id, d.category_id, COALESCE(c.category_name,''), d.dish_name, d.dish_image,
	d.description, d.status, d.sort_order, d.del_flag, d.create_by, d.create_time, d.update_by, d.update_time, d.remark`

// ScanDish 扫描一行菜品记录(含分类名,不含规格)。
func ScanDish(rows interface{ Scan(...interface{}) error }) (model.Dish, error) {
	var d model.Dish
	err := rows.Scan(&d.DishID, &d.CategoryID, &d.CategoryName, &d.DishName, &d.DishImage,
		&d.Description, &d.Status, &d.SortOrder, &d.DelFlag, &d.CreateBy, &d.CreateTime, &d.UpdateBy, &d.UpdateTime, &d.Remark)
	return d, err
}

// LoadDishSpecs 加载单道菜品的规格。
func LoadDishSpecs(dishID int) []model.Spec {
	rows, err := DB.Query(`SELECT spec_id, dish_id, spec_name, price FROM tb_spec WHERE dish_id=? ORDER BY spec_id`, dishID)
	if err != nil {
		return []model.Spec{}
	}
	defer rows.Close()
	specs := []model.Spec{}
	for rows.Next() {
		var s model.Spec
		var priceCents int64
		rows.Scan(&s.SpecID, &s.DishID, &s.SpecName, &priceCents)
		s.Price = model.ToYuan(priceCents)
		specs = append(specs, s)
	}
	return specs
}

// LoadSpecsByDishIDs 批量加载多道菜品的规格,避免 N+1 查询。
func LoadSpecsByDishIDs(ids []int) map[int][]model.Spec {
	out := map[int][]model.Spec{}
	if len(ids) == 0 {
		return out
	}
	placeholders := ""
	args := make([]interface{}, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := DB.Query(`SELECT spec_id, dish_id, spec_name, price FROM tb_spec WHERE dish_id IN (`+placeholders+`) ORDER BY dish_id, spec_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s model.Spec
		var priceCents int64
		rows.Scan(&s.SpecID, &s.DishID, &s.SpecName, &priceCents)
		s.Price = model.ToYuan(priceCents)
		out[s.DishID] = append(out[s.DishID], s)
	}
	return out
}
