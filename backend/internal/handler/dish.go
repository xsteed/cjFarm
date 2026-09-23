package handler

import (
	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 菜品 ============

func DishList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	total, rows, err := service.DishList(c.Query("dishName"), c.Query("categoryId"), pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, total, rows)
}

func DishGet(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	dish, err := service.DishGet(id)
	if err != nil {
		fail(c, "菜品不存在")
		return
	}
	ok(c, dish)
}

func DishSave(c *gin.Context) {
	var d dto.Dish
	if err := c.ShouldBindJSON(&d); err != nil {
		fail(c, "参数错误")
		return
	}
	id, err := service.SaveDish(d)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"dishId": id})
}

func DishUpdate(c *gin.Context) {
	var d dto.Dish
	if err := c.ShouldBindJSON(&d); err != nil || d.DishID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.UpdateDish(d); err != nil {
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
	if err := service.DeleteDish(id); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "删除成功")
}
