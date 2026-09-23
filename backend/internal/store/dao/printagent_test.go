package dao

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"dining-system/internal/store"
)

func TestPrintAgentTokenLifecycle(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "printagent.db"))
	defer store.DB.Close()

	agent, token, err := InsertPrintAgent("  收银电脑  ", "1, 2, x,3")
	if err != nil {
		t.Fatalf("新增打印代理失败: %v", err)
	}
	if len(token) != 32 {
		t.Fatalf("令牌应为 32 字符 hex, got %q", token)
	}
	if agent.TokenHint != token[:4] {
		t.Fatalf("token_hint 应为令牌前 4 位: got %q want %q", agent.TokenHint, token[:4])
	}
	if agent.AgentName != "收银电脑" {
		t.Fatalf("代理名称应 trim 后入库, got %q", agent.AgentName)
	}

	sum := sha256.Sum256([]byte(token))
	wantHash := hex.EncodeToString(sum[:])
	var gotHash string
	if err := store.DB.QueryRow(`SELECT token_hash FROM tb_print_agent WHERE agent_id=?`, agent.AgentID).Scan(&gotHash); err != nil {
		t.Fatalf("读取 token_hash 失败: %v", err)
	}
	if gotHash != wantHash {
		t.Fatalf("token_hash 不正确: got %q want %q", gotHash, wantHash)
	}

	if _, ok := FindPrintAgentByToken(token); !ok {
		t.Fatal("正确令牌应命中启用代理")
	}
	if _, ok := FindPrintAgentByToken("wrong-token"); ok {
		t.Fatal("错误令牌不应命中")
	}
	if err := SetPrintAgentStatus(agent.AgentID, 0); err != nil {
		t.Fatalf("吊销代理失败: %v", err)
	}
	if _, ok := FindPrintAgentByToken(token); ok {
		t.Fatal("吊销后的代理令牌不应命中")
	}
}

func TestListPrintAgentsKeepsTokenHash(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "printagent-list.db"))
	defer store.DB.Close()

	if _, _, err := InsertPrintAgent("后厨树莓派", ""); err != nil {
		t.Fatalf("新增打印代理失败: %v", err)
	}
	list := ListPrintAgents()
	if len(list) != 1 {
		t.Fatalf("代理列表数量错误: %d", len(list))
	}
	if list[0].TokenHash == "" {
		t.Fatal("store 层应保留 TokenHash 供内部安全复核")
	}
}
