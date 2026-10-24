package service

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dining-system/domain/po"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// ============================================================================
// 订单评价(仅商家可见的内部反馈通道)
// 设计与决策见 docs/design-docs/order-review/spec.md。
// ============================================================================

// ErrReviewAlready 该订单已评价过(一单一评,前端入口也据此切换形态)。
var ErrReviewAlready = errors.New("该订单已评价过")

// sanitizeReviewTags 清洗标签入参:逐项去空格、去掉分隔符逗号(否则会污染库内
// 逗号分隔格式)、按列宽截断、去重、限量。返回可直接 join 的干净列表。
func sanitizeReviewTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		// 逗号是存储分隔符,出现在标签里会破坏拆分;替换成空格而不是丢弃,
		// 「味道,服务」仍能保留一点原意。
		t = strings.ReplaceAll(t, ",", " ")
		t = truncateRunes(t, po.ReviewTagMaxLen)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= po.ReviewTagsMax {
			break
		}
	}
	return out
}

// reviewableStatus 报告订单状态是否允许评价:已上齐(3)或已完成(4)。
// 只在「已付款」的前提下有意义(见 CreateCustomerReview 的校验链)。
func reviewableStatus(status int) bool {
	return status == OrderStatusDining || status == OrderStatusFinished
}

// CreateCustomerReview 顾客提交评价(匿名与会员同一入口)。
//
// 校验链(顺序即防线,前面的先挡):
//  1. 参数:orderNo 非空、rating 1..5、标签与文字清洗;
//  2. 订单存在且属于请求方声称的桌台(orderNo 可被猜测,归属校验与催菜同款);
//  3. 已付款(pay_status=1)且已上齐/完成 —— 没吃完不能评,没付清不能评;
//  4. 未评过(预检 + order_no 唯一索引兜底并发双击);
//  5. 会员快照(昵称/手机号)一次性写死,注销后追溯链不断。
//
// ip/ua 只存胡椒哈希(明文永不落库),与访问统计同一密钥体系(hashVisitValue)。
func CreateCustomerReview(customerID, tableID int, orderNo string, rating int, tags []string, content string, images []string, ip, ua string) (dto.CustomerReviewStatus, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" || tableID == 0 {
		return dto.CustomerReviewStatus{}, errors.New("参数错误")
	}
	if rating < po.ReviewRatingMin || rating > po.ReviewRatingMax {
		return dto.CustomerReviewStatus{}, fmt.Errorf("评分需在 %d~%d 星之间", po.ReviewRatingMin, po.ReviewRatingMax)
	}
	cleanTags := sanitizeReviewTags(tags)
	content = truncateRunes(strings.TrimSpace(content), po.ReviewContentMaxLen)

	o, err := dao.GetOrderReviewTarget(orderNo)
	if err != nil {
		return dto.CustomerReviewStatus{}, errors.New("订单不存在")
	}
	if o.TableID != tableID {
		return dto.CustomerReviewStatus{}, errors.New("订单不属于当前桌台")
	}
	if o.PayStatus != 1 {
		return dto.CustomerReviewStatus{}, errors.New("订单尚未支付，暂时不能评价")
	}
	if !reviewableStatus(o.OrderStatus) {
		return dto.CustomerReviewStatus{}, errors.New("用餐结束后才能评价")
	}
	if existing, err := dao.GetReviewByOrderNo(orderNo); err == nil && existing.ReviewID > 0 {
		return dto.CustomerReviewStatus{}, ErrReviewAlready
	}

	r := po.Review{
		OrderID:     o.OrderID,
		OrderNo:     orderNo,
		BizDate:     o.BizDate,
		TableID:     o.TableID,
		TableNo:     o.TableNo,
		TableName:   o.TableName,
		PersonCount: o.PersonCount,
		CustomerID:  customerID, // 0 = 匿名;会员身份只来自令牌解析,不信请求体
		Rating:      rating,
		Tags:        strings.Join(cleanTags, ","),
		Content:     content,
		IpHash:      hashVisitValue(ip),
		UaHash:      hashVisitValue(ua),
	}
	if customerID > 0 {
		// 快照缺失(如并发注销)不阻断提交:评价本体比快照重要,匿名字段兜底。
		r.Nickname, r.Phone = dao.ReviewMemberSnapshot(customerID)
	}
	// 评价行与图片绑定在同一事务:评价插成功而绑定失败会留下指向不明的外链图片,
	// 反之则产生孤儿图。任一步失败整体回滚。图片按「先传后评」流程先落在
	// tb_review_image(review_id=0),这里认领;只认未绑定的行(先到先得)。
	err = store.WithTx(func(tx *sql.Tx) error {
		id, e := dao.InsertReviewTx(tx, r)
		if e != nil {
			return e
		}
		if len(images) > 0 {
			bound, e := dao.BindReviewImagesTx(tx, id, images)
			if e != nil {
				return e
			}
			if bound != int64(len(images)) {
				// 部分图名不存在或已被占用:整体回滚,让顾客重新选图。
				return fmt.Errorf("部分图片已失效，请重新选择后提交")
			}
		}
		return nil
	})
	if err != nil {
		// 并发双击撞唯一索引:重查确认后转成友好语义,而不是把 SQL 错误抛给顾客。
		if existing, qErr := dao.GetReviewByOrderNo(orderNo); qErr == nil && existing.ReviewID > 0 {
			return dto.CustomerReviewStatus{}, ErrReviewAlready
		}
		logger.Errorf("[review][告警] 评价落库失败 单%s: %v", orderNo, err)
		if strings.Contains(err.Error(), "已失效") {
			return dto.CustomerReviewStatus{}, err
		}
		return dto.CustomerReviewStatus{}, errors.New("评价提交失败，请稍后再试")
	}
	return dto.CustomerReviewStatus{Reviewed: true, Rating: rating, Tags: cleanTags, Content: content}, nil
}

// SaveCustomerReviewImage 顾客上传评价图片(独立表 tb_review_image,不进 tb_image)。
// 返回可直接展示的 /uploads/{name} 路径;提交评价时按 img_name 绑定。
//
// 限制:扩展名白名单(与商家上传同口径)、单张 ≤5MB(po.ReviewImageMaxBytes)、
// 内容必须通过魔数/解码校验(isValidImage 与商家上传同一套,防伪装成图片的载荷)。
// review_id 落 0,由提交评价时的事务绑定;未提交的孤儿由后台任务按 create_time 清理。
func SaveCustomerReviewImage(fileName string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if !allowedImageExt[ext] {
		return "", errors.New("仅支持上传图片文件(jpg/png/gif/webp/bmp)")
	}
	if len(data) > po.ReviewImageMaxBytes {
		return "", errors.New("图片大小不能超过 5MB")
	}
	if err := isValidImage(ext, data); err != nil {
		return "", err
	}
	now := time.Now()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成文件名失败: %w", err)
	}
	name := fmt.Sprintf("review_%s_%d_%x%s", now.Format("20060102_150405"), now.Nanosecond(), b, ext)
	if err := dao.SaveReviewImage(name, store.ImageMIME(ext), data); err != nil {
		logger.Errorf("[review][告警] 评价图片落库失败: %v", err)
		return "", errors.New("上传失败，请稍后再试")
	}
	return store.UploadURLPrefix + name, nil
}

// CustomerReviewStatus 顾客端查「本单是否已评价」(驱动入口按钮的两种形态)。
// 归属校验同上:单号可猜测,必须带桌台 ID。
func CustomerReviewStatus(tableID int, orderNo string) (dto.CustomerReviewStatus, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" || tableID == 0 {
		return dto.CustomerReviewStatus{}, errors.New("参数错误")
	}
	o, err := dao.GetOrderReviewTarget(orderNo)
	if err != nil {
		return dto.CustomerReviewStatus{}, errors.New("订单不存在")
	}
	if o.TableID != tableID {
		return dto.CustomerReviewStatus{}, errors.New("订单不属于当前桌台")
	}
	r, err := dao.GetReviewByOrderNo(orderNo)
	if err != nil || r.ReviewID == 0 {
		// 未评价(查无记录是正常态,不是错误)。
		return dto.CustomerReviewStatus{Reviewed: false, Tags: []string{}}, nil
	}
	return dto.CustomerReviewStatus{
		Reviewed: true,
		Rating:   r.Rating,
		Tags:     dto.SplitReviewTags(r.Tags),
		Content:  r.Content,
	}, nil
}

// AdminReviewListResult 管理端评价列表(分页 + 同口径汇总一次返回)。
type AdminReviewListResult struct {
	Total   int               `json:"total"`
	Items   []dto.Review      `json:"items"`
	Summary dto.ReviewSummary `json:"summary"`
}

// AdminReviewList 管理端分页查询评价:列表、总数与汇总用同一份筛选条件,
// 三者口径不会漂移(见 dao.reviewCond)。
func AdminReviewList(q dao.ReviewQuery, pageNum, pageSize int) (AdminReviewListResult, error) {
	total, rows, err := dao.ListReviews(q, pageNum, pageSize)
	if err != nil {
		return AdminReviewListResult{}, err
	}

	// 顾客身份三态:customer_id>0 且档案仍在 = member;档案已物理删除 = deleted。
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.CustomerID > 0 {
			ids = append(ids, r.CustomerID)
		}
	}
	alive := dao.CustomerAliveMap(ids)
	// 图片按评价批量取(避免 N+1),填 /uploads/{name} 展示路径。
	ids2 := make([]int, 0, len(rows))
	for _, r := range rows {
		ids2 = append(ids2, r.ReviewID)
	}
	imagesByReview := dao.ListReviewImagesByReviewIDs(ids2)
	items := make([]dto.Review, 0, len(rows))
	for _, r := range rows {
		ctype := dto.ReviewCustomerAnonymous
		switch {
		case r.CustomerID > 0 && alive[r.CustomerID]:
			ctype = dto.ReviewCustomerMember
		case r.CustomerID > 0:
			ctype = dto.ReviewCustomerDeleted
		}
		item := dto.FromReview(r, ctype)
		for _, n := range imagesByReview[r.ReviewID] {
			item.Images = append(item.Images, store.UploadURLPrefix+n)
		}
		items = append(items, item)
	}

	summary, err := buildReviewSummary(q)
	if err != nil {
		return AdminReviewListResult{}, err
	}
	return AdminReviewListResult{Total: total, Items: items, Summary: summary}, nil
}

// buildReviewSummary 在「与列表同一筛选条件」下聚合均分/分布/差评率/标签频次;
// 近 7 日均分不受筛选影响(始终是全局趋势参考)。
func buildReviewSummary(q dao.ReviewQuery) (dto.ReviewSummary, error) {
	out := dto.ReviewSummary{Dist: map[string]int{"1": 0, "2": 0, "3": 0, "4": 0, "5": 0}, Tags: []dto.ReviewTagCount{}}
	rows, err := dao.ReviewSummaryRows(q)
	if err != nil {
		return out, err
	}
	var sum, bad int
	tagCount := map[string]int{}
	for _, r := range rows {
		out.Total++
		sum += r.Rating
		if r.Rating <= 2 {
			bad++
		}
		out.Dist[fmt.Sprintf("%d", r.Rating)]++
		for _, t := range dto.SplitReviewTags(r.Tags) {
			tagCount[t]++
		}
	}
	if out.Total > 0 {
		out.AvgScore = po.Round2(float64(sum) / float64(out.Total))
		out.BadRate = po.Round2(float64(bad) / float64(out.Total))
	}
	out.BadCount = bad
	for name, n := range tagCount {
		out.Tags = append(out.Tags, dto.ReviewTagCount{Name: name, Count: n})
	}
	sort.Slice(out.Tags, func(i, j int) bool {
		if out.Tags[i].Count != out.Tags[j].Count {
			return out.Tags[i].Count > out.Tags[j].Count
		}
		return out.Tags[i].Name < out.Tags[j].Name
	})
	if len(out.Tags) > 12 {
		out.Tags = out.Tags[:12]
	}
	// 近 7 日(含今日)均分:不受列表筛选影响,给管理者一个稳定的趋势锚点。
	recent, err := dao.ReviewSummaryRows(dao.ReviewQuery{StartDay: time.Now().AddDate(0, 0, -6).Format("2006-01-02")})
	if err != nil {
		return out, err
	}
	if len(recent) > 0 {
		var s int
		for _, r := range recent {
			s += r.Rating
		}
		out.Last7Avg = po.Round2(float64(s) / float64(len(recent)))
	}
	return out, nil
}

// AdminDeleteReview 管理端删除不当评价,返回审计摘要(单号/星级/内容前段)。
func AdminDeleteReview(reviewID int, operator string) (string, error) {
	// 删除是物理删除,审计摘要必须先取原文:事后连查都查不回来。
	r, err := dao.GetReviewByID(reviewID)
	if err != nil || r.ReviewID == 0 {
		return "", errors.New("评价不存在")
	}
	n, err := dao.DeleteReview(reviewID)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", errors.New("评价不存在")
	}
	// 级联删图片:BLOB 大对象不随评价残留;摘要里带张数供审计核对。
	var imgCount int
	if imgs := dao.ListReviewImagesByReviewIDs([]int{reviewID})[reviewID]; len(imgs) > 0 {
		imgCount = len(imgs)
		if err := dao.DeleteReviewImagesByReview(reviewID); err != nil {
			logger.Errorf("[review][告警] 评价已删除但其图片清理失败 review=%d: %v", reviewID, err)
		}
	}
	detail := fmt.Sprintf("删除评价 单%s %d星", r.OrderNo, r.Rating)
	if imgCount > 0 {
		detail += fmt.Sprintf(" 含图片%d张", imgCount)
	}
	if r.Content != "" {
		detail += " 内容:" + truncateRunes(r.Content, 50)
	}
	logger.Infof("[review] 管理员 %s 删除评价 %d(%s)", operator, reviewID, detail)
	return detail, nil
}

// StartReviewImageOrphanCleaner 启动评价图片孤儿清理(每 6 小时一轮):
// 顾客上传后放弃提交/提交失败的图(review_id=0)超过 24h 即整行删除。
// 与 wallet 充值过期清理同一模式(简单 for+time.Sleep,失败只告警下轮再试)。
func StartReviewImageOrphanCleaner() {
	go func() {
		for {
			cutoff := time.Now().Add(-24 * time.Hour).Format(conf.TimeLayout)
			if n, err := dao.CleanOrphanReviewImages(cutoff); err != nil {
				logger.Warnf("[review][告警] 评价图片孤儿清理失败(下轮重试): %v", err)
			} else if n > 0 {
				logger.Infof("[review] 已清理未绑定的评价图片 %d 张", n)
			}
			time.Sleep(6 * time.Hour)
		}
	}()
}
