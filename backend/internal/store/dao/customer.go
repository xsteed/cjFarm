// 顾客会员档案(tb_customer)的数据访问。
//
// 设计见 docs/design-docs/customer-member/spec.md:
//   - openid 唯一,「首次出现即注册,再次出现即登录」;
//   - 令牌无状态,登录态校验时要顺带查一次 status(停用立即生效);
//   - order_count / last_order_time 是会员卡展示用的冗余列,下单成功后尽力维护(失败不影响下单)。
package dao

import (
	"database/sql"
	"errors"

	"dining-system/infra/logger"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

const customerCols = `customer_id, openid, nickname, avatar_url, status, order_count, last_order_time, create_time, last_login_time, remark`

func scanCustomer(row *sql.Row) (*po.Customer, error) {
	var c po.Customer
	err := row.Scan(&c.CustomerID, &c.OpenID, &c.Nickname, &c.AvatarURL, &c.Status,
		&c.OrderCount, &c.LastOrderTime, &c.CreateTime, &c.LastLoginTime, &c.Remark)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCustomerByID 按主键取会员档案(登录态校验用)。不存在返回 (nil, nil)。
func GetCustomerByID(id int) (*po.Customer, error) {
	c, err := scanCustomer(store.DB.QueryRow(`SELECT `+customerCols+` FROM tb_customer WHERE customer_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// GetCustomerByOpenid 按 openid 取会员档案。不存在返回 (nil, nil) —— 登录流程据此判定 isNew。
func GetCustomerByOpenid(openid string) (*po.Customer, error) {
	c, err := scanCustomer(store.DB.QueryRow(`SELECT `+customerCols+` FROM tb_customer WHERE openid=?`, openid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// InsertCustomer 建会员档案,返回主键。openid 上的唯一索引兜底并发首登的幂等性:
// 两个并发请求同时建档,后到者拿到唯一冲突 → service 层转「按 openid 重查」走登录分支。
func InsertCustomer(c po.Customer) (int64, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_customer(openid, nickname, avatar_url, status, order_count, last_order_time, create_time, last_login_time, remark)
		VALUES(?,?,?,1,0,'',?,?,?)`,
		c.OpenID, c.Nickname, c.AvatarURL, store.Now(), store.Now(), c.Remark)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// TouchCustomerLogin 记录一次成功登录(时间)。失败仅告警,不影响登录结果。
func TouchCustomerLogin(id int) {
	if _, err := store.DB.Exec(`UPDATE tb_customer SET last_login_time=? WHERE customer_id=?`, store.Now(), id); err != nil {
		logger.Warnf("[member] 刷新会员登录时间失败(id=%d): %v", id, err)
	}
}

// UpdateCustomerProfile 更新昵称与头像(「完善资料」,均为用户主动填写)。
func UpdateCustomerProfile(id int, nickname, avatarURL string) error {
	_, err := store.DB.Exec(`UPDATE tb_customer SET nickname=?, avatar_url=? WHERE customer_id=?`,
		nickname, avatarURL, id)
	return err
}

// SetCustomerStatus 启用/停用会员。停用后现有令牌在下一次登录态校验时立即被拒
// (校验路径每次都查 status)。
func SetCustomerStatus(id, status int) error {
	_, err := store.DB.Exec(`UPDATE tb_customer SET status=? WHERE customer_id=?`, status, id)
	return err
}

// TouchCustomerOrder 下单成功后维护会员卡展示用的冗余列(累计次数 +1、最近下单时间)。
// 展示数据,失败不影响下单结果。
func TouchCustomerOrder(id int) {
	if _, err := store.DB.Exec(`UPDATE tb_customer SET order_count=order_count+1, last_order_time=? WHERE customer_id=?`,
		store.Now(), id); err != nil {
		logger.Warnf("[member] 维护会员订单计数失败(id=%d): %v", id, err)
	}
}

// CustomerOrderRow 「我的订单」列表行:只取会员页需要的字段,
// 不复用重量级的 po.Order(那里字段 30+,会员列表用不到)。
// json tag 与管理端接口保持 camelCase 口径。
type CustomerOrderRow struct {
	OrderID     int    `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	PickupNo    int    `json:"pickupNo"`
	TableID     int    `json:"tableId"`
	TableName   string `json:"tableName"`
	OrderStatus int    `json:"orderStatus"`
	PayStatus   int    `json:"payStatus"`
	TotalAmount int64  `json:"totalAmount"`
	CreateTime  string `json:"createTime"`
}

// ListOrdersByCustomer 分页取某会员的全部订单(跨桌,按下单时间倒序)。
// customerID<=0 防御性返回空结果(未登录不会有调用路径)。
func ListOrdersByCustomer(customerID, pageNum, pageSize int) (int, []CustomerOrderRow, error) {
	if customerID <= 0 {
		return 0, []CustomerOrderRow{}, nil
	}
	var total int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_order WHERE customer_id=?`, customerID).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := store.DB.Query(`SELECT order_id, order_no, COALESCE(pickup_no,0), COALESCE(table_id,0), COALESCE(table_name,''),
		order_status, pay_status, total_amount, COALESCE(create_time,'')
		FROM tb_order WHERE customer_id=? ORDER BY order_id DESC LIMIT ? OFFSET ?`,
		customerID, pageSize, (pageNum-1)*pageSize)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]CustomerOrderRow, 0, pageSize)
	for rows.Next() {
		var r CustomerOrderRow
		if err := rows.Scan(&r.OrderID, &r.OrderNo, &r.PickupNo, &r.TableID, &r.TableName,
			&r.OrderStatus, &r.PayStatus, &r.TotalAmount, &r.CreateTime); err != nil {
			return 0, nil, err
		}
		out = append(out, r)
	}
	return total, out, rows.Err()
}
