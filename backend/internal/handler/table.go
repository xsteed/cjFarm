package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
	"dining-system/internal/weapp"
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

// TableWeappCode 生成桌台小程序码,返回 {dataUrl, cached, envVersion, width}。
//
// 回 JSON 而非图片二进制:前端请求层把响应统一解包成 {code,msg,data},
// 二进制过不了这层;而打印排版用的正是 dataURL,JSON 里直接给字符串最省事
// (详见 internal/weapp 的 CodeResult 注释)。
//
// 权限沿用 table:view:出码本质是「看这张桌的码」,与列表同级;
// 真正的开关在「系统配置」里 —— 没填 AppSecret 就出不了码。
func TableWeappCode(c *gin.Context) {
	code := strings.TrimSpace(c.Query("tableCode"))
	if code == "" {
		fail(c, "缺少桌台码")
		return
	}
	// width 非法值交给 weapp 收敛到微信允许的区间(280~1280),
	// 这里只负责把「不传/非数字」当成 0 传下去。
	width, _ := strconv.Atoi(c.Query("width"))
	res, err := weapp.TableCode(code, width)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, res)
}
