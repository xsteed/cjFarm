package dao

import (
	"database/sql"
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/store"
)

// ==================== 订单评价(仅商家可见) ====================
// 设计与追溯快照的理由见 docs/design-docs/order-review/spec.md。

// ReviewCols 评价表完整列(与 ScanReview 顺序一一对应)。
const ReviewCols = `review_id, order_id, order_no, biz_date, table_id, table_no, table_name,
	person_count, customer_id, nickname, phone, rating, tags, content, reviewer_key, ip_hash, ua_hash,
	create_time`

// ScanReview 扫描一行评价记录。
func ScanReview(rows interface{ Scan(...interface{}) error }) (po.Review, error) {
	var r po.Review
	err := rows.Scan(&r.ReviewID, &r.OrderID, &r.OrderNo, &r.BizDate, &r.TableID, &r.TableNo, &r.TableName,
		&r.PersonCount, &r.CustomerID, &r.Nickname, &r.Phone, &r.Rating, &r.Tags, &r.Content,
		&r.ReviewerKey, &r.IpHash, &r.UaHash, TS(&r.CreateTime))
	return r, err
}

// GetOrderReviewTarget 查询提交评价所需的订单信息(归属校验 + 快照来源)。
//
// 只填充 po.Order 的列子集,其余为零值。table_no/table_name/person_count/biz_date
// 是评价行的快照来源;order_status/pay_status 是「可评状态」的判据。
func GetOrderReviewTarget(orderNo string) (po.Order, error) {
	var o po.Order
	err := store.DB.QueryRow(`SELECT order_id, table_id, table_no, table_name, person_count,
		biz_date, order_status, pay_status FROM tb_order WHERE order_no=?`, orderNo).
		Scan(&o.OrderID, &o.TableID, &o.TableNo, &o.TableName, &o.PersonCount,
			&o.BizDate, &o.OrderStatus, &o.PayStatus)
	return o, err
}

// ReviewMemberSnapshot 取会员的昵称与手机号快照(评价提交时一次性写死)。
//
// 手机号来自身份表 provider='phone'(getPhoneNumber 绑定);weapp-only 会员没有
// 手机号身份,返回空串 —— 回访线索只有昵称。档案不存在(已注销/ID 非法)返回
// 零值与 nil:评价提交不该因为快照缺失而失败,匿名单也走同一出口。
func ReviewMemberSnapshot(customerID int) (nickname, phone string) {
	if customerID <= 0 {
		return "", ""
	}
	// LEFT JOIN:没绑手机号的会员仍要拿到昵称。
	err := store.DB.QueryRow(`SELECT c.nickname, COALESCE(i.external_id,'')
		FROM tb_customer c
		LEFT JOIN tb_customer_identity i ON i.customer_id=c.customer_id AND i.provider='phone'
		WHERE c.customer_id=?`, customerID).Scan(&nickname, &phone)
	if err != nil {
		return "", ""
	}
	return nickname, phone
}

// GetReviewByKey 按「订单号 + 评价者键」取评价(顾客端「我是否已评」与提交去重共用)。
// reviewer_key 会员为服务端派生的 m:{id}、匿名即设备键。
func GetReviewByKey(orderNo, reviewerKey string) (po.Review, error) {
	row := store.DB.QueryRow(`SELECT `+ReviewCols+` FROM tb_review WHERE order_no=? AND reviewer_key=?`, orderNo, reviewerKey)
	return ScanReview(row)
}

// InsertReview 落一条评价。order_no 上的唯一索引兜底并发双击:后到者拿到
// INSERT 错误,由 service 层重查并转成「已评价」的友好语义(方言无关,
// 不依赖各库错误码)。
func InsertReview(r po.Review) (int64, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_review(order_id, order_no, biz_date, table_id, table_no, table_name,
		person_count, customer_id, nickname, phone, rating, tags, content, reviewer_key, ip_hash, ua_hash, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.OrderID, r.OrderNo, r.BizDate, r.TableID, r.TableNo, r.TableName,
		r.PersonCount, r.CustomerID, r.Nickname, r.Phone, r.Rating, r.Tags, r.Content,
		r.ReviewerKey, r.IpHash, r.UaHash, store.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertReviewTx 事务内落一条评价,返回主键(评价与图片绑定须同生共死)。
func InsertReviewTx(tx *sql.Tx, r po.Review) (int64, error) {
	res, err := tx.Exec(`INSERT INTO tb_review(order_id, order_no, biz_date, table_id, table_no, table_name,
		person_count, customer_id, nickname, phone, rating, tags, content, reviewer_key, ip_hash, ua_hash, create_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.OrderID, r.OrderNo, r.BizDate, r.TableID, r.TableNo, r.TableName,
		r.PersonCount, r.CustomerID, r.Nickname, r.Phone, r.Rating, r.Tags, r.Content,
		r.ReviewerKey, r.IpHash, r.UaHash, store.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ReviewQuery 管理端评价列表筛选;零值字段表示不限。
type ReviewQuery struct {
	StartDay   string // 营业日起(YYYY-MM-DD,含)
	EndDay     string // 营业日止(含)
	RatingMin  int    // 评分下限(含);0 = 不限
	RatingMax  int    // 评分上限(含);0 = 不限
	HasContent bool   // 仅看有文字评价
	// HasImages 图片筛选:true=仅看有图,false+HasNoImages=全部;
	// 三态由两个布尔表达(nil 省事但布尔已够):HasImages 与 HasNoImages 互斥使用。
	HasImages   bool   // 仅看有图片评价
	HasNoImages bool   // 仅看无图片评价
	Keyword     string // 内容/标签/桌台号模糊匹配( LIKE 转义后)
}

// reviewCond 由筛选条件生成 WHERE 子句与参数(列表/计数/汇总三处共用同一份,
// 保证 total、items 与 summary 口径一致)。
func reviewCond(q ReviewQuery) (string, []interface{}) {
	where := []string{"1=1"}
	args := []interface{}{}
	if q.StartDay != "" {
		where = append(where, "biz_date>=?")
		args = append(args, q.StartDay)
	}
	if q.EndDay != "" {
		where = append(where, "biz_date<=?")
		args = append(args, q.EndDay)
	}
	if q.RatingMin > 0 {
		where = append(where, "rating>=?")
		args = append(args, q.RatingMin)
	}
	if q.RatingMax > 0 {
		where = append(where, "rating<=?")
		args = append(args, q.RatingMax)
	}
	if q.HasContent {
		where = append(where, "content<>''")
	}
	// 有图/无图:EXISTS 半连接(两库方言一致);无图 = NOT EXISTS。
	if q.HasImages {
		where = append(where, "EXISTS (SELECT 1 FROM tb_review_image i WHERE i.review_id=tb_review.review_id)")
	}
	if q.HasNoImages {
		where = append(where, "NOT EXISTS (SELECT 1 FROM tb_review_image i WHERE i.review_id=tb_review.review_id)")
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		// 同一关键词匹配内容/标签/桌台号:三者任一命中即返回,LIKE 统一走转义。
		where = append(where, `(content LIKE ? ESCAPE '\' OR tags LIKE ? ESCAPE '\' OR table_no LIKE ? ESCAPE '\')`)
		like := store.LikeContains(kw)
		args = append(args, like, like, like)
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// ListReviews 按条件分页查询评价,返回总数与当前页(总数与列表同一 cond)。
func ListReviews(q ReviewQuery, pageNum, pageSize int) (total int, list []po.Review, err error) {
	cond, args := reviewCond(q)
	if err = store.DB.QueryRow(`SELECT COUNT(*) FROM tb_review`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := store.DB.Query(`SELECT `+ReviewCols+` FROM tb_review`+cond+
		` ORDER BY review_id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return total, nil, err
	}
	defer rows.Close()
	list = []po.Review{}
	for rows.Next() {
		r, scanErr := ScanReview(rows)
		if scanErr != nil {
			return 0, nil, scanErr
		}
		list = append(list, r)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, list, nil
}

// ReviewSummaryRow 汇总统计的中间行(rating + tags 原文,标签频次在 Go 侧拆分)。
type ReviewSummaryRow struct {
	Rating int
	Tags   string
}

// ReviewSummaryRows 取「与列表同一筛选条件」下的全部评分与标签行,供 service
// 聚合均分/分布/标签频次。单店评价量级(日均几十条、总量数万)全量读无压力,
// 不值得为聚合写三段 SQL 再对口径。
func ReviewSummaryRows(q ReviewQuery) ([]ReviewSummaryRow, error) {
	cond, args := reviewCond(q)
	rows, err := store.DB.Query(`SELECT rating, tags FROM tb_review`+cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReviewSummaryRow{}
	for rows.Next() {
		var r ReviewSummaryRow
		if err := rows.Scan(&r.Rating, &r.Tags); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CustomerAliveMap 批量判断会员档案是否仍存在(注销是物理删除,见 DeleteCustomerAccount)。
// 管理端评价列表据此把 customer_id>0 的行标注为 member / deleted ——
// 评价行自含快照,但「还能不能回访」取决于档案是否还在。
func CustomerAliveMap(ids []int) map[int]bool {
	out := map[int]bool{}
	if len(ids) == 0 {
		return out
	}
	placeholders := ""
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if len(args) > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	if len(args) == 0 {
		return out
	}
	rows, err := store.DB.Query(`SELECT customer_id FROM tb_customer WHERE customer_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			out[id] = true
		}
	}
	return out
}

// GetReviewByID 按主键取评价(管理端删除前的取文与审计摘要)。
func GetReviewByID(reviewID int) (po.Review, error) {
	row := store.DB.QueryRow(`SELECT `+ReviewCols+` FROM tb_review WHERE review_id=?`, reviewID)
	return ScanReview(row)
}

// DeleteReview 物理删除一条评价(管理端清理不当内容;操作审计另行留痕)。
// 返回影响行数,0 = 不存在(已被删)。
func DeleteReview(reviewID int) (int64, error) {
	res, err := store.DB.Exec(`DELETE FROM tb_review WHERE review_id=?`, reviewID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
