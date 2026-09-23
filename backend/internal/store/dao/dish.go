package dao

import (
	"database/sql"
	"strings"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// DishCols 菜品列表查询的 SELECT 列(含分类名,需 FROM tb_dish d LEFT JOIN tb_category c)。
const DishCols = `d.dish_id, d.category_id, COALESCE(c.category_name,''), d.dish_name, d.dish_image,
	d.description, d.status, d.sort_order, d.del_flag, d.create_by, d.create_time, d.update_by, d.update_time, d.remark`

// DishRow 菜品+联表分类名的查询投影。
type DishRow struct {
	po.Dish
	CategoryName string
}

// ScanDish 扫描一行菜品记录(含分类名,不含规格)。
func ScanDish(rows interface{ Scan(...interface{}) error }) (DishRow, error) {
	var d DishRow
	err := rows.Scan(&d.DishID, &d.CategoryID, &d.CategoryName, &d.DishName, &d.DishImage,
		&d.Description, &d.Status, &d.SortOrder, &d.DelFlag, &d.CreateBy, &d.CreateTime, &d.UpdateBy, &d.UpdateTime, &d.Remark)
	return d, err
}

// DishQuery 菜品列表筛选条件;零值字段表示不限。
type DishQuery struct {
	DishName   string // 菜品名称模糊匹配
	CategoryID string // 分类 ID(字符串直接绑定)
}

// ListDishes 按条件分页查询菜品列表(不含规格)。
func ListDishes(q DishQuery, pageNum, pageSize int) (total int, list []DishRow, err error) {
	where := []string{"d.del_flag='0'"}
	args := []interface{}{}
	if q.DishName != "" {
		where = append(where, "d.dish_name LIKE ?")
		args = append(args, "%"+q.DishName+"%")
	}
	if q.CategoryID != "" {
		where = append(where, "d.category_id=?")
		args = append(args, q.CategoryID)
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	// COUNT 失败必须上抛:total 恒 0 会让分页在前端显示「共 0 条」,与列表实际有数据矛盾。
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_dish d`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	rows, err := store.DB.Query(`SELECT `+DishCols+`
		FROM tb_dish d LEFT JOIN tb_category c ON d.category_id=c.category_id`+cond+` ORDER BY d.sort_order, d.dish_id LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return total, nil, err
	}
	defer rows.Close()
	list = []DishRow{}
	for rows.Next() {
		if d, err := ScanDish(rows); err == nil {
			list = append(list, d)
		}
	}
	return total, list, nil
}

// ListEnabledDishes 查询已启用且未删除的菜品列表(不含规格)。
func ListEnabledDishes() ([]DishRow, error) {
	rows, err := store.DB.Query(`SELECT ` + DishCols + `
		FROM tb_dish d LEFT JOIN tb_category c ON d.category_id=c.category_id WHERE d.del_flag='0' AND d.status=1 ORDER BY d.sort_order, d.dish_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []DishRow{}
	for rows.Next() {
		if d, err := ScanDish(rows); err == nil {
			list = append(list, d)
		}
	}
	return list, nil
}

// GetDishByID 按 ID 查询单道菜品(不含规格)。
func GetDishByID(id int) (DishRow, error) {
	return ScanDish(store.DB.QueryRow(`SELECT `+DishCols+`
		FROM tb_dish d LEFT JOIN tb_category c ON d.category_id=c.category_id WHERE d.dish_id=?`, id))
}

// CreateDish 创建菜品并写入规格。
func CreateDish(d po.Dish, specs []po.Spec) (int64, error) {
	var dishID int64
	err := store.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?)`, d.CategoryID, d.DishName, d.DishImage, d.Description, d.Status, d.SortOrder, "0", store.Now(), store.Now())
		if err != nil {
			return err
		}
		dishID, err = res.LastInsertId()
		if err != nil {
			return err
		}
		for _, s := range specs {
			if _, err := tx.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price) VALUES(?,?,?)`, dishID, s.SpecName, s.Price); err != nil {
				return err
			}
		}
		return nil
	})
	return dishID, err
}

// UpdateDish 更新菜品并重建规格。
func UpdateDish(d po.Dish, specs []po.Spec) error {
	return store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_dish SET category_id=?, dish_name=?, dish_image=?, description=?, status=?, sort_order=?, update_time=? WHERE dish_id=?`,
			d.CategoryID, d.DishName, d.DishImage, d.Description, d.Status, d.SortOrder, store.Now(), d.DishID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tb_spec WHERE dish_id=?`, d.DishID); err != nil {
			return err
		}
		for _, s := range specs {
			if _, err := tx.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price) VALUES(?,?,?)`, d.DishID, s.SpecName, s.Price); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteDish 软删除菜品。
func DeleteDish(id int) error {
	_, err := store.DB.Exec(`UPDATE tb_dish SET del_flag='1', update_time=? WHERE dish_id=?`, store.Now(), id)
	return err
}

// GetSpecForOrder 查询订单明细对应的菜品与规格信息。
func GetSpecForOrder(specID, dishID int) (dishName, specName, delFlag string, dishStatus int, priceCents int64, err error) {
	err = store.DB.QueryRow(`SELECT d.dish_name, d.status, d.del_flag, s.spec_name, s.price
		FROM tb_spec s JOIN tb_dish d ON d.dish_id = s.dish_id
		WHERE s.spec_id=? AND s.dish_id=?`, specID, dishID).
		Scan(&dishName, &dishStatus, &delFlag, &specName, &priceCents)
	return
}

// SpecOrderInfo 订单明细校验所需的菜品与规格信息(与 GetSpecForOrder 返回内容一致)。
type SpecOrderInfo struct {
	SpecID     int
	DishID     int
	SpecName   string
	PriceCents int64
	DishName   string
	DishStatus int
	DelFlag    string
}

// LoadSpecsForOrder 按规格 ID 批量加载订单明细对应的菜品与规格信息,key 为 spec_id。
// spec_id 是 tb_spec 主键,全局唯一,故可按 spec_id IN 一次查出;是否属于请求的
// dish_id 由调用方在内存里比对(等价于 GetSpecForOrder 的 s.spec_id=? AND s.dish_id=?),
// 避免逐条 GetSpecForOrder 在单次下单(最多 50 条)时退化成 50 次查询。
func LoadSpecsForOrder(specIDs []int) (map[int]SpecOrderInfo, error) {
	out := map[int]SpecOrderInfo{}
	if len(specIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]interface{}, 0, len(specIDs))
	for i, id := range specIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := store.DB.Query(`SELECT s.spec_id, s.dish_id, s.spec_name, s.price, d.dish_name, d.status, d.del_flag
		FROM tb_spec s JOIN tb_dish d ON d.dish_id = s.dish_id
		WHERE s.spec_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var info SpecOrderInfo
		if err := rows.Scan(&info.SpecID, &info.DishID, &info.SpecName, &info.PriceCents,
			&info.DishName, &info.DishStatus, &info.DelFlag); err != nil {
			return nil, err
		}
		out[info.SpecID] = info
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadDishSpecs 加载单道菜品的规格。
func LoadDishSpecs(dishID int) []po.Spec {
	rows, err := store.DB.Query(`SELECT spec_id, dish_id, spec_name, price FROM tb_spec WHERE dish_id=? ORDER BY spec_id`, dishID)
	if err != nil {
		return []po.Spec{}
	}
	defer rows.Close()
	specs := []po.Spec{}
	for rows.Next() {
		var s po.Spec
		rows.Scan(&s.SpecID, &s.DishID, &s.SpecName, &s.Price)
		specs = append(specs, s)
	}
	return specs
}

// LoadSpecsByDishIDs 批量加载多道菜品的规格,避免 N+1 查询。
func LoadSpecsByDishIDs(ids []int) map[int][]po.Spec {
	out := map[int][]po.Spec{}
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
	rows, err := store.DB.Query(`SELECT spec_id, dish_id, spec_name, price FROM tb_spec WHERE dish_id IN (`+placeholders+`) ORDER BY dish_id, spec_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s po.Spec
		rows.Scan(&s.SpecID, &s.DishID, &s.SpecName, &s.Price)
		out[s.DishID] = append(out[s.DishID], s)
	}
	return out
}
