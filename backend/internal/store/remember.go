package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"dining-system/internal/model"
)

// ============================================================================
// 「记住我」令牌(免登录 7/30 天)
//
// 与后端访问令牌(默认 24h,见 handler.auth.go 的 tokenTTL)解耦:
// 访问令牌短期有效、靠签名自校验;记住令牌长期有效、靠本表查库裁决。
// 是否过期完全由 expire_time 决定 —— 前端只持有这个不透明串,既无法伪造也无法篡改有效期。
// ============================================================================

const rememberTokenBytes = 32 // 64 个十六进制字符,足够抗枚举

func randToken() (string, error) {
	b := make([]byte, rememberTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateRememberToken 为账号生成一条「记住我」令牌,有效期 days 天,返回令牌串。
func CreateRememberToken(userID, days int) (string, error) {
	tok, err := randToken()
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	_, err = DB.Exec(`INSERT INTO tb_remember_token (user_id, token, expire_time, create_time)
		VALUES (?,?,?,?)`, userID, tok, exp.Format("2006-01-02 15:04:05"), Now())
	if err != nil {
		return "", err
	}
	return tok, nil
}

// ConsumeRememberToken 用「记住我」令牌换取账号信息:令牌存在、未过期且账号仍可用才返回,
// 否则返回错误(过期/失效的行顺手删除,避免脏数据累积)。账号状态由 GetAuthByID 校验启用/角色。
func ConsumeRememberToken(token string) (*AuthInfo, error) {
	var userID int
	var expire string
	row := DB.QueryRow(`SELECT user_id, expire_time FROM tb_remember_token WHERE token=?`, token)
	if err := row.Scan(&userID, &expire); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("记住登录令牌无效")
		}
		return nil, err
	}
	exp, err := time.ParseInLocation("2006-01-02 15:04:05", expire, time.Local)
	if err != nil {
		// 时间格式异常视为过期,清掉脏行。
		DeleteRememberToken(token)
		return nil, fmt.Errorf("记住登录令牌已过期")
	}
	if exp.Before(time.Now()) {
		DeleteRememberToken(token)
		return nil, fmt.Errorf("记住登录令牌已过期")
	}
	auth, err := GetAuthByID(userID)
	if err != nil || auth == nil || auth.Status != model.UserStatusEnabled || auth.RoleStatus != model.UserStatusEnabled {
		// 账号已删除/停用,或所属角色被停用:令牌立即作废。
		DeleteRememberToken(token)
		return nil, fmt.Errorf("账号状态已变更,请重新登录")
	}
	DB.Exec(`UPDATE tb_remember_token SET last_used_time=? WHERE token=?`, Now(), token)
	return auth, nil
}

// DeleteRememberToken 删除单条令牌(退出登录 / 令牌失效时调用)。
func DeleteRememberToken(token string) error {
	_, err := DB.Exec(`DELETE FROM tb_remember_token WHERE token=?`, token)
	return err
}

// DeleteRememberTokensByUser 删除某账号的全部令牌(改密 / 退出登录等场景调用,
// 使该账号所有「记住我」会话立即失效)。
//
// ⚠️ 事务内请使用 deleteRememberTokensByUserTx:
// SQLite 只允许一个写者,事务未提交时再用连接池的其它连接写库会自锁
// 等到 busy_timeout(5s) 后报 SQLITE_BUSY —— 这个坑在测试里真实踩到过。
func DeleteRememberTokensByUser(userID int) error {
	_, err := DB.Exec(`DELETE FROM tb_remember_token WHERE user_id=?`, userID)
	return err
}

// deleteRememberTokensByUserTx 事务版本:与业务变更(停用/删除账号)同生共死,
// 保证「账号状态变了」与「记住会话作废」要么同时生效、要么同时回滚。
func deleteRememberTokensByUserTx(tx *sql.Tx, userID int) error {
	_, err := tx.Exec(`DELETE FROM tb_remember_token WHERE user_id=?`, userID)
	return err
}
