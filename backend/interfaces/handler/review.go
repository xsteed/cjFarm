package handler

import (
	"errors"
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
	"dining-system/domain/po"
	"dining-system/infrastructure/store/dao"
)

// ============ 顾客端:订单评价(仅商家可见的内部反馈) ============
// 匿名与会员同一入口:挂 CustomerAuthOptional,会员身份只认令牌解析。
// 路由层另挂 IP 限流(匿名可评 = 无鉴权写接口,限流是垃圾内容的第一道闸)。

// CustomerReviewStatus GET /customer/review?orderNo=&tableId=&reviewerKey=
// 顾客端查「本单本人是否已评价」,驱动订单页入口按钮的两种形态(去评价 / 已评价)。
// 会员身份走令牌(服务端派生评价者键);匿名为设备持久化的随机键。
func CustomerReviewStatus(c *gin.Context) {
	orderNo := c.Query("orderNo")
	tableID, _ := strconv.Atoi(c.Query("tableId"))
	st, err := service.CustomerReviewStatus(customerIDFrom(c), tableID, orderNo, c.Query("reviewerKey"))
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, st)
}

// CustomerReviewCreate POST /customer/review
func CustomerReviewCreate(c *gin.Context) {
	var p struct {
		TableID int      `json:"tableId"`
		OrderNo string   `json:"orderNo"`
		Rating  int      `json:"rating"`
		Tags    []string `json:"tags"`
		Content string   `json:"content"`
		// Images 「先传后评」:先经 /customer/review/upload 拿到的图片名,
		// 提交时在事务内绑定到评价(最多 po.ReviewImagesMax 张,超限截断)。
		Images []string `json:"images"`
		// ReviewerKey 匿名顾客的设备评价键(会员会被服务端按身份覆盖):
		// 一单一评×一人一次,同桌其他顾客不受影响。
		ReviewerKey string `json:"reviewerKey"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	if len(p.Images) > po.ReviewImagesMax {
		p.Images = p.Images[:po.ReviewImagesMax]
	}
	st, err := service.CreateCustomerReview(customerIDFrom(c), p.TableID, p.OrderNo,
		p.Rating, p.Tags, p.Content, p.Images, p.ReviewerKey, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		// 已评价按幂等友好处理:入口按钮本会切到「已评价」,双击/重试落到这里
		// 不该是一句冷冰冰的报错。
		if errors.Is(err, service.ErrReviewAlready) {
			failData(c, err.Error(), gin.H{"reviewed": true})
			return
		}
		fail(c, err.Error())
		return
	}
	ok(c, st)
}

// CustomerReviewUpload POST /customer/review/upload (multipart, 字段名 file)
// 「先传后评」:图片先落在 tb_review_image(review_id=0),提交评价时事务绑定;
// 未绑定的孤儿由后台任务 24h 清理。匿名与会员同入口,挂 IP 限流。
func CustomerReviewUpload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		fail(c, "请选择要上传的图片")
		return
	}
	f, err := fh.Open()
	if err != nil {
		fail(c, "读取上传文件失败")
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		fail(c, "读取上传文件失败")
		return
	}
	url, err := service.SaveCustomerReviewImage(fh.Filename, data)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"url": url})
}

// ============ 管理端:评价列表 / 删除 ============

// AdminReviewList GET /admin/review/list
// 分页 + 同口径汇总一次返回;筛选条件见 dao.ReviewQuery。
func AdminReviewList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	var q dao.ReviewQuery
	if v := c.Query("startDay"); v != "" {
		q.StartDay = v
	}
	if v := c.Query("endDay"); v != "" {
		q.EndDay = v
	}
	ratingMin, okArg := intArg(c, "ratingMin")
	if !okArg {
		return
	}
	q.RatingMin = derefInt(ratingMin)
	ratingMax, okArg := intArg(c, "ratingMax")
	if !okArg {
		return
	}
	q.RatingMax = derefInt(ratingMax)
	if c.Query("hasContent") == "1" || c.Query("hasContent") == "true" {
		q.HasContent = true
	}
	// hasImages=1 仅看有图 / 0 仅看无图(与缺省「全部」区分:空=不限)。
	if v := c.Query("hasImages"); v == "1" || v == "true" {
		q.HasImages = true
	} else if v == "0" || v == "false" {
		q.HasNoImages = true
	}
	if v := c.Query("keyword"); v != "" {
		q.Keyword = v
	}
	res, err := service.AdminReviewList(q, pageNum, pageSize)
	if err != nil {
		fail(c, "查询失败，请稍后再试")
		return
	}
	ok(c, gin.H{"total": res.Total, "items": res.Items, "summary": res.Summary})
}

// AdminReviewDelete DELETE /admin/review/:id
func AdminReviewDelete(c *gin.Context) {
	id, valid := idParam(c)
	if !valid {
		return
	}
	detail, err := service.AdminDeleteReview(id, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "review", strconv.Itoa(id), detail)
	okMsg(c, "已删除")
}

// derefInt 解引用可选整数指针(nil = 0 = 不限)。
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
