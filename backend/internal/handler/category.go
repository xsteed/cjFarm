package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 分类 ============

func CategoryList(c *gin.Context) {
	rows, err := store.DB.Query(`SELECT category_id, category_name, sort_order, del_flag, create_time, update_time
		FROM tb_category WHERE del_flag='0' ORDER BY sort_order, category_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Category{}
	for rows.Next() {
		var ct model.Category
		rows.Scan(&ct.CategoryID, &ct.CategoryName, &ct.SortOrder, &ct.DelFlag, &ct.CreateTime, &ct.UpdateTime)
		list = append(list, ct)
	}
	tableResult(c, len(list), list)
}

func CategorySave(c *gin.Context) {
	var ct model.Category
	if err := c.ShouldBindJSON(&ct); err != nil {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(ct.CategoryName) == "" {
		fail(c, "请填写分类名称")
		return
	}
	res, err := store.DB.Exec(`INSERT INTO tb_category(category_name, sort_order, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?)`, ct.CategoryName, ct.SortOrder, "0", store.Now(), store.Now())
	if err != nil {
		fail(c, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	ok(c, gin.H{"categoryId": id})
}

func CategoryUpdate(c *gin.Context) {
	var ct model.Category
	if err := c.ShouldBindJSON(&ct); err != nil || ct.CategoryID == 0 {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(ct.CategoryName) == "" {
		fail(c, "请填写分类名称")
		return
	}
	store.DB.Exec(`UPDATE tb_category SET category_name=?, sort_order=?, update_time=? WHERE category_id=?`,
		ct.CategoryName, ct.SortOrder, store.Now(), ct.CategoryID)
	okMsg(c, "修改成功")
}

func CategoryDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	store.DB.Exec(`UPDATE tb_category SET del_flag='1', update_time=? WHERE category_id=?`, store.Now(), id)
	okMsg(c, "删除成功")
}
