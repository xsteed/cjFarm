package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/weapp"
	"dining-system/interfaces/dto"
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
	id, err := service.SaveTable(c.Request.Context(), t)
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

// TableLock 设置桌台人工可用性状态(停用/预约/恢复正常),避免被其他客人扫码占用。
func TableLock(c *gin.Context) {
	var p struct {
		TableID    int    `json:"tableId"`
		LockStatus int    `json:"lockStatus"`
		LockRemark string `json:"lockRemark"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.TableID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.SetTableLock(p.TableID, p.LockStatus, p.LockRemark); err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "桌台状态已更新")
}

// TableRelease 强制清台:店家漏点「收款」时的唯一人工补救入口。
//
// 漏点收款 → 订单停在 (1/2/3, pay_status=0) → 桌台永远占用 → 下一桌扫码被直接
// 带进上一桌的账单,而常规编辑接口又不改占用状态。这里把该桌进行中订单一次性
// 收走(补结账/作废二选一)并置空闲。
//
// 这是会动钱的高危动作,校验放在 service,这里只负责透传与留痕:
// 金额与方式必须进审计日志,否则对账时说不清钱去哪了。
func TableRelease(c *gin.Context) {
	var p struct {
		TableID int    `json:"tableId"`
		Action  string `json:"action"` // settle 补结账 / cancel 作废
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.TableID == 0 {
		fail(c, "参数错误")
		return
	}
	// 补结账会把钱记进营收,光凭桌台管理权限不足以做这个判断:必须同时具备订单结账权限。
	// 作废不动账务,沿用路由上的 table:edit 即可。内置角色都不受影响(店长两项俱全),
	// 这道校验是给「自建了只有 table:edit 的角色」的店兜底,避免有人悄悄把营收做上去。
	if p.Action == service.ReleaseActionSettle && !HasPerm(c, "order:settle") {
		forbidden(c, "补结账需要「订单结账」权限，请改用「作废订单」或联系店长")
		return
	}
	out, err := service.ReleaseTable(c.Request.Context(), p.TableID, p.Action, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 桌台已清空,顾客手里还开着的在线支付二维码必须一并关掉,否则就是重复支付的入口。
	// 与 /order/cancel 同一口径:不按 pay_status 过滤,已支付单的残留二维码同样要关。
	for _, orderNo := range out.OrderNos {
		closePendingPayments(c.Request.Context(), orderNo, "")
	}
	name := out.TableName
	if name == "" {
		name = strconv.Itoa(p.TableID)
	}
	actionText := "作废"
	if p.Action == service.ReleaseActionSettle {
		actionText = fmt.Sprintf("补结账 %.2f 元", float64(out.TotalCents)/100)
	}
	SetAuditDetail(c, "table", strconv.Itoa(p.TableID),
		fmt.Sprintf("强制清台【%s】：%s；结算 %d 单 / 作废 %d 单 / 归档 %d 单", name, actionText, out.Settled, out.Canceled, out.Finished))
	okMsg(c, "已清台，桌台恢复空闲")
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
// (详见 infrastructure/weapp 的 CodeResult 注释)。
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
	res, err := weapp.TableCode(c.Request.Context(), code, width)
	if err != nil {
		// 出码失败原因(appid/secret 配错、IP 白名单、微信频控)都只在 weapp 包里能被
		// 判别,用户看到的仅一句中文提示;这里补一条带 requestID 的入口日志,
		// 让「哪次请求出码失败」与服务端内部日志能对上。
		logger.CtxWarnf(c.Request.Context(), "[table] 生成桌台小程序码失败 tableCode=%s: %v", code, err)
		fail(c, err.Error())
		return
	}
	ok(c, res)
}
