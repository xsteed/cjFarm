package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 桌台 ============

func TableList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	total, rows, err := service.TableList(c.Query("tableName"), pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, total, rows)
}

func TableSave(c *gin.Context) {
	var t dto.Table
	if err := c.ShouldBindJSON(&t); err != nil {
		fail(c, "参数错误")
		return
	}
	id, err := service.SaveTable(t)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"tableId": id})
}

func TableUpdate(c *gin.Context) {
	var t dto.Table
	if err := c.ShouldBindJSON(&t); err != nil || t.TableID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.UpdateTable(t); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "修改成功")
}

func TableDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	if err := service.DeleteTable(id); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "删除成功")
}
