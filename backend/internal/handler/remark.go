package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 备注 ============

func RemarkList(c *gin.Context) {
	rows, err := store.DB.Query(`SELECT remark_id, option_name, sort_order, del_flag, create_time, update_time
		FROM tb_remark WHERE del_flag='0' ORDER BY sort_order, remark_id`)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Remark{}
	for rows.Next() {
		var r model.Remark
		rows.Scan(&r.RemarkID, &r.OptionName, &r.SortOrder, &r.DelFlag, &r.CreateTime, &r.UpdateTime)
		list = append(list, r)
	}
	tableResult(c, len(list), list)
}

func RemarkSave(c *gin.Context) {
	var r model.Remark
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(r.OptionName) == "" {
		fail(c, "请填写备注名称")
		return
	}
	res, err := store.DB.Exec(`INSERT INTO tb_remark(option_name, sort_order, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?)`, r.OptionName, r.SortOrder, "0", store.Now(), store.Now())
	if err != nil {
		fail(c, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	ok(c, gin.H{"remarkId": id})
}

func RemarkUpdate(c *gin.Context) {
	var r model.Remark
	if err := c.ShouldBindJSON(&r); err != nil || r.RemarkID == 0 {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(r.OptionName) == "" {
		fail(c, "请填写备注名称")
		return
	}
	store.DB.Exec(`UPDATE tb_remark SET option_name=?, sort_order=?, update_time=? WHERE remark_id=?`,
		r.OptionName, r.SortOrder, store.Now(), r.RemarkID)
	okMsg(c, "修改成功")
}

func RemarkDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	store.DB.Exec(`UPDATE tb_remark SET del_flag='1', update_time=? WHERE remark_id=?`, store.Now(), id)
	okMsg(c, "删除成功")
}
