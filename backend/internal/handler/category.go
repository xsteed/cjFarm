package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 分类 ============

func CategoryList(c *gin.Context) {
	rows, err := service.CategoryList()
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, len(rows), rows)
}

func CategorySave(c *gin.Context) {
	var ct dto.Category
	if err := c.ShouldBindJSON(&ct); err != nil {
		fail(c, "参数错误")
		return
	}
	id, err := service.SaveCategory(ct)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"categoryId": id})
}

func CategoryUpdate(c *gin.Context) {
	var ct dto.Category
	if err := c.ShouldBindJSON(&ct); err != nil || ct.CategoryID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.UpdateCategory(ct); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "修改成功")
}

func CategoryDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	if err := service.DeleteCategory(id); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "删除成功")
}
