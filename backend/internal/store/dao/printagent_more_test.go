package dao

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"dining-system/internal/store"
)

func svcAgentInit(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "printagent-more.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestSvcAgentLoadPrintAgent(t *testing.T) {
	svcAgentInit(t)

	agent, _, err := InsertPrintAgent("收银电脑", "1,2,3")
	if err != nil {
		t.Fatalf("InsertPrintAgent 失败: %v", err)
	}
	got, err := LoadPrintAgent(agent.AgentID)
	if err != nil {
		t.Fatalf("LoadPrintAgent 失败: %v", err)
	}
	if got.AgentID != agent.AgentID || got.AgentName != "收银电脑" || got.PrinterIDs != "1,2,3" {
		t.Fatalf("LoadPrintAgent 结果不匹配: got %+v", got)
	}
	if got.Status != 1 {
		t.Fatalf("新代理应启用, got status=%d", got.Status)
	}
}

func TestSvcAgentLoadPrintAgentNullLastSeen(t *testing.T) {
	svcAgentInit(t)
	// 直接插入 last_seen 为 NULL 的记录,验证扫描时 COALESCE 兜底。
	res, err := store.DB.Exec(`INSERT INTO tb_print_agent(agent_name, token_hash, token_hint, printer_ids, status, last_seen, last_report, create_time, update_time)
		VALUES('直插代理','hash','hint','',1,NULL,'',?,?)`, store.Now(), store.Now())
	if err != nil {
		t.Fatalf("直插代理失败: %v", err)
	}
	id, _ := res.LastInsertId()

	got, err := LoadPrintAgent(int(id))
	if err != nil {
		t.Fatalf("LoadPrintAgent 失败: %v", err)
	}
	if got.LastSeen != "" {
		t.Fatalf("NULL last_seen 应被 COALESCE 为空串, got %q", got.LastSeen)
	}
}

func TestSvcAgentLoadPrintAgentNotFound(t *testing.T) {
	svcAgentInit(t)
	if _, err := LoadPrintAgent(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在的代理应返回 sql.ErrNoRows, got %v", err)
	}
}

func TestSvcAgentTouchPrintAgent(t *testing.T) {
	svcAgentInit(t)

	agent, _, err := InsertPrintAgent("后厨树莓派", "")
	if err != nil {
		t.Fatalf("InsertPrintAgent 失败: %v", err)
	}

	// 心跳应 trim 后落库,并更新 last_seen / update_time。
	if err := TouchPrintAgent(agent.AgentID, "  心跳正常  "); err != nil {
		t.Fatalf("TouchPrintAgent 失败: %v", err)
	}
	got, err := LoadPrintAgent(agent.AgentID)
	if err != nil {
		t.Fatalf("回读代理失败: %v", err)
	}
	if got.LastReport != "心跳正常" {
		t.Fatalf("last_report=%q, 期望 trim 后的「心跳正常」", got.LastReport)
	}
	if got.LastSeen == "" || got.UpdateTime == "" {
		t.Fatalf("心跳后 last_seen/update_time 不应为空: %+v", got)
	}

	// 超长上报应被截断到 64 字符(含截断标记),避免超列宽。
	long := strings.Repeat("很长的上报内容", 20)
	if err := TouchPrintAgent(agent.AgentID, long); err != nil {
		t.Fatalf("超长心跳失败: %v", err)
	}
	got, _ = LoadPrintAgent(agent.AgentID)
	if n := len([]rune(got.LastReport)); n != 64 {
		t.Fatalf("超长上报应截断为 64 字符, got %d", n)
	}
	if !strings.HasSuffix(got.LastReport, "…(已截断)") {
		t.Fatalf("超长上报应带截断标记, got %q", got.LastReport)
	}
}

func TestSvcAgentTouchPrintAgentNotFound(t *testing.T) {
	svcAgentInit(t)
	// 不存在的代理:UPDATE 影响 0 行,不应报错。
	if err := TouchPrintAgent(999999, "x"); err != nil {
		t.Fatalf("不存在代理不应报错, got %v", err)
	}
}
