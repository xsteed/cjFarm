// Package handler 提供 HTTP 层:响应封装、鉴权中间件与各资源的处理器。
//
// 依赖方向:handler 依赖 dto + po + store + service + print。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"dining-system/interfaces/middleware"
)

// ==================== 统一响应结构(复刻 RuoYi 风格) ====================

// Rsp 是所有业务接口的统一响应结构体。
//
// 字段均带 omitempty:各接口只填充自己需要的字段,避免未使用字段
// 以零值/null 污染响应体。注意 Data 的语义变化:传入 nil 时字段整体
// 省略(旧实现输出 "data":null),前端判断 resp.data 存在性反而更简单。
type Rsp struct {
	Code      int         `json:"code"`
	Msg       string      `json:"msg"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
}

// PageData 是分页接口的数据结构,整体作为 Rsp.Data 承载:
// total 与 items 收进 data 下,与普通对象结果保持同一层级语义,
// 前端统一从 resp.data 取数,无需区分分页/非分页。
type PageData struct {
	Total int         `json:"total"`
	Items interface{} `json:"items"`
}

// injectRequestID 把当前请求的 requestID(由全局 RequestID 中间件生成)
// 注入响应结构。单独成函数,所有响应出口统一调用:
// 漏注入只是少一个字段、不会 panic;中间件未挂载时自动得到空串被省略。
func injectRequestID(c *gin.Context, rsp *Rsp) *Rsp {
	rsp.RequestID = middleware.GetString(c)
	return rsp
}

// NewRsp 构造统一响应并注入 requestID。供包外响应出口使用
// (如 main 包的 404 兜底),包内各包装函数请直接组合 injectRequestID。
func NewRsp(c *gin.Context, code int, msg string) *Rsp {
	return injectRequestID(c, &Rsp{Code: code, Msg: msg})
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, injectRequestID(c, &Rsp{Code: 200, Msg: "操作成功", Data: data}))
}

func okMsg(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, injectRequestID(c, &Rsp{Code: 200, Msg: msg}))
}

func tableResult(c *gin.Context, total int, items interface{}) {
	c.JSON(http.StatusOK, injectRequestID(c, &Rsp{Code: 200, Msg: "查询成功", Data: &PageData{Total: total, Items: items}}))
}

// fail 返回业务错误。HTTP 状态码用 400(而非 200),使错误语义正确、
// 便于网关/监控识别;响应体仍保留 {code, msg} 结构,前端拦截器据此提取提示信息。
func fail(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, injectRequestID(c, &Rsp{Code: 400, Msg: msg}))
}

// failData 返回业务错误并附带结构化数据(HTTP 400 + {code, msg, data}),
// 供特定错误场景把机器可读字段(如催菜冷却剩余秒数)交给前端。
func failData(c *gin.Context, msg string, data interface{}) {
	c.JSON(http.StatusBadRequest, injectRequestID(c, &Rsp{Code: 400, Msg: msg, Data: data}))
}

// pageParams 解析分页参数(页码/每页条数),非法值回退默认。
//
// pageNum 必须加上界:Atoi 对超 int 范围的输入返回 MaxInt 且报错,若忽略错误,
// 下游的 (pageNum-1)*pageSize 会溢出成负数 OFFSET —— SQLite 把负 OFFSET 当 0
// (返回第一页,分页结果错乱),MySQL 直接报错。上界同时挡住「翻到第 10 万页」
// 造成的慢查询。
func pageParams(c *gin.Context) (int, int) {
	pageNum, err := strconv.Atoi(c.DefaultQuery("pageNum", "1"))
	if err != nil || pageNum < 1 {
		pageNum = 1
	}
	if pageNum > maxPageNum {
		pageNum = maxPageNum
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	if err != nil || pageSize < 1 || pageSize > 500 {
		pageSize = 10
	}
	return pageNum, pageSize
}

// maxPageNum 页码上界。单店任何列表的数据量都远小于此(10 条/页 × 10 万页),
// 取一个足够大又不会让 OFFSET 溢出的值。
const maxPageNum = 100000

// idParam 解析路径参数 :id,失败时写入错误响应并返回 false。
// 同时校验下界:0/负数 ID 在业务上不存在,放过去只是让下游白查一趟。
func idParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		fail(c, "无效的ID")
		return 0, false
	}
	return id, true
}
