// Package handler 提供 HTTP 层:响应封装、鉴权中间件与各资源的处理器。
//
// 依赖方向:handler 依赖 model + store + service + print。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ==================== 响应封装(复刻 RuoYi 风格) ====================

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "操作成功", "data": data})
}

func okMsg(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": msg})
}

func tableResult(c *gin.Context, total int, rows interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "查询成功", "total": total, "rows": rows})
}

// fail 返回业务错误。HTTP 状态码用 400(而非 200),使错误语义正确、
// 便于网关/监控识别;响应体仍保留 {code, msg} 结构,前端拦截器据此提取提示信息。
func fail(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": msg})
}

// pageParams 解析分页参数(页码/每页条数),非法值回退默认。
func pageParams(c *gin.Context) (int, int) {
	pageNum, _ := strconv.Atoi(c.DefaultQuery("pageNum", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 10
	}
	return pageNum, pageSize
}

// idParam 解析路径参数 :id,失败时写入错误响应并返回 false。
func idParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		fail(c, "无效的ID")
		return 0, false
	}
	return id, true
}
