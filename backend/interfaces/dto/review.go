package dto

import (
	"strings"

	"dining-system/domain/po"
)

// 顾客身份三态(管理端评价列表展示用)。
const (
	ReviewCustomerAnonymous = "anonymous" // 匿名顾客(customer_id=0)
	ReviewCustomerMember    = "member"    // 在册会员(档案仍存在)
	ReviewCustomerDeleted   = "deleted"   // 已注销会员(档案已物理删除,仅存快照)
)

// SplitReviewTags 把库里的逗号分隔标签拆成数组(空串返回空数组)。
func SplitReviewTags(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// CustomerReviewStatus 顾客端「本单是否已评价」的出参(GET /customer/review)。
// 只回本单自己的评价内容,不含任何他人信息。
type CustomerReviewStatus struct {
	Reviewed bool     `json:"reviewed"`
	Rating   int      `json:"rating"`
	Tags     []string `json:"tags"`
	Content  string   `json:"content"`
}

// Review 管理端评价列表行(API 出参)。追溯快照字段全部带出,
// 顾客身份三态由 service 结合会员档案存活情况标注。
type Review struct {
	ReviewID     int      `json:"reviewId"`
	OrderID      int      `json:"orderId"`
	OrderNo      string   `json:"orderNo"`
	ShortNo      string   `json:"shortNo"`
	BizDate      string   `json:"bizDate"`
	TableID      int      `json:"tableId"`
	TableNo      string   `json:"tableNo"`
	TableName    string   `json:"tableName"`
	PersonCount  int      `json:"personCount"`
	CustomerID   int      `json:"customerId"`
	CustomerType string   `json:"customerType"` // anonymous / member / deleted
	Nickname     string   `json:"nickname"`
	Phone        string   `json:"phone"`
	Rating       int      `json:"rating"`
	Tags         []string `json:"tags"`
	Content      string   `json:"content"`
	// Images 评价图片的展示路径(/uploads/{name}),本体在 tb_review_image 独立表。
	Images     []string `json:"images"`
	CreateTime string   `json:"createTime"`
}

// FromReview 将持久化对象转为 API 出参;customerType 由调用方结合存活表标注。
func FromReview(p po.Review, customerType string) Review {
	if customerType == "" {
		if p.CustomerID <= 0 {
			customerType = ReviewCustomerAnonymous
		} else {
			customerType = ReviewCustomerMember
		}
	}
	return Review{
		ReviewID:     p.ReviewID,
		Images:       []string{},
		OrderID:      p.OrderID,
		OrderNo:      p.OrderNo,
		ShortNo:      po.ShortOrderNo(p.OrderNo),
		BizDate:      p.BizDate,
		TableID:      p.TableID,
		TableNo:      p.TableNo,
		TableName:    p.TableName,
		PersonCount:  p.PersonCount,
		CustomerID:   p.CustomerID,
		CustomerType: customerType,
		Nickname:     p.Nickname,
		Phone:        p.Phone,
		Rating:       p.Rating,
		Tags:         SplitReviewTags(p.Tags),
		Content:      p.Content,
		CreateTime:   p.CreateTime,
	}
}

// ReviewTagCount 标签频次(汇总出参)。
type ReviewTagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ReviewSummary 评价汇总(与列表同一筛选条件下的口径)。
type ReviewSummary struct {
	Total    int              `json:"total"`    // 评价总数(同筛选)
	AvgScore float64          `json:"avgScore"` // 平均分(0~5,两位小数;无评价为 0)
	BadCount int              `json:"badCount"` // 差评数(1~2 星)
	BadRate  float64          `json:"badRate"`  // 差评占比(0~1,两位小数)
	Dist     map[string]int   `json:"dist"`     // 评分分布 {"1":n,...,"5":n}
	Tags     []ReviewTagCount `json:"tags"`     // 标签频次(降序,Top 12)
	Last7Avg float64          `json:"last7Avg"` // 近 7 日平均分(不受筛选影响,趋势参考)
}
