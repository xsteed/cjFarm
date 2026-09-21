package handler

import (
	"database/sql"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 菜品 ============

func DishList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	name := c.Query("dishName")
	categoryId := c.Query("categoryId")
	where := " WHERE d.del_flag='0'"
	var args []interface{}
	if name != "" {
		where += " AND d.dish_name LIKE ?"
		args = append(args, "%"+name+"%")
	}
	if categoryId != "" {
		where += " AND d.category_id=?"
		args = append(args, categoryId)
	}
	var total int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_dish d`+where, args...).Scan(&total)

	rows, err := store.DB.Query(`SELECT `+store.DishCols+`
		FROM tb_dish d LEFT JOIN tb_category c ON d.category_id=c.category_id`+where+` ORDER BY d.sort_order, d.dish_id LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Dish{}
	ids := []int{}
	for rows.Next() {
		if d, err := store.ScanDish(rows); err == nil {
			list = append(list, d)
			ids = append(ids, d.DishID)
		}
	}
	specMap := store.LoadSpecsByDishIDs(ids)
	for i := range list {
		list[i].Specs = specMap[list[i].DishID]
	}
	tableResult(c, total, list)
}

func DishGet(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	d, err := store.ScanDish(store.DB.QueryRow(`SELECT `+store.DishCols+`
		FROM tb_dish d LEFT JOIN tb_category c ON d.category_id=c.category_id WHERE d.dish_id=?`, id))
	if err != nil {
		fail(c, "菜品不存在")
		return
	}
	d.Specs = store.LoadDishSpecs(d.DishID)
	ok(c, d)
}

func DishSave(c *gin.Context) {
	var d model.Dish
	if err := c.ShouldBindJSON(&d); err != nil {
		fail(c, "参数错误")
		return
	}
	if d.CategoryID == 0 || strings.TrimSpace(d.DishName) == "" {
		fail(c, "请选择分类并填写菜品名称")
		return
	}
	if len(d.Specs) == 0 {
		fail(c, "请至少添加一个规格")
		return
	}
	var id int64
	err := store.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO tb_dish(category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time)
			VALUES(?,?,?,?,?,?,?,?,?)`, d.CategoryID, d.DishName, d.DishImage, d.Description, d.Status, d.SortOrder, "0", store.Now(), store.Now())
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		for _, s := range d.Specs {
			if _, err := tx.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price) VALUES(?,?,?)`, id, s.SpecName, model.ToCents(s.Price)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"dishId": id})
}

func DishUpdate(c *gin.Context) {
	var d model.Dish
	if err := c.ShouldBindJSON(&d); err != nil || d.DishID == 0 {
		fail(c, "参数错误")
		return
	}
	if d.CategoryID == 0 || strings.TrimSpace(d.DishName) == "" {
		fail(c, "请选择分类并填写菜品名称")
		return
	}
	if len(d.Specs) == 0 {
		fail(c, "请至少添加一个规格")
		return
	}
	err := store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_dish SET category_id=?, dish_name=?, dish_image=?, description=?, status=?, sort_order=?, update_time=? WHERE dish_id=?`,
			d.CategoryID, d.DishName, d.DishImage, d.Description, d.Status, d.SortOrder, store.Now(), d.DishID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tb_spec WHERE dish_id=?`, d.DishID); err != nil {
			return err
		}
		for _, s := range d.Specs {
			if _, err := tx.Exec(`INSERT INTO tb_spec(dish_id, spec_name, price) VALUES(?,?,?)`, d.DishID, s.SpecName, model.ToCents(s.Price)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "修改成功")
}

func DishDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	store.DB.Exec(`UPDATE tb_dish SET del_flag='1', update_time=? WHERE dish_id=?`, store.Now(), id)
	okMsg(c, "删除成功")
}
