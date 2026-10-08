package dao

import (
	"database/sql"
	"errors"
	"strings"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ErrSoldOut 限量规格库存不足：并发下单把库存抢光时用于回滚整单。
var ErrSoldOut = errors.New("库存不足，该菜品已售罄")

// DishCols 菜品列表查询的 SELECT 列(含分类名,需 FROM tb_dish d LEFT JOIN tb_category c)。
const DishCols = `d.dish_id, d.category_id, COALESCE(c.category_name,''), d.dish_name, d.dish_image,
	d.description, d.status, d.sold_out, d.sort_order, d.del_flag, d.create_by, d.create_time, d.update_by, d.update_time, d.remark`

// DishRow 菜品+联表分类名的查询投影。
type DishRow struct {
	po.Dish
	CategoryName string
}

// ScanDish 扫描一行菜品记录(含分类名,不含规格)。
func ScanDish(rows interface{ Scan(...interface{}) error }) (DishRow, error) {
	var d DishRow
	err := rows.Scan(&d.DishID, &d.CategoryID, &d.CategoryName, &d.DishName, &d.DishImage,
		&d.Description, &d.Status, &d.SoldOut, &d.SortOrder, &d.DelFlag, &d.CreateBy, &d.CreateTime, &d.UpdateBy, &d.UpdateTime, &d.Remark)
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

// normalizeStock 把「写入口」的库存归一化：<=0（未设置 / 负数 / 0）一律按「不限量」-1 存。
// 库存 0（售罄）只能由下单扣减产生；店长或旧调用方传一个零值不能意外把规格做成售罄。
func normalizeStock(stock int64) int64 {
	if stock <= 0 {
		return -1
	}
	return stock
}

// CreateDish 创建菜品并写入规格。
func CreateDish(d po.Dish, specs []po.Spec) (int64, error) {
	var dishID int64
	err := store.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, description, status, sold_out, sort_order, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, d.CategoryID, d.DishName, d.DishImage, d.Description, d.Status, d.SoldOut, d.SortOrder, "0", store.Now(), store.Now())
		if err != nil {
			return err
		}
		dishID, err = res.LastInsertId()
		if err != nil {
			return err
		}
		for _, s := range specs {
			if _, err := tx.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price, stock) VALUES(?,?,?,?)`,
				dishID, s.SpecName, s.Price, normalizeStock(s.Stock)); err != nil {
				return err
			}
		}
		return nil
	})
	return dishID, err
}

// UpdateDish 更新菜品并**增量**更新规格：
//   - 带 SpecID 的规格按 ID UPDATE（库存随表单写回）
//   - 不带 SpecID 的 INSERT 新增
//   - 旧有但不在本次列表里的 DELETE
//
// 相比旧的「DELETE 全量重建」，增量更新保住了 spec_id 与库存 —— 否则店长改个价格/名字，
// 规格会被重建（spec_id 全变、库存归零），自动售罄的计数就前功尽弃。
func UpdateDish(d po.Dish, specs []po.Spec) error {
	return store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_dish SET category_id=?, dish_name=?, dish_image=?, description=?, status=?, sold_out=?, sort_order=?, update_time=? WHERE dish_id=?`,
			d.CategoryID, d.DishName, d.DishImage, d.Description, d.Status, d.SoldOut, d.SortOrder, store.Now(), d.DishID); err != nil {
			return err
		}

		keep := map[int]bool{}
		for _, s := range specs {
			if s.SpecID > 0 {
				keep[s.SpecID] = true
				if _, err := tx.Exec(`UPDATE tb_spec SET spec_name=?, price=?, stock=? WHERE spec_id=? AND dish_id=?`,
					s.SpecName, s.Price, normalizeStock(s.Stock), s.SpecID, d.DishID); err != nil {
					return err
				}
			}
		}

		// 事务内读现有规格，删除不在本次列表里的（被店长删掉的行）。
		rows, err := tx.Query(`SELECT spec_id FROM tb_spec WHERE dish_id=?`, d.DishID)
		if err != nil {
			return err
		}
		var toDelete []int
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !keep[id] {
				toDelete = append(toDelete, id)
			}
		}
		rows.Close()
		for _, id := range toDelete {
			if _, err := tx.Exec(`DELETE FROM tb_spec WHERE spec_id=?`, id); err != nil {
				return err
			}
		}

		for _, s := range specs {
			if s.SpecID == 0 {
				if _, err := tx.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price, stock) VALUES(?,?,?,?)`,
					d.DishID, s.SpecName, s.Price, normalizeStock(s.Stock)); err != nil {
					return err
				}
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
	SpecID      int
	DishID      int
	SpecName    string
	PriceCents  int64
	SpecStock   int64
	DishName    string
	DishStatus  int
	DishSoldOut int
	DelFlag     string
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
	rows, err := store.DB.Query(`SELECT s.spec_id, s.dish_id, s.spec_name, s.price, s.stock, d.dish_name, d.status, d.sold_out, d.del_flag
		FROM tb_spec s JOIN tb_dish d ON d.dish_id = s.dish_id
		WHERE s.spec_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var info SpecOrderInfo
		if err := rows.Scan(&info.SpecID, &info.DishID, &info.SpecName, &info.PriceCents, &info.SpecStock,
			&info.DishName, &info.DishStatus, &info.DishSoldOut, &info.DelFlag); err != nil {
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
	rows, err := store.DB.Query(`SELECT spec_id, dish_id, spec_name, price, stock FROM tb_spec WHERE dish_id=? ORDER BY spec_id`, dishID)
	if err != nil {
		return []po.Spec{}
	}
	defer rows.Close()
	specs := []po.Spec{}
	for rows.Next() {
		var s po.Spec
		rows.Scan(&s.SpecID, &s.DishID, &s.SpecName, &s.Price, &s.Stock)
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
	rows, err := store.DB.Query(`SELECT spec_id, dish_id, spec_name, price, stock FROM tb_spec WHERE dish_id IN (`+placeholders+`) ORDER BY dish_id, spec_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s po.Spec
		rows.Scan(&s.SpecID, &s.DishID, &s.SpecName, &s.Price, &s.Stock)
		out[s.DishID] = append(out[s.DishID], s)
	}
	return out
}

// DecrementSpecStockTx 在事务内扣减某个规格的库存（下单 / 加菜 / 改单写入明细时调用）。
// 不限量（stock < 0）的规格不扣减、恒成功。
//
// 并发安全：限量规格用「带守卫的原子 UPDATE」扣减（stock >= qty 才扣）。
// 并发下单把同一规格抢光时，后到者的 UPDATE 影响 0 行 → 返回 ErrSoldOut，整个事务回滚。
func DecrementSpecStockTx(tx *sql.Tx, specID int, qty int) error {
	var stock int64
	if err := tx.QueryRow(`SELECT stock FROM tb_spec WHERE spec_id=?`, specID).Scan(&stock); err != nil {
		return err
	}
	if stock < 0 {
		return nil // 不限量
	}
	res, err := tx.Exec(`UPDATE tb_spec SET stock = stock - ? WHERE spec_id=? AND stock >= ?`, qty, specID, qty)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSoldOut
	}
	return nil
}

// RestoreSpecStockTx 在事务内回补某个规格的库存（取消未支付订单 / 改单删除旧明细时调用）。
// 不限量的规格不回补；规格已被删除（删菜）时也按无操作处理，不阻断取消/改单。
func RestoreSpecStockTx(tx *sql.Tx, specID int, qty int) error {
	var stock int64
	if err := tx.QueryRow(`SELECT stock FROM tb_spec WHERE spec_id=?`, specID).Scan(&stock); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if stock < 0 {
		return nil
	}
	_, err := tx.Exec(`UPDATE tb_spec SET stock = stock + ? WHERE spec_id=?`, qty, specID)
	return err
}
