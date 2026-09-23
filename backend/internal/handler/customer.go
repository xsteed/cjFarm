package handler

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/infra/logger"
	"dining-system/internal/dto"
	"dining-system/internal/print"
	"dining-system/internal/service"
)

// ============ 顾客端(公开接口) ============

// customerFail 收敛顾客端公开接口的错误文案:匿名访客不应看到 dao 原始错误,
// 真实错误统一写日志;只有不含内部细节的业务提示才原样返回,其余返回固定文案。
func customerFail(c *gin.Context, err error) {
	if err == nil {
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	msg := err.Error()
	// 订单不存在/已结束/不可加菜/已支付统一为同一文案,防止通过响应差异探测订单存在性。
	switch msg {
	case "订单不存在", "订单已结束，无需催菜", "当前订单状态不可加菜", "订单已支付，不可加菜":
		logger.Warnf("[customer] 请求失败 %s: %v", c.Request.URL.Path, err)
		fail(c, "订单不存在或已结束")
		return
	}
	// 「下单失败/加菜失败」包装了 dao 底层错误(可能是 SQL 错误),不能回显给访客。
	if strings.HasPrefix(msg, "下单失败:") || strings.HasPrefix(msg, "加菜失败:") {
		logger.Warnf("[customer] 请求失败 %s: %v", c.Request.URL.Path, err)
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	fail(c, msg)
}

// CustomerTable 顾客扫码进入桌台:路由参数同时接受「桌台稳定码」与「数字桌台ID」。
// 老二维码(/order/1)与新二维码(/order/XXXXXXXX)都可正常打开。
func CustomerTable(c *gin.Context) {
	table, currentOrder, err := service.CustomerTable(c.Param("id"))
	if err != nil {
		fail(c, "桌台不存在")
		return
	}
	var co interface{}
	if currentOrder != nil {
		co = currentOrder
	}
	ok(c, gin.H{
		"tableId":      table.TableID,
		"tableNo":      table.TableNo,
		"tableName":    table.TableName,
		"tableCode":    table.TableCode,
		"capacity":     table.Capacity,
		"status":       table.Status,
		"currentOrder": co,
	})
}

func CustomerMenu(c *gin.Context) {
	out, err := service.CustomerMenu()
	if err != nil {
		logger.Warnf("[customer] 菜单加载失败: %v", err)
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	ok(c, out)
}

func CustomerRemarks(c *gin.Context) {
	list, err := service.RemarkList()
	if err != nil {
		logger.Warnf("[customer] 备注加载失败: %v", err)
		fail(c, "系统繁忙，请稍后再试")
		return
	}
	ok(c, list)
}

func CustomerCreateOrder(c *gin.Context) {
	var p struct {
		TableID     int             `json:"tableId"`
		PersonCount int             `json:"personCount"`
		OrderRemark string          `json:"orderRemark"`
		Items       []dto.OrderItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	res, err := service.CustomerCreateOrder(p.TableID, p.PersonCount, p.OrderRemark, p.Items)
	if err != nil {
		customerFail(c, err)
		return
	}

	// 下单成功后异步触发打印机打印(厨房单 + 食客小票),失败不影响下单。
	if created, ok := service.LoadOrderForPrint(int(res.OrderID)); ok {
		print.PrintOrder(created, res.Items)
	}

	ok(c, gin.H{
		"orderId":        res.OrderID,
		"orderNo":        res.OrderNo,
		"dishAmount":     res.DishAmount,
		"seatFee":        res.SeatFee,
		"discountAmount": res.DiscountAmount,
		"totalAmount":    res.TotalAmount,
	})
}

func CustomerOrderByNo(c *gin.Context) {
	order, err := service.CustomerOrderByNo(c.Param("orderNo"))
	if err != nil {
		customerFail(c, err)
		return
	}
	ok(c, order)
}

func CustomerPayQr(c *gin.Context) {
	ok(c, service.CustomerPayQr())
}

// CustomerSetting 返回顾客端所需的公开配置(无需登录)。
// 之前顾客点餐页/打印页误用了需鉴权的管理端 /api/admin/config/list,导致 401,现统一走此接口。
func CustomerSetting(c *gin.Context) {
	ok(c, service.CustomerSettings())
}

// CustomerAppendOrder 顾客加菜:向该桌进行中且未支付的订单追加菜品。
// 与首次下单不同,加菜复用已有订单(合并明细、重算金额),避免同一桌产生多笔订单。
func CustomerAppendOrder(c *gin.Context) {
	var p struct {
		TableID int             `json:"tableId"`
		OrderNo string          `json:"orderNo"`
		Items   []dto.OrderItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}

	res, err := service.CustomerAppendOrder(p.TableID, p.OrderNo, p.Items)
	if err != nil {
		customerFail(c, err)
		return
	}

	// 打印新增菜品的厨房单(食客小票在结账时打印,此处仅通知后厨)。
	if created, ok := service.LoadOrderForPrint(res.OrderID); ok {
		print.PrintKitchen(created, res.Items)
	}

	ok(c, gin.H{
		"orderId":     res.OrderID,
		"orderNo":     res.OrderNo,
		"dishAmount":  res.DishAmount,
		"seatFee":     res.SeatFee,
		"totalAmount": res.TotalAmount,
	})
}

// CustomerUrgeOrder 顾客催菜:请求后厨加急处理当前订单。
//
// 约束:
//   - 仅进行中的订单可催(已完成/已取消无意义);
//   - 同一订单 180 秒内只受理一次,防止顾客连点把后厨刷屏,
//     超频时返回剩余等待秒数,前端据此展示倒计时。
//
// 催菜会生成一条待处理记录,商家端订单列表与桌台看板据此打角标。
func CustomerUrgeOrder(c *gin.Context) {
	var p struct {
		TableID int    `json:"tableId"`
		OrderNo string `json:"orderNo"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}

	res, err := service.CustomerUrgeOrder(p.TableID, p.OrderNo)
	if err != nil {
		// 冷却拒绝:结构化返回剩余秒数,前端据此展示倒计时,不再解析 msg 文案。
		var cd *service.UrgeCooldownError
		if errors.As(err, &cd) {
			failData(c, cd.Error(), gin.H{"cooldown": cd.Remain})
			return
		}
		customerFail(c, err)
		return
	}

	ok(c, gin.H{
		"orderId":  res.OrderID,
		"cooldown": res.Cooldown,
	})
}
