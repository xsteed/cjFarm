package main

// ============================================================================
// 打印机地址记忆测试
//
// 这块的存在意义只有一个:让门店不必知道打印机 IP。所以两条性质必须钉住 ——
// 「记下来的能在下一条命令里用上」,以及「没记到时给的是明确指引而不是默默失败」。
// ============================================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetPrinterMemory 清掉进程内去重缓存并指向一个全新的数据目录。
//
// printersSeen 是包级缓存(为了避免每条任务都读文件),测试之间必须复位,否则
// 用例顺序会影响结果 —— 这正是本仓库里踩过的坑(见 cups_test.go 的注释)。
func resetPrinterMemory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PRINT_AGENT_DATA_DIR", dir)

	printersMu.Lock()
	printersSeen = nil
	printersMu.Unlock()
	t.Cleanup(func() {
		printersMu.Lock()
		printersSeen = nil
		printersMu.Unlock()
	})
	return dir
}

func TestPrintersRecordAndRecall(t *testing.T) {
	dir := resetPrinterMemory(t)

	recordPrinterAddr("")
	recordPrinterAddr("   ")
	if got := knownPrinterAddrs(); len(got) != 0 {
		t.Fatalf("空地址不应被记下, got %v", got)
	}

	recordPrinterAddr("192.168.1.133:9100")
	recordPrinterAddr("192.168.1.134:9100")
	recordPrinterAddr("192.168.1.133:9100") // 重复:不该产生第二行

	got := knownPrinterAddrs()
	if len(got) != 2 || got[0] != "192.168.1.133:9100" || got[1] != "192.168.1.134:9100" {
		t.Fatalf("应去重且排序, got %v", got)
	}

	// 落盘内容也要能被人读:一行一个地址,没有多余空行/重复。
	raw, err := os.ReadFile(filepath.Join(dir, printerAddrsFileName))
	if err != nil {
		t.Fatalf("记忆文件应已生成: %v", err)
	}
	if string(raw) != "192.168.1.133:9100\n192.168.1.134:9100\n" {
		t.Fatalf("文件内容不符: %q", raw)
	}
}

func TestPrintersSurvivesRestart(t *testing.T) {
	// 模拟「代理重启后配置 CUPS」:清掉进程内缓存,但文件还在。
	dir := resetPrinterMemory(t)
	recordPrinterAddr("192.168.1.133:9100")

	printersMu.Lock()
	printersSeen = nil
	printersMu.Unlock()

	if got := knownPrinterAddrs(); len(got) != 1 || got[0] != "192.168.1.133:9100" {
		t.Fatalf("重启后应仍能读回, got %v", got)
	}
	// 再记一次同一地址:应识别为已存在,不追加第二行。
	recordPrinterAddr("192.168.1.133:9100")
	raw, _ := os.ReadFile(filepath.Join(dir, printerAddrsFileName))
	if strings.Count(string(raw), "192.168.1.133:9100") != 1 {
		t.Fatalf("重启后重复记录不应追加, got %q", raw)
	}
}

func TestPrintersResolveSetupTargets(t *testing.T) {
	resetPrinterMemory(t)

	t.Run("显式 IP 原样解析", func(t *testing.T) {
		targets, reason := resolveSetupCUPSTargets("192.168.1.133")
		if len(targets) != 1 || targets[0].addr() != "192.168.1.133:9100" || reason != "" {
			t.Fatalf("解析错误: %v %q", targets, reason)
		}
	})

	t.Run("多台逗号分隔", func(t *testing.T) {
		targets, _ := resolveSetupCUPSTargets("192.168.1.133,192.168.1.134:9101")
		if len(targets) != 2 {
			t.Fatalf("应解析出 2 台, got %v", targets)
		}
		if targets[1].addr() != "192.168.1.134:9101" {
			t.Fatalf("显式端口应保留, got %q", targets[1].addr())
		}
	})

	t.Run("auto 但没记到任何地址时给出指引", func(t *testing.T) {
		targets, reason := resolveSetupCUPSTargets("auto")
		if len(targets) != 0 {
			t.Fatalf("不应产出目标, got %v", targets)
		}
		// 指引必须同时说清「怎么让它有」和「怎么直接给」——否则门店只会卡住。
		for _, want := range []string{"测试打印", "--setup-cups 192.168.1.133"} {
			if !strings.Contains(reason, want) {
				t.Fatalf("指引应包含 %q: %s", want, reason)
			}
		}
	})

	t.Run("auto 用已记下的地址", func(t *testing.T) {
		recordPrinterAddr("192.168.1.133:9100")
		recordPrinterAddr("192.168.1.134:9100")
		targets, reason := resolveSetupCUPSTargets("AUTO") // 大小写不敏感
		if reason != "" {
			t.Fatalf("不应有错误说明: %q", reason)
		}
		if len(targets) != 2 {
			t.Fatalf("应解析出 2 台, got %v", targets)
		}
	})
}
