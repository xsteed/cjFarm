package dao

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
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

// hashRememberToken 返回 SHA-256(token) 的 hex 串,库内永不保存明文令牌。
// 与 printagent.go 的 hashPrintAgentToken 同一策略:DB 泄露时拿到的是哈希,
// 无法直接冒充账号免登录。
func hashRememberToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// rememberPrefix 返回令牌前 8 位,供「登录设备管理」页识别当前设备。
// 与 token_prefix 列对应;令牌不足 8 位(理论只可能是手工构造的存量数据)时整串返回,
// 避免切片越界。
func rememberPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

// CreateRememberToken 为账号生成一条「记住我」令牌,有效期 days 天,返回令牌串。
// ua 为签发时的环境指纹(如 "Chrome·macOS",见 handler.uaFingerprint),用于环境绑定。
//
// 库内只存 SHA-256 哈希(token 列)与令牌前 8 位(token_prefix 列,设备管理页识别用);
// 明文令牌仅随返回值给客户端展示一次,此后服务端不再持有明文。
func CreateRememberToken(userID, days int, ua string) (string, error) {
	tok, err := randToken()
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	_, err = store.DB.Exec(`INSERT INTO tb_remember_token (user_id, token, token_prefix, expire_time, create_time, ua)
		VALUES (?,?,?,?,?,?)`, userID, hashRememberToken(tok), rememberPrefix(tok), exp.Format(conf.TimeLayout), store.Now(), ua)
	if err != nil {
		return "", err
	}
	return tok, nil
}

// ConsumeRememberToken 用「记住我」令牌换取账号信息:令牌存在、未过期、环境一致且账号仍可用
// 才返回,否则返回错误(过期/失效的行顺手删除,避免脏数据累积)。账号状态由 GetAuthByID 校验
// 启用/角色。
//
// 环境绑定(uaFP 为本次请求的环境指纹):签发时记录的指纹与本次不一致,视为令牌被复制到
// 其它环境使用 —— 立即作废该令牌(防止继续被用)并拒绝。老库升级前的存量行 ua 为空,
// 跳过校验并顺手补写当前指纹,已有会话平滑升级绑定、不打扰用户。
//
// 令牌查找策略(哈希优先 + 明文回退):
//   - 新签发与已升级的行 token 列为 SHA-256 hex,先按哈希查;
//   - 未命中再按客户端传来的明文查一次,兼容老库升级前已签发的明文行,
//     命中后把该行改写为哈希存储(UPDATE),后续走哈希路径。
//
// 迁移说明:明文回退与升级逻辑只为兼容存量明文令牌。待这些令牌全部自然过期、
// 或全量轮换(改密/停用/删除账号会按 user_id 批量删除全部会话)后,即可移除
// 明文回退查询与 upgradeRememberTokenToHash,消费路径只保留按哈希查询。
func ConsumeRememberToken(token, uaFP string) (*AuthInfo, error) {
	hashed := hashRememberToken(token)

	var userID int
	var expire, ua string
	// 先按哈希查(新签发与已升级的行都走这条路径)。
	row := store.DB.QueryRow(`SELECT user_id, expire_time, ua FROM tb_remember_token WHERE token=?`, hashed)
	err := row.Scan(&userID, &expire, &ua)
	if err == sql.ErrNoRows {
		// 存量兼容:老库仍存明文令牌,按明文再查一次。
		row = store.DB.QueryRow(`SELECT user_id, expire_time, ua FROM tb_remember_token WHERE token=?`, token)
		err = row.Scan(&userID, &expire, &ua)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("记住登录令牌无效")
		}
		if err != nil {
			return nil, err
		}
		// 命中存量明文行:升级为哈希存储,后续消费/删除都走哈希路径。
		upgradeRememberTokenToHash(token, hashed)
	} else if err != nil {
		return nil, err
	}

	exp, err := time.ParseInLocation(conf.TimeLayout, expire, time.Local)
	if err != nil {
		// 时间格式异常视为过期,清掉脏行。
		DeleteRememberToken(token)
		return nil, fmt.Errorf("记住登录令牌已过期")
	}
	if exp.Before(time.Now()) {
		DeleteRememberToken(token)
		return nil, fmt.Errorf("记住登录令牌已过期")
	}
	// 环境绑定:指纹不一致 = 疑似令牌被带到其它环境,作废并拒绝。
	if ua != "" && uaFP != "" && ua != uaFP {
		DeleteRememberToken(token)
		return nil, fmt.Errorf("登录环境已变更,请重新登录")
	}
	auth, err := GetAuthByID(userID)
	if err != nil || auth == nil || auth.Status != po.UserStatusEnabled || auth.RoleStatus != po.UserStatusEnabled {
		// 账号已删除/停用,或所属角色被停用:令牌立即作废。
		DeleteRememberToken(token)
		return nil, fmt.Errorf("账号状态已变更,请重新登录")
	}
	// 存量行(ua 为空)首次使用时补写指纹,完成绑定升级。
	if ua == "" && uaFP != "" {
		ua = uaFP
	}
	// 升级后 token 列已是哈希,此处也按哈希定位,避免再走明文路径。
	store.DB.Exec(`UPDATE tb_remember_token SET last_used_time=?, ua=? WHERE token=?`, store.Now(), ua, hashed)
	return auth, nil
}

// upgradeRememberTokenToHash 把存量明文行改写为哈希存储,并补齐设备识别用的前缀。
// token 列带唯一索引;理论上 SHA-256 碰撞概率可忽略,若极端情况下 UPDATE 因冲突失败,
// 本次消费仍按已读到的行继续(不因此拒绝用户),后续自然落到哈希或明文路径之一。
func upgradeRememberTokenToHash(token, hashed string) {
	store.DB.Exec(`UPDATE tb_remember_token SET token=?, token_prefix=? WHERE token=?`,
		hashed, rememberPrefix(token), token)
}

// RememberSessionRow 「记住我」会话投影(store 内部使用,不带 json tag;
// 出参由 handler 转换为 dto.RememberSession)。
type RememberSessionRow struct {
	TokenID      int
	TokenPrefix  string // 令牌前 8 位,仅供前端识别「当前设备」;完整令牌不回传
	UA           string // 签发环境指纹,如 "Chrome·macOS"
	CreateTime   string
	LastUsedTime string
	ExpireTime   string
}

// ListRememberSessions 列出某账号全部有效(未过期)的「记住我」会话,新的在前。
func ListRememberSessions(userID int) ([]RememberSessionRow, error) {
	// last_used_time 等列可空(令牌签发后、首次免登录使用前为 NULL),
	// 直接用 string 接收会把 NULL Scan 成 error("converting NULL to string is unsupported"),
	// 因此统一 COALESCE 成空串。
	// 哈希存储后 token 列是 SHA-256 摘要,SUBSTR(token,1,8) 已不是原令牌前缀;
	// 新行用 token_prefix 列返回原令牌前 8 位,存量明文行(token_prefix 为空)仍回退
	// 到 SUBSTR(token,1,8),保证设备管理页在老库升级前后都能识别设备。
	rows, err := store.DB.Query(`SELECT token_id, COALESCE(NULLIF(token_prefix,''), SUBSTR(token,1,8)), COALESCE(ua,''), COALESCE(create_time,''), COALESCE(last_used_time,''), expire_time
		FROM tb_remember_token WHERE user_id=? AND expire_time>? ORDER BY token_id DESC`, userID, store.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RememberSessionRow{}
	for rows.Next() {
		var s RememberSessionRow
		if err := rows.Scan(&s.TokenID, &s.TokenPrefix, &s.UA, &s.CreateTime, &s.LastUsedTime, &s.ExpireTime); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RevokeRememberSession 吊销指定的一条「记住我」会话(登录设备管理页)。
// user_id 条件确保属主:传别人的 tokenId 既删不掉、也不泄露是否存在。
func RevokeRememberSession(userID, tokenID int) (bool, error) {
	res, err := store.DB.Exec(`DELETE FROM tb_remember_token WHERE token_id=? AND user_id=?`, tokenID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteRememberToken 删除单条令牌(退出登录 / 令牌失效时调用)。
// 调用方传入的是客户端持有的原令牌:先按哈希删,未命中再按明文删(存量兼容),
// 与 ConsumeRememberToken 的查找顺序保持一致。
func DeleteRememberToken(token string) error {
	hashed := hashRememberToken(token)
	res, err := store.DB.Exec(`DELETE FROM tb_remember_token WHERE token=?`, hashed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	// 存量明文回退:老库升级前签发的明文行仍按明文删除。
	_, err = store.DB.Exec(`DELETE FROM tb_remember_token WHERE token=?`, token)
	return err
}

// DeleteRememberTokensByUser 删除某账号的全部令牌(改密 / 退出登录等场景调用,
// 使该账号所有「记住我」会话立即失效)。
//
// ⚠️ 事务内请使用 deleteRememberTokensByUserTx:
// SQLite 只允许一个写者,事务未提交时再用连接池的其它连接写库会自锁
// 等到 busy_timeout(5s) 后报 SQLITE_BUSY —— 这个坑在测试里真实踩到过。
func DeleteRememberTokensByUser(userID int) error {
	_, err := store.DB.Exec(`DELETE FROM tb_remember_token WHERE user_id=?`, userID)
	return err
}

// deleteRememberTokensByUserTx 事务版本:与业务变更(停用/删除账号)同生共死,
// 保证「账号状态变了」与「记住会话作废」要么同时生效、要么同时回滚。
func deleteRememberTokensByUserTx(tx *sql.Tx, userID int) error {
	_, err := tx.Exec(`DELETE FROM tb_remember_token WHERE user_id=?`, userID)
	return err
}
