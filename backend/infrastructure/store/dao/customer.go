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
	"strings"

	"dining-system/domain/po"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
)

const customerCols = `customer_id, nickname, avatar_url, status, order_count, last_order_time, create_time, last_login_time, remark`

// customerColsAliased 同一个列清单的「带表别名」版本,供 JOIN 查询使用。
// 单列一份而不是拼接前缀:列清单是扫描顺序的唯一来源,拼错了只有运行时才发现。
const customerColsAliased = `c.customer_id, c.nickname, c.avatar_url, c.status, c.order_count, c.last_order_time, c.create_time, c.last_login_time, c.remark`

func scanCustomer(row *sql.Row) (*po.Customer, error) {
	var c po.Customer
	err := row.Scan(&c.CustomerID, &c.Nickname, &c.AvatarURL, &c.Status,
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
// 只写档案字段 —— 登录凭据由 InsertIdentityTx 单独绑定(两者同事务,见 CreateCustomerWithIdentity)。
func InsertCustomerTx(tx *sql.Tx, c po.Customer) (int64, error) {
	now := store.Now()
	res, err := tx.Exec(`INSERT INTO tb_customer(nickname, avatar_url, status, order_count, last_order_time, create_time, last_login_time, remark)
		VALUES(?,?,1,0,'',?,?,?)`,
		c.Nickname, c.AvatarURL, now, now, c.Remark)
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
// 两条 INSERT 必须在同一事务里:并发首登时后到者撞 identity 的唯一索引,
// 分开写的话第一条成功、第二条失败就会留下一条没有任何登录方式指向的孤儿档案
// —— 那种行再也登不进去,只能人工清库。
func CreateCustomerWithIdentity(provider, externalID, unionid string) (int, error) {
	id := 0
	err := store.WithTx(func(tx *sql.Tx) error {
		pk, err := InsertCustomerTx(tx, po.Customer{})
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

// ============================================================================
// 管理端:会员列表与注销
// ============================================================================

// MemberRow 管理端会员列表行。
//
// 与 po.Customer 的分工:这里只取后台表格需要的字段,并把「登录方式」聚合成
// 一个字符串(前端直接展示);po.Customer 用于登录态校验与顾客端会员卡。
type MemberRow struct {
	CustomerID    int    `json:"customerId"`
	Nickname      string `json:"nickname"`
	AvatarURL     string `json:"avatarUrl"`
	Status        int    `json:"status"`
	OrderCount    int    `json:"orderCount"`
	LastOrderTime string `json:"lastOrderTime"`
	CreateTime    string `json:"createTime"`
	LastLoginTime string `json:"lastLoginTime"`
	// Providers 该会员绑定的登录方式(逗号分隔,如 "wx_mp,phone");未绑定时为空串。
	Providers string `json:"providers"`
	// Phone 绑定的手机号(管理端展示/回拨用,完整号码;只对拥有 member:view 的人可见)。
	Phone string `json:"phone"`
	// Balance 储值余额(元,出参口径与订单金额一致)。从未充过值的会员为 0。
	Balance float64 `json:"balance"`
}

// MemberQuery 会员列表筛选条件。
//
// Status 用指针而不是 int:0 表示「停用」,若用 int,零值 MemberQuery{} 会被
// 误当成「只看停用会员」—— 与 dao.UserQuery 同一个坑,这里保持一致。
type MemberQuery struct {
	Keyword string // 匹配昵称
	Status  *int   // nil 表示不限
}

// ListMembers 分页查询会员(按入会时间倒序,即 customer_id 倒序)。
func ListMembers(q MemberQuery, pageNum, pageSize int) (int, []MemberRow, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		// 昵称或手机号(手机号在身份表里,用 EXISTS 而不是 JOIN,避免与登录方式聚合重复)。
		where = append(where, `(c.nickname LIKE ? OR EXISTS (
			SELECT 1 FROM tb_customer_identity i2
			WHERE i2.customer_id=c.customer_id AND i2.provider='phone' AND i2.external_id LIKE ?))`)
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	if q.Status != nil {
		where = append(where, "c.status=?")
		args = append(args, *q.Status)
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_customer c`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	// 登录方式用相关子查询聚合,而不是 LEFT JOIN + GROUP BY:
	// MySQL 开启 ONLY_FULL_GROUP_BY 时,GROUP BY 主键虽被允许,但一旦有人改查询
	// 就容易踩到;子查询两种方言行为一致,且会员量级很小(万级以内),开销可忽略。
	rows, err := store.DB.Query(`SELECT c.customer_id, COALESCE(c.nickname,''), COALESCE(c.avatar_url,''),
		c.status, c.order_count, COALESCE(c.last_order_time,''), COALESCE(c.create_time,''), COALESCE(c.last_login_time,''),
		COALESCE((SELECT GROUP_CONCAT(i.provider) FROM tb_customer_identity i WHERE i.customer_id=c.customer_id), ''),
		COALESCE((SELECT i3.external_id FROM tb_customer_identity i3 WHERE i3.customer_id=c.customer_id AND i3.provider='phone' LIMIT 1), ''),
		COALESCE((SELECT w.balance FROM tb_wallet w WHERE w.customer_id=c.customer_id), 0)
		FROM tb_customer c`+cond+` ORDER BY c.customer_id DESC LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]MemberRow, 0, pageSize)
	for rows.Next() {
		var r MemberRow
		var balanceCents int64
		if err := rows.Scan(&r.CustomerID, &r.Nickname, &r.AvatarURL, &r.Status, &r.OrderCount,
			TS(&r.LastOrderTime), TS(&r.CreateTime), TS(&r.LastLoginTime), &r.Providers, &r.Phone, &balanceCents); err != nil {
			return 0, nil, err
		}
		// 库里存「分」,出参一律「元」(与订单金额同一口径)。
		r.Balance = po.ToYuan(balanceCents)
		out = append(out, r)
	}
	return total, out, rows.Err()
}

// DeleteCustomerAccount 注销会员:清订单归属 + 删登录方式 + 删档案,整体一个事务。
//
// 订单本身保留(它是门店的经营数据,也是报表口径的来源),只把 customer_id
// 置回 0 变回「匿名单」;登录方式与档案物理删除(spec §2.6:会员不做软删除)。
// 访问统计里的 customer_id 不回填 —— 它只用于会员 UV 聚合,SQLite 的
// AUTOINCREMENT 不会复用主键,残留值不会指向新会员。
func DeleteCustomerAccount(customerID int) error {
	if customerID <= 0 {
		return errors.New("会员不存在")
	}
	return store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tb_order SET customer_id=0 WHERE customer_id=?`, customerID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tb_customer_identity WHERE customer_id=?`, customerID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM tb_customer WHERE customer_id=?`, customerID)
		return err
	})
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
