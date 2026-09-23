package dao

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// ==================== 本地打印代理身份 ====================
//
// 本文件承载 print-agent v2 的 per-agent 令牌身份数据基座。全局 agent_token 仍由
// 配置表承担 legacy 兼容通道;tb_print_agent 用于逐台代理签发、吊销令牌,并通过
// printer_ids 把单台代理可见的打印机范围收窄。

// PrintAgentCols 代理身份表完整列(与 scanPrintAgent 顺序一一对应)。
const PrintAgentCols = `agent_id, agent_name, token_hash, token_hint, printer_ids,
	status, COALESCE(last_seen,''), last_report, create_time, update_time`

// scanPrintAgent 扫描一行代理身份记录。
func scanPrintAgent(rows interface{ Scan(...interface{}) error }) (po.PrintAgent, error) {
	var a po.PrintAgent
	err := rows.Scan(&a.AgentID, &a.AgentName, &a.TokenHash, &a.TokenHint, &a.PrinterIDs,
		&a.Status, &a.LastSeen, &a.LastReport, &a.CreateTime, &a.UpdateTime)
	if err != nil {
		return a, err
	}
	return a, nil
}

// InsertPrintAgent 新增一台本地打印代理身份,返回身份记录与仅展示一次的明文令牌。
func InsertPrintAgent(name, printerIDs string) (po.PrintAgent, string, error) {
	name = strings.TrimSpace(name)
	printerIDs = strings.TrimSpace(printerIDs)
	token := newPrintAgentToken()
	tokenHash := hashPrintAgentToken(token)
	now := store.Now()

	res, err := store.DB.Exec(`INSERT INTO tb_print_agent(agent_name, token_hash, token_hint, printer_ids,
		status, last_seen, last_report, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		name, tokenHash, token[:4], printerIDs, 1, "", "", now, now)
	if err != nil {
		return po.PrintAgent{}, "", err
	}
	id, _ := res.LastInsertId()
	agent, err := loadPrintAgentByID(int(id))
	if err != nil {
		return po.PrintAgent{}, "", err
	}
	return agent, token, nil
}

// NewAgentToken 生成一段新的代理令牌明文。
//
// legacy 全局令牌签发与 per-agent 身份签发共用同一实现:两者都是「持有即授权的
// 凭据」,熵、字符集与不可预测性要求完全一致,分开实现只会埋下强弱不一的隐患。
func NewAgentToken() string { return newPrintAgentToken() }

// newPrintAgentToken 生成 128 位随机数的 hex 串(32 字符)作为代理令牌。
// 用 crypto/rand 而非时间戳/自增:令牌是持有即授权的凭据,必须不可预测。
func newPrintAgentToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败几乎不可能;兜底用纳秒时间戳避免生成空令牌。
		return hex.EncodeToString([]byte(store.Now()))
	}
	return hex.EncodeToString(b)
}

// hashPrintAgentToken 返回 SHA-256(token) 的 hex 串,库内永不保存明文令牌。
func hashPrintAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func loadPrintAgentByID(agentID int) (po.PrintAgent, error) {
	return scanPrintAgent(store.DB.QueryRow(`SELECT `+PrintAgentCols+` FROM tb_print_agent WHERE agent_id=?`, agentID))
}

// LoadPrintAgent 按 ID 读取一台代理身份,供 handler 做授权域过滤。
func LoadPrintAgent(agentID int) (po.PrintAgent, error) {
	return loadPrintAgentByID(agentID)
}

// ListPrintAgents 按创建顺序返回全部代理身份。
func ListPrintAgents() []po.PrintAgent {
	list := []po.PrintAgent{}
	rows, err := store.DB.Query(`SELECT ` + PrintAgentCols + ` FROM tb_print_agent ORDER BY agent_id`)
	if err != nil {
		return list
	}
	defer rows.Close()
	for rows.Next() {
		if a, err := scanPrintAgent(rows); err == nil {
			list = append(list, a)
		}
	}
	return list
}

// SetPrintAgentStatus 启用或吊销一台代理身份。
func SetPrintAgentStatus(agentID, status int) error {
	res, err := store.DB.Exec(`UPDATE tb_print_agent SET status=?, update_time=? WHERE agent_id=?`, status, store.Now(), agentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("代理不存在")
	}
	return nil
}

// UpdatePrintAgent 修改一台代理身份的名称与授权范围(不重新签发令牌)。
//
// 名称留空表示不改名;授权范围留空表示「授权全部打印机」,与新增时同一语义。
// 令牌哈希与状态不在本函数的修改范围内——改授权范围不该让门店正在用的令牌失效,
// 停用请走 SetPrintAgentStatus 吊销。
func UpdatePrintAgent(agentID int, name, printerIDs string) error {
	name = strings.TrimSpace(name)
	printerIDs = strings.TrimSpace(printerIDs)
	if name == "" {
		old, err := loadPrintAgentByID(agentID)
		if err != nil {
			return errors.New("代理不存在")
		}
		name = old.AgentName
	}
	res, err := store.DB.Exec(`UPDATE tb_print_agent SET agent_name=?, printer_ids=?, update_time=? WHERE agent_id=?`,
		name, printerIDs, store.Now(), agentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("代理不存在")
	}
	return nil
}

// DeletePrintAgent 删除一台已吊销(status=0)的代理身份,连同其令牌哈希一起清掉。
//
// 启用中的代理不允许直接删:它仍持有令牌并可能在轮询,删掉会让一台正在出纸的
// 代理凭空消失且无从追责;正确顺序是先吊销、确认无人使用后再清理。
func DeletePrintAgent(agentID int) error {
	res, err := store.DB.Exec(`DELETE FROM tb_print_agent WHERE agent_id=? AND status=0`, agentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 区分「根本不存在」与「还在启用」,后者要给出可执行的下一步。
		if _, e := loadPrintAgentByID(agentID); e != nil {
			return errors.New("代理不存在")
		}
		return errors.New("请先吊销该代理，再删除")
	}
	return nil
}

// FindPrintAgentByToken 按明文令牌查找启用中的代理身份。
func FindPrintAgentByToken(tok string) (po.PrintAgent, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return po.PrintAgent{}, false
	}
	computed := hashPrintAgentToken(tok)
	a, err := scanPrintAgent(store.DB.QueryRow(`SELECT `+PrintAgentCols+` FROM tb_print_agent
		WHERE token_hash=? AND status=1 LIMIT 1`, computed))
	if err != nil {
		if err == sql.ErrNoRows {
			return po.PrintAgent{}, false
		}
		return po.PrintAgent{}, false
	}
	if subtle.ConstantTimeCompare([]byte(a.TokenHash), []byte(computed)) != 1 {
		return po.PrintAgent{}, false
	}
	return a, true
}

// TouchPrintAgent 记录代理最近一次心跳/取单/回执上报信息。
func TouchPrintAgent(agentID int, report string) error {
	report = truncateRunes(strings.TrimSpace(report), 64)
	now := store.Now()
	_, err := store.DB.Exec(`UPDATE tb_print_agent SET last_seen=?, last_report=?, update_time=? WHERE agent_id=?`,
		now, report, now, agentID)
	return err
}
