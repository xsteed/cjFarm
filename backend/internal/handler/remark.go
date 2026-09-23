package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 备注 ============

func RemarkList(c *gin.Context) {
	rows, err := service.RemarkList()
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, len(rows), rows)
}

func RemarkSave(c *gin.Context) {
	var r dto.Remark
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, "参数错误")
		return
	}
	id, err := service.SaveRemark(r)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"remarkId": id})
}

func RemarkUpdate(c *gin.Context) {
	var r dto.Remark
	if err := c.ShouldBindJSON(&r); err != nil || r.RemarkID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.UpdateRemark(r); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "修改成功")
}

func RemarkDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	if err := service.DeleteRemark(id); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "删除成功")
}
