// 顾客会员的数据访问:档案(tb_customer) + 登录方式(tb_customer_identity)。
//
// 设计见 docs/design-docs/customer-member-identity/spec.md:
//   - 账号只由 customer_id 标识,(provider, external_id) 唯一 ——
//     「这个登录凭据首次出现即注册,再次出现即登录」;
//   - 建档与绑定必须同事务:分开写会在并发首登时留下没有登录方式的孤儿档案;
//   - 令牌无状态,登录态校验时要顺带查一次 status(停用立即生效);
//   - order_count / last_order_time 是会员卡展示用的冗余列,下单成功后尽力维护(失败不影响下单)。
package dao

import (
	"database/sql"
	"errors"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
)

const customerCols = `customer_id, openid, nickname, avatar_url, status, order_count, last_order_time, create_time, last_login_time, remark`

// customerColsAliased 同一个列清单的「带表别名」版本,供 JOIN 查询使用。
// 单列一份而不是拼接前缀:列清单是扫描顺序的唯一来源,拼错了只有运行时才发现。
const customerColsAliased = `c.customer_id, c.openid, c.nickname, c.avatar_url, c.status, c.order_count, c.last_order_time, c.create_time, c.last_login_time, c.remark`

func scanCustomer(row *sql.Row) (*po.Customer, error) {
	var c po.Customer
	err := row.Scan(&c.CustomerID, &c.OpenID, &c.Nickname, &c.AvatarURL, &c.Status,
		&c.OrderCount, TS(&c.LastOrderTime), TS(&c.CreateTime), TS(&c.LastLoginTime), &c.Remark)
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

// GetCustomerByIdentity 按「登录方式 + 该方式下的用户标识」查会员档案。
// 不存在返回 (nil, nil) —— 登录流程据此判定 isNew。
//
// 这是会员登录的唯一查档入口:openid 只是其中一种 external_id,
// 后续加手机号 / 密码 / Apple 登录时都是同一句 SQL。
func GetCustomerByIdentity(provider, externalID string) (*po.Customer, error) {
	c, err := scanCustomer(store.DB.QueryRow(`SELECT `+customerColsAliased+`
		FROM tb_customer_identity i JOIN tb_customer c ON c.customer_id = i.customer_id
		WHERE i.provider=? AND i.external_id=?`, provider, externalID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// InsertCustomerTx 在事务内建会员档案,返回主键。
// openid 列在 P0 过渡期仍为 NOT NULL:微信小程序登录时传 external_id,其它方式传空串。
func InsertCustomerTx(tx *sql.Tx, c po.Customer) (int64, error) {
	now := store.Now()
	res, err := tx.Exec(`INSERT INTO tb_customer(openid, nickname, avatar_url, status, order_count, last_order_time, create_time, last_login_time, remark)
		VALUES(?,?,?,1,0,'',?,?,?)`,
		c.OpenID, c.Nickname, c.AvatarURL, now, now, c.Remark)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertCustomer 非事务建档(直插场景 / 测试用)。
// 生产登录路径一律走 CreateCustomerWithIdentity —— 只建档案不绑身份会留下
// 没有登录方式的孤儿行,那种行没有任何入口能再次登录进去。
func InsertCustomer(c po.Customer) (int64, error) {
	var id int64
	err := store.WithTx(func(tx *sql.Tx) error {
		var e error
		id, e = InsertCustomerTx(tx, c)
		return e
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// InsertIdentityTx 在事务内绑定一条登录方式。
// (provider, external_id) 上的唯一索引兜底并发首登:后到者拿到唯一冲突,
// 事务整体回滚(档案也一并撤销),由 service 层按身份重查走登录分支。
func InsertIdentityTx(tx *sql.Tx, customerID int, provider, externalID, unionid string) error {
	// unionid 未绑开放平台时存 NULL 而不是空串:空串会在索引里与其它「未绑定」用户
	// 挤在同一组,后续按 unionid 合并多端时会误伤。
	uid := sql.NullString{String: unionid, Valid: unionid != ""}
	now := store.Now()
	_, err := tx.Exec(`INSERT INTO tb_customer_identity(customer_id, provider, external_id, credential, unionid, last_login_time, create_time)
		VALUES(?,?,?,'',?,?,?)`, customerID, provider, externalID, uid, now, now)
	return err
}

// CreateCustomerWithIdentity 事务内「建档 + 绑定登录方式」,返回会员 ID。
//
// 两条 INSERT 必须在同一事务里。并发首登时冲突可能落在任一条上
// (wx_mp 时第一条就撞 tb_customer.openid 的旧唯一索引,其它 provider 撞 identity
// 的唯一索引);分开写的话,第一条成功、第二条失败就会留下一条没有任何登录方式
// 指向的孤儿档案 —— 那种行再也登不进去,只能人工清库。
func CreateCustomerWithIdentity(provider, externalID, unionid string) (int, error) {
	id := 0
	err := store.WithTx(func(tx *sql.Tx) error {
		// P0 过渡:openid 列仍为 NOT NULL,微信登录写一份;其它 provider 在 P2 之前
		// 不会走到这里(P2 会放开可空并删列,见 spec §7.2)。
		openid := ""
		if provider == po.CustomerProviderWxMP {
			openid = externalID
		}
		pk, err := InsertCustomerTx(tx, po.Customer{OpenID: openid})
		if err != nil {
			return err
		}
		id = int(pk)
		return InsertIdentityTx(tx, id, provider, externalID, unionid)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// TouchCustomerLogin 记录一次成功登录(时间)。失败仅告警,不影响登录结果。
func TouchCustomerLogin(id int) {
	if _, err := store.DB.Exec(`UPDATE tb_customer SET last_login_time=?, update_time=? WHERE customer_id=?`, store.Now(), store.Now(), id); err != nil {
		logger.Warnf("[member] 刷新会员登录时间失败(id=%d): %v", id, err)
	}
}

// TouchIdentityLogin 记录「这条登录方式」最近一次使用时间。
// 与 TouchCustomerLogin 的分工:前者是账号级(会员卡展示),这里是凭据级 ——
// 一个会员绑了多种方式时,能看出他实际在哪个端活跃。
// 失败仅告警,不影响登录结果。
func TouchIdentityLogin(provider, externalID string) {
	if _, err := store.DB.Exec(`UPDATE tb_customer_identity SET last_login_time=?, update_time=? WHERE provider=? AND external_id=?`,
		store.Now(), store.Now(), provider, externalID); err != nil {
		logger.Warnf("[member] 刷新登录方式时间失败(%s): %v", provider, err)
	}
}

// UpdateCustomerProfile 更新昵称与头像(「完善资料」,均为用户主动填写)。
func UpdateCustomerProfile(id int, nickname, avatarURL string) error {
	_, err := store.DB.Exec(`UPDATE tb_customer SET nickname=?, avatar_url=?, update_time=? WHERE customer_id=?`,
		nickname, avatarURL, store.Now(), id)
	return err
}

// SetCustomerStatus 启用/停用会员。停用后现有令牌在下一次登录态校验时立即被拒
// (校验路径每次都查 status)。
func SetCustomerStatus(id, status int) error {
	_, err := store.DB.Exec(`UPDATE tb_customer SET status=?, update_time=? WHERE customer_id=?`, status, store.Now(), id)
	return err
}

// TouchCustomerOrder 下单成功后维护会员卡展示用的冗余列(累计次数 +1、最近下单时间)。
// 展示数据,失败不影响下单结果。
func TouchCustomerOrder(id int) {
	if _, err := store.DB.Exec(`UPDATE tb_customer SET order_count=order_count+1, last_order_time=?, update_time=? WHERE customer_id=?`,
		store.Now(), store.Now(), id); err != nil {
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
	// TotalAmount 单位是**元**(顾客端所有接口口径统一,与 dto.Order 一致)。
	TotalAmount float64 `json:"totalAmount"`
	CreateTime  string  `json:"createTime"`
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
		var totalCents int64
		if err := rows.Scan(&r.OrderID, &r.OrderNo, &r.PickupNo, &r.TableID, &r.TableName,
			&r.OrderStatus, &r.PayStatus, &totalCents, TS(&r.CreateTime)); err != nil {
			return 0, nil, err
		}
		// 库里存的是「分」,出参必须是「元」:顾客端所有接口都以元计价(同 dto.Order)。
		// 早先把分直接下发,小程序再按元格式化 —— ¥28.00 的单会显示成 ¥2800.00,
		// 同一个订单在订单页(走 po.ToYuan)与「我的订单」还会给出两个价。
		r.TotalAmount = po.ToYuan(totalCents)
		out = append(out, r)
	}
	return total, out, rows.Err()
}
