package dao

import (
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// 分类仓储封装分类相关数据库操作。

// ListCategories 查询未删除的分类列表。
func ListCategories() ([]po.Category, error) {
	list := []po.Category{}
	rows, err := store.DB.Query(`SELECT category_id, category_name, sort_order, del_flag, create_time, update_time
		FROM tb_category WHERE del_flag='0' ORDER BY sort_order, category_id`)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var ct po.Category
		rows.Scan(&ct.CategoryID, &ct.CategoryName, &ct.SortOrder, &ct.DelFlag, &ct.CreateTime, &ct.UpdateTime)
		list = append(list, ct)
	}
	return list, nil
}

// InsertCategory 新增分类并返回自增 ID。
func InsertCategory(ct po.Category) (int64, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_category(category_name, sort_order, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?)`, ct.CategoryName, ct.SortOrder, "0", store.Now(), store.Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// UpdateCategory 更新分类信息。
func UpdateCategory(ct po.Category) error {
	_, err := store.DB.Exec(`UPDATE tb_category SET category_name=?, sort_order=?, update_time=? WHERE category_id=?`,
		ct.CategoryName, ct.SortOrder, store.Now(), ct.CategoryID)
	return err
}

// DeleteCategory 软删除分类。
func DeleteCategory(id int) error {
	_, err := store.DB.Exec(`UPDATE tb_category SET del_flag='1', update_time=? WHERE category_id=?`, store.Now(), id)
	return err
}

// CategoryNameMapByIDs 按分类 ID 批量查询分类名称,返回 id→名称 映射。
// 打印机列表用它一次性补齐所有打印机的分类名,避免逐台查询(N+1)。
func CategoryNameMapByIDs(ids []int) map[int]string {
	out := map[int]string{}
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
	rows, err := store.DB.Query(`SELECT category_id, category_name FROM tb_category WHERE category_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		out[id] = name
	}
	return out
}
