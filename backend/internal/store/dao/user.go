package dao

import (
	"database/sql"
	"fmt"
	"strings"

	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ============================================================================
// 员工与角色数据访问
//
// 设计要点见 docs/user-permission-design.md:
//   - 内置角色(admin/manager/cashier/staff)由 SyncBuiltinRoles 保证存在,
//     其中 admin 的权限每次启动强制恢复为全量 —— 这是防锁死的最后保险;
//   - 所有「会导致系统失去管理员」的写操作都在事务内先查数量再改,
//     避免两个并发请求各自通过检查;
//   - 用户名/角色标识的唯一性由应用层保证(MySQL 下部分唯一索引会降级为普通索引)。
// ============================================================================

// RuleError 业务规则错误,Msg 为可直接展示给用户的中文提示。
type RuleError struct{ Msg string }

func (e *RuleError) Error() string { return e.Msg }

func ruleErr(format string, a ...interface{}) error {
	return &RuleError{Msg: fmt.Sprintf(format, a...)}
}

// IsRuleError 判断错误是否为业务规则错误(用于决定是否原样回显给用户)。
func IsRuleError(err error) bool {
	_, ok := err.(*RuleError)
	return ok
}

// ============================================================================
// 账号 + 角色快照(鉴权中间件每次请求加载一次)
// ============================================================================

// AuthInfo 一次查询取出的账号与角色信息,供鉴权与「我的信息」接口使用。
type AuthInfo struct {
	UserID       int
	Username     string
	RealName     string
	Status       int // 账号状态 1 启用 / 0 停用
	TokenVersion int
	RoleID       int
	RoleKey      string
	RoleName     string
	RoleStatus   int // 角色状态 1 启用 / 0 停用
	Perms        []string
}

// DisplayName 留痕显示名:优先中文姓名,未填则退回登录名。
func (a *AuthInfo) DisplayName() string {
	if strings.TrimSpace(a.RealName) != "" {
		return a.RealName
	}
	return a.Username
}

// userAuthCols 账号 + 角色的联合查询列。角色用 LEFT JOIN,角色缺失时返回零值,
// 由调用方判定「角色已失效」并拒绝访问(而不是当成无角色放行)。
const userAuthCols = `u.user_id, u.username, COALESCE(u.real_name,''), u.status, COALESCE(u.token_version,0),
	COALESCE(r.role_id,0), COALESCE(r.role_key,''), COALESCE(r.role_name,''), COALESCE(r.status,0), COALESCE(r.perms,'')`

func scanAuth(row interface{ Scan(...interface{}) error }) (*AuthInfo, error) {
	var a AuthInfo
	var perms string
	if err := row.Scan(&a.UserID, &a.Username, &a.RealName, &a.Status, &a.TokenVersion,
		&a.RoleID, &a.RoleKey, &a.RoleName, &a.RoleStatus, &perms); err != nil {
		return nil, err
	}
	a.Perms = store.ParsePerms(perms)
	return &a, nil
}

// GetAuthByID 按账号 ID 加载鉴权信息(鉴权中间件用)。
func GetAuthByID(userID int) (*AuthInfo, error) {
	row := store.DB.QueryRow(`SELECT `+userAuthCols+`
		FROM tb_user u
		LEFT JOIN tb_role r ON r.role_id = u.role_id AND r.del_flag='0'
		WHERE u.user_id=? AND u.del_flag='0'`, userID)
	return scanAuth(row)
}

// GetAuthByUsername 按用户名加载鉴权信息(登录用)。
func GetAuthByUsername(username string) (*AuthInfo, error) {
	row := store.DB.QueryRow(`SELECT `+userAuthCols+`
		FROM tb_user u
		LEFT JOIN tb_role r ON r.role_id = u.role_id AND r.del_flag='0'
		WHERE u.username=? AND u.del_flag='0'`, username)
	return scanAuth(row)
}

// GetUserPasswordHash 取指定账号的密码哈希。
func GetUserPasswordHash(userID int) string {
	var h string
	store.DB.QueryRow(`SELECT COALESCE(password_hash,'') FROM tb_user WHERE user_id=? AND del_flag='0'`, userID).Scan(&h)
	return h
}

// RecordLogin 记录一次成功登录(时间/IP/次数)。
func RecordLogin(userID int, ip string) {
	store.DB.Exec(`UPDATE tb_user SET last_login_time=?, last_login_ip=?, login_count=COALESCE(login_count,0)+1
		WHERE user_id=?`, store.Now(), ip, userID)
}

// CountUsers 未删除的账号总数(用于判断「是否首次启动」)。
func CountUsers() int {
	var n int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_user WHERE del_flag='0'`).Scan(&n)
	return n
}

// ============================================================================
// 员工查询
// ============================================================================

// UserQuery 员工列表筛选条件。
//
// Status 用指针而不是 int:账号状态里 0 表示「停用」,若用 int,零值 UserQuery{}
// 会被误当成「只看停用的账号」(这个坑在单元测试里真的踩到过)。
// 指针的 nil 明确表示「不限」。
type UserQuery struct {
	Keyword string // 匹配用户名 / 姓名 / 手机号
	RoleID  int    // 0 表示不限
	Status  *int   // nil 表示不限
}

// ListUsers 分页查询员工。
func ListUsers(q UserQuery, pageNum, pageSize int) (int, []UserRow, error) {
	where := []string{"u.del_flag='0'"}
	args := []interface{}{}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		where = append(where, "(u.username LIKE ? OR COALESCE(u.real_name,'') LIKE ? OR COALESCE(u.phone,'') LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	if q.RoleID > 0 {
		where = append(where, "u.role_id=?")
		args = append(args, q.RoleID)
	}
	if q.Status != nil {
		where = append(where, "u.status=?")
		args = append(args, *q.Status)
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_user u`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}

	rows, err := store.DB.Query(`SELECT `+userCols+`
		FROM tb_user u
		LEFT JOIN tb_role r ON r.role_id = u.role_id
		`+cond+` ORDER BY u.user_id LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	list := []UserRow{}
	for rows.Next() {
		u, scanErr := scanUser(rows)
		if scanErr != nil {
			return 0, nil, scanErr
		}
		list = append(list, *u)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	return total, list, nil
}

// UserRow 员工+角色联表查询投影;RoleKey/RoleName 来自 tb_role。
type UserRow struct {
	po.User
	RoleKey  string
	RoleName string
}

const userCols = `u.user_id, u.username, COALESCE(u.real_name,''), COALESCE(u.role_id,0), COALESCE(u.phone,''), u.status,
	COALESCE(u.last_login_time,''), COALESCE(u.last_login_ip,''), COALESCE(u.login_count,0),
	COALESCE(u.pwd_update_time,''), COALESCE(u.create_by,''), COALESCE(u.create_time,''),
	COALESCE(u.update_by,''), COALESCE(u.update_time,''), COALESCE(u.remark,''),
	COALESCE(r.role_key,''), COALESCE(r.role_name,'')`

func scanUser(rows *sql.Rows) (*UserRow, error) {
	var u UserRow
	err := rows.Scan(&u.UserID, &u.Username, &u.RealName, &u.RoleID, &u.Phone, &u.Status,
		&u.LastLoginTime, &u.LastLoginIP, &u.LoginCount,
		&u.PwdUpdateTime, &u.CreateBy, &u.CreateTime,
		&u.UpdateBy, &u.UpdateTime, &u.Remark,
		&u.RoleKey, &u.RoleName)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByID 按 ID 取单个员工。
func GetUserByID(userID int) (*UserRow, error) {
	rows, err := store.DB.Query(`SELECT `+userCols+`
		FROM tb_user u
		LEFT JOIN tb_role r ON r.role_id = u.role_id
		WHERE u.user_id=? AND u.del_flag='0'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		return scanUser(rows)
	}
	return nil, sql.ErrNoRows
}

// GetUserByUsername 按用户名取员工(不存在返回 nil, nil)。
func GetUserByUsername(username string) (*UserRow, error) {
	rows, err := store.DB.Query(`SELECT `+userCols+`
		FROM tb_user u
		LEFT JOIN tb_role r ON r.role_id = u.role_id
		WHERE u.username=? AND u.del_flag='0'`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		return scanUser(rows)
	}
	return nil, nil
}

// UsernameExists 判断用户名是否已被占用(仅看未删除的行)。
func UsernameExists(username string, excludeUserID int) (bool, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_user WHERE username=? AND del_flag='0' AND user_id<>?`,
		username, excludeUserID).Scan(&n)
	return n > 0, err
}

// EnsureUsernameAvailable 用户名唯一性校验(MySQL 下无部分唯一索引,必须走应用层)。
func EnsureUsernameAvailable(username string, excludeUserID int) error {
	taken, err := UsernameExists(username, excludeUserID)
	if err != nil {
		return err
	}
	if taken {
		return ruleErr("用户名【%s】已存在,请换一个", username)
	}
	return nil
}

// ============================================================================
// 员工写入
// ============================================================================

// InsertUser 新增员工。passwordHash 由调用方用 HashPassword 生成。
func InsertUser(u po.User, passwordHash, operatorName string) (int64, error) {
	u.Username = strings.TrimSpace(u.Username)
	if err := EnsureUsernameAvailable(u.Username, 0); err != nil {
		return 0, err
	}
	now := store.Now()
	res, err := store.DB.Exec(`INSERT INTO tb_user
		(username, password_hash, real_name, role_id, phone, status, pwd_update_time, token_version,
		 del_flag, create_by, create_time, update_by, update_time, remark)
		VALUES(?,?,?,?,?,?,?,0,'0',?,?,?,?,?)`,
		u.Username, passwordHash, u.RealName, u.RoleID, u.Phone, po.UserStatusEnabled,
		now, operatorName, now, operatorName, now, u.Remark)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUserProfile 修改员工资料(姓名/角色/手机/备注)。
//
// 防锁死校验:不允许改自己的角色;不允许把「最后一个启用的超级管理员」改成别的角色。
func UpdateUserProfile(u po.User, operatorID int, operatorName string) error {
	return store.WithTx(func(tx *sql.Tx) error {
		var oldRoleID, oldStatus int
		if err := tx.QueryRow(`SELECT COALESCE(role_id,0), status FROM tb_user
			WHERE user_id=? AND del_flag='0'`, u.UserID).Scan(&oldRoleID, &oldStatus); err != nil {
			return ruleErr("账号不存在")
		}
		if u.UserID == operatorID && u.RoleID != oldRoleID {
			return ruleErr("不能修改自己的角色")
		}
		// 若把「启用中的超级管理员」移出 admin 角色,需保证还有别人在管。
		if oldRoleID != u.RoleID && oldStatus == po.UserStatusEnabled && isAdminRoleID(oldRoleID) {
			// 先锁目标用户行再计数:避免「查数量→改行」之间的 check-then-act 竞态(见 TouchUserTx)。
			if err := TouchUserTx(tx, u.UserID); err != nil {
				return err
			}
			n, err := countActiveAdminsTx(tx, u.UserID)
			if err != nil {
				return err
			}
			if n == 0 {
				return ruleErr("系统必须保留至少一个启用的超级管理员")
			}
		}
		_, err := tx.Exec(`UPDATE tb_user SET real_name=?, role_id=?, phone=?, remark=?, update_by=?, update_time=?
			WHERE user_id=? AND del_flag='0'`,
			u.RealName, u.RoleID, u.Phone, u.Remark, operatorName, store.Now(), u.UserID)
		return err
	})
}

// SetUserPassword 重置密码。token_version +1 使该账号的旧令牌立即失效。
func SetUserPassword(userID int, passwordHash, operatorName string) error {
	res, err := store.DB.Exec(`UPDATE tb_user SET password_hash=?, pwd_update_time=?, token_version=COALESCE(token_version,0)+1,
		update_by=?, update_time=? WHERE user_id=? AND del_flag='0'`,
		passwordHash, store.Now(), operatorName, store.Now(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ruleErr("账号不存在")
	}
	return nil
}

// SetUserStatus 启用 / 停用账号(停用后旧令牌立即失效,因为中间件会检查 status)。
//
// 防锁死校验:不能停用自己;不能停用最后一个启用的超级管理员。
func SetUserStatus(userID, status, operatorID int, operatorName string) error {
	if status != po.UserStatusEnabled && status != po.UserStatusDisabled {
		return ruleErr("状态值非法")
	}
	if userID == operatorID && status == po.UserStatusDisabled {
		return ruleErr("不能停用当前登录账号")
	}
	return store.WithTx(func(tx *sql.Tx) error {
		var roleID, oldStatus int
		if err := tx.QueryRow(`SELECT COALESCE(role_id,0), status FROM tb_user
			WHERE user_id=? AND del_flag='0'`, userID).Scan(&roleID, &oldStatus); err != nil {
			return ruleErr("账号不存在")
		}
		if status == po.UserStatusDisabled && oldStatus == po.UserStatusEnabled && isAdminRoleID(roleID) {
			// 先锁目标用户行再计数:避免「查数量→停用」之间的 check-then-act 竞态(见 TouchUserTx)。
			if err := TouchUserTx(tx, userID); err != nil {
				return err
			}
			n, err := countActiveAdminsTx(tx, userID)
			if err != nil {
				return err
			}
			if n == 0 {
				return ruleErr("系统必须保留至少一个启用的超级管理员,无法停用该账号")
			}
		}
		_, err := tx.Exec(`UPDATE tb_user SET status=?, update_by=?, update_time=? WHERE user_id=? AND del_flag='0'`,
			status, operatorName, store.Now(), userID)
		if err != nil {
			return err
		}
		// 停用即作废该账号的全部「记住我」会话。
		// 必须走事务版:这里已持有写锁,再用连接池的其它连接写库会 SQLITE_BUSY。
		if status == po.UserStatusDisabled {
			if e := deleteRememberTokensByUserTx(tx, userID); e != nil {
				return e
			}
		}
		return nil
	})
}

// SoftDeleteUser 软删除账号(保留订单等历史数据里的操作人姓名,不做物理删除)。
func SoftDeleteUser(userID, operatorID int, operatorName string) error {
	if userID == operatorID {
		return ruleErr("不能删除当前登录账号")
	}
	return store.WithTx(func(tx *sql.Tx) error {
		var roleID, status int
		if err := tx.QueryRow(`SELECT COALESCE(role_id,0), status FROM tb_user
			WHERE user_id=? AND del_flag='0'`, userID).Scan(&roleID, &status); err != nil {
			return ruleErr("账号不存在")
		}
		if status == po.UserStatusEnabled && isAdminRoleID(roleID) {
			// 先锁目标用户行再计数:避免「查数量→删除」之间的 check-then-act 竞态(见 TouchUserTx)。
			if err := TouchUserTx(tx, userID); err != nil {
				return err
			}
			n, err := countActiveAdminsTx(tx, userID)
			if err != nil {
				return err
			}
			if n == 0 {
				return ruleErr("系统必须保留至少一个启用的超级管理员,无法删除该账号")
			}
		}
		_, err := tx.Exec(`UPDATE tb_user SET del_flag='1', update_by=?, update_time=? WHERE user_id=?`,
			operatorName, store.Now(), userID)
		if err != nil {
			return err
		}
		// 删除账号:作废其全部「记住我」会话(事务版,理由同上)。
		if e := deleteRememberTokensByUserTx(tx, userID); e != nil {
			return e
		}
		return nil
	})
}

// ============================================================================
// 角色查询
// ============================================================================

// RoleRow 角色+运行时补算字段的查询投影;PermList/UserCount 不入库。
type RoleRow struct {
	po.Role
	PermList  []string
	UserCount int
}

const roleCols = `role_id, role_key, role_name, COALESCE(perms,''), COALESCE(data_scope,'all'), COALESCE(is_builtin,0),
	sort_order, status, COALESCE(create_time,''), COALESCE(update_time,''), COALESCE(remark,'')`

func scanRole(rows *sql.Rows) (*RoleRow, error) {
	var r RoleRow
	err := rows.Scan(&r.RoleID, &r.RoleKey, &r.RoleName, &r.Perms, &r.DataScope, &r.IsBuiltin,
		&r.SortOrder, &r.Status, &r.CreateTime, &r.UpdateTime, &r.Remark)
	if err != nil {
		return nil, err
	}
	r.PermList = store.ParsePerms(r.Perms)
	return &r, nil
}

// ListRoles 全部未删除角色(按排序号),并补算每个角色的员工数。
func ListRoles() ([]RoleRow, error) {
	// 员工数一次 GROUP BY 聚合拿全,避免逐角色 CountUsersByRole 的 N+1 查询。
	counts, err := CountUsersByRoleGroup()
	if err != nil {
		return nil, err
	}
	rows, err := store.DB.Query(`SELECT ` + roleCols + ` FROM tb_role WHERE del_flag='0' ORDER BY sort_order, role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []RoleRow{}
	for rows.Next() {
		if r, err := scanRole(rows); err == nil {
			r.UserCount = counts[r.RoleID]
			list = append(list, *r)
		}
	}
	return list, nil
}

// GetRoleByID 按 ID 取角色。
func GetRoleByID(roleID int) (*RoleRow, error) {
	rows, err := store.DB.Query(`SELECT `+roleCols+` FROM tb_role WHERE role_id=? AND del_flag='0'`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		return scanRole(rows)
	}
	return nil, sql.ErrNoRows
}

// RoleIDByKey 按标识取角色 ID(内置角色用),不存在返回 0。
func RoleIDByKey(key string) int {
	var id int
	store.DB.QueryRow(`SELECT role_id FROM tb_role WHERE role_key=? AND del_flag='0'`, key).Scan(&id)
	return id
}

// isAdminRoleID 判断角色 ID 是否为超级管理员。
func isAdminRoleID(roleID int) bool {
	if roleID <= 0 {
		return false
	}
	return roleID == RoleIDByKey(store.RoleKeyAdmin)
}

// RoleKeyExists 角色标识是否已占用。
func RoleKeyExists(key string, excludeRoleID int) (bool, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_role WHERE role_key=? AND del_flag='0' AND role_id<>?`,
		key, excludeRoleID).Scan(&n)
	return n > 0, err
}

// CountUsersByRole 引用该角色的未删除员工数。
func CountUsersByRole(roleID int) (int, error) {
	var n int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM tb_user WHERE role_id=? AND del_flag='0'`, roleID).Scan(&n)
	return n, err
}

// CountUsersByRoleGroup 统计各角色引用的未删除员工数,key 为 role_id。
// 软删过滤条件与 CountUsersByRole 完全一致(del_flag='0'),仅把逐角色 COUNT
// 合并为一次 GROUP BY 聚合,供 ListRoles 一次补齐全部角色的 UserCount。
func CountUsersByRoleGroup() (map[int]int, error) {
	rows, err := store.DB.Query(`SELECT role_id, COUNT(*) FROM tb_user WHERE del_flag='0' GROUP BY role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[int]int{}
	for rows.Next() {
		var roleID, n int
		if err := rows.Scan(&roleID, &n); err != nil {
			return nil, err
		}
		counts[roleID] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// CountActiveAdmins 启用中的超级管理员数量(不排除任何人)。
func CountActiveAdmins() int {
	n, _ := countActiveAdmins(store.DB, 0)
	return n
}

// queryer 抽象 *sql.DB 与 *sql.Tx 的公共查询能力。
type queryer interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// countActiveAdmins 统计启用中的超级管理员,excludeUserID>0 时排除该账号
// (校验「停用/删除/改角色后是否还有人能管权限」时用)。
func countActiveAdmins(q queryer, excludeUserID int) (int, error) {
	adminRoleID := RoleIDByKey(store.RoleKeyAdmin)
	if adminRoleID == 0 {
		return 0, nil
	}
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM tb_user
		WHERE del_flag='0' AND status=? AND role_id=? AND user_id<>?`,
		po.UserStatusEnabled, adminRoleID, excludeUserID).Scan(&n)
	return n, err
}

func countActiveAdminsTx(tx *sql.Tx, excludeUserID int) (int, error) {
	return countActiveAdmins(tx, excludeUserID)
}

// TouchUserTx 对用户行做自赋值 no-op 更新,先取该行写锁,把「保留最后一个启用超级管理员」
// 的 check-then-act 竞态(查数量→改行)在行上串行化。
//
// 为什么自赋值 UPDATE 能取锁:UPDATE 无论是否真的改变列值,都会先按 WHERE 定位行并加写锁;
// SET update_time=update_time 不产生可见变更,仅用于占锁,与 dao.TouchOrderTx 同模式。
//
// 两库行为差异:
//   - SQLite 写锁是库级的,任一笔自赋值 UPDATE 都会让其它连接上的写操作等待,天然全局串行;
//   - MySQL 是行锁,只对同一 user_id 的行互斥(不同目标用户仍可能并发)。因此本锁保证的是
//     「同一目标用户」的停用/删除/改角色在计数与落库之间不交错;跨用户并发由调用方按业务
//     上限约束兜底(最后一道保险仍是 SyncBuiltinRoles 每次启动强制恢复 admin 权限)。
func TouchUserTx(tx *sql.Tx, userID int) error {
	_, err := tx.Exec(`UPDATE tb_user SET update_time=update_time WHERE user_id=?`, userID)
	return err
}

// ============================================================================
// 角色写入
// ============================================================================

// InsertRole 新增自定义角色。permList 为解析后的权限码列表,落库前由 JoinPerms 序列化。
func InsertRole(r po.Role, permList []string, operatorName string) (int64, error) {
	key := strings.TrimSpace(r.RoleKey)
	if key == "" {
		return 0, ruleErr("请填写角色标识")
	}
	if store.IsBuiltinRoleKey(key) {
		return 0, ruleErr("角色标识【%s】为内置角色保留,请换一个", key)
	}
	taken, err := RoleKeyExists(key, 0)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, ruleErr("角色标识【%s】已存在", key)
	}
	now := store.Now()
	res, err := store.DB.Exec(`INSERT INTO tb_role
		(role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark)
		VALUES(?,?,?,?,0,?,?, '0',?,?,?)`,
		key, r.RoleName, store.JoinPerms(permList), "all", r.SortOrder, po.UserStatusEnabled, now, now, r.Remark)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateRole 修改角色(名称/权限/排序/备注)。内置角色的标识与权限不可改。
func UpdateRole(r po.Role, permList []string, operatorName string) error {
	old, err := GetRoleByID(r.RoleID)
	if err != nil {
		return ruleErr("角色不存在")
	}
	if old.RoleKey == store.RoleKeyAdmin {
		return ruleErr("超级管理员角色的权限不可修改")
	}
	if old.IsBuiltin == 1 && r.RoleKey != "" && r.RoleKey != old.RoleKey {
		return ruleErr("内置角色的标识不可修改")
	}
	_, err = store.DB.Exec(`UPDATE tb_role SET role_name=?, perms=?, sort_order=?, remark=?, update_time=?
		WHERE role_id=? AND del_flag='0'`,
		r.RoleName, store.JoinPerms(permList), r.SortOrder, r.Remark, store.Now(), r.RoleID)
	return err
}

// SoftDeleteRole 删除角色(内置角色与仍被员工引用的角色不可删)。
func SoftDeleteRole(roleID int, operatorName string) error {
	old, err := GetRoleByID(roleID)
	if err != nil {
		return ruleErr("角色不存在")
	}
	if old.IsBuiltin == 1 {
		return ruleErr("内置角色【%s】不可删除", old.RoleName)
	}
	n, err := CountUsersByRole(roleID)
	if err != nil {
		return err
	}
	if n > 0 {
		return ruleErr("该角色下还有 %d 名员工,请先调整他们的角色后再删除", n)
	}
	_, err = store.DB.Exec(`UPDATE tb_role SET del_flag='1', update_time=? WHERE role_id=?`, store.Now(), roleID)
	return err
}

// WarnDefaultAdminPassword 启动时探测引导管理员是否仍在使用默认口令 admin123。
//
// 默认口令写在 README 里,等于公开;公网部署的商户若从不改密,任何人都能
// 以超管身份登录。此前只有创建时的一句 Infof,商户根本不会注意 ——
// 这里改为每次启动都校验当前哈希并打高可见度告警。
func WarnDefaultAdminPassword() {
	username := strings.TrimSpace(infra.Getenv(conf.EnvAdminUser, conf.DefaultAdminUser))
	if username == "" {
		username = conf.DefaultAdminUser
	}
	u, err := GetUserByUsername(username)
	if err != nil || u == nil {
		return
	}
	if store.VerifyPassword(conf.DefaultAdminPass, GetUserPasswordHash(u.UserID)) {
		logger.Warnf("[security][告警] 管理员账号 %s 仍在使用默认口令 admin123,存在被接管风险!"+
			"请立即登录后台在「员工管理」中修改密码;公网部署必须在上线前完成修改。", username)
	}
}
