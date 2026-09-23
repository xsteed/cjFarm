package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderDarwinPlist(t *testing.T) {
	tpl := deployTemplate("deploy/print-agent.plist")
	if tpl == "" {
		t.Fatal("deploy/print-agent.plist 未内嵌")
	}
	exe := "/opt/dining/print-agent"
	// 模板头部注释会举例说明 caffeinate 形态,断言统一只取
	// ProgramArguments 之后的段,避免注释里的示例干扰判断。
	const mark = "<key>ProgramArguments</key>"
	argsOf := func(got string) string {
		if i := strings.Index(got, mark); i >= 0 {
			return got[i+len(mark):]
		}
		return got
	}

	// 普通版:ProgramArguments 只留 exe,合盖后允许系统睡眠(原有行为)。
	plain := renderDarwinPlist(tpl, exe, false)
	if !strings.Contains(argsOf(plain), "<string>"+exe+"</string>") {
		t.Fatalf("普通版应包含 exe 路径:\n%s", plain)
	}
	if strings.Contains(argsOf(plain), "<string>/usr/bin/caffeinate</string>") {
		t.Fatalf("普通版 ProgramArguments 不应包含 caffeinate:\n%s", plain)
	}

	// 防睡眠版:ProgramArguments 必须是 caffeinate 包装代理的数组。
	// 注意 caffeinate 只负责顶住「空闲睡眠」,合盖睡眠(Clamshell Sleep)要另设
	// pmset 的 disablesleep —— 这里只断言包装形态,合盖是否真的不睡由
	// reportDarwinLidClosedReadiness 在安装时核对。
	noSleep := renderDarwinPlist(tpl, exe, true)
	for _, want := range []string{"<string>/usr/bin/caffeinate</string>", "<string>-s</string>", "<string>" + exe + "</string>"} {
		if !strings.Contains(argsOf(noSleep), want) {
			t.Fatalf("防睡眠版应包含 %q:\n%s", want, noSleep)
		}
	}

	// 两种形态都要把模板里的占位参数替换干净(整段 array 被替换,注释里的示例不算)。
	for name, got := range map[string]string{"普通版": plain, "防睡眠版": noSleep} {
		for _, stale := range []string{
			"<string>https://CHANGEME.example.com</string>",
			"<string>CHANGEME_TOKEN</string>",
			"--server",
			"--token",
		} {
			if strings.Contains(argsOf(got), stale) {
				t.Fatalf("%s 渲染结果不应包含 %q:\n%s", name, stale, got)
			}
		}
	}
}

func TestRenderSystemdUnit(t *testing.T) {
	tpl := deployTemplate("deploy/print-agent.service")
	if tpl == "" {
		t.Fatal("deploy/print-agent.service 未内嵌")
	}
	exe := "/home/store/print-agent"
	got := renderSystemdUnit(tpl, exe)

	if !strings.Contains(got, "ExecStart="+exe+"\n") {
		t.Fatalf("渲染后的 unit 应包含新 ExecStart:\n%s", got)
	}
	if strings.Contains(got, "ExecStart=/opt/dining-print-agent/print-agent") {
		t.Fatalf("渲染后的 unit 不应保留旧 ExecStart:\n%s", got)
	}
}

func TestRenderWindowsTask(t *testing.T) {
	tpl := deployTemplate("deploy/print-agent-windows-task.xml")
	if tpl == "" {
		t.Fatal("deploy/print-agent-windows-task.xml 未内嵌")
	}
	exe := `C:\store\print-agent.exe`
	got := renderWindowsTask(tpl, exe)

	if !strings.Contains(got, "<Command>"+escapeXML(exe)+"</Command>") {
		t.Fatalf("渲染后的 XML 应包含 exe 路径:\n%s", got)
	}
	// 注释里也提到 C:\dining\print-agent.exe 示例,故按 <Command> 整值断言。
	if strings.Contains(got, `<Command>C:\dining\print-agent.exe</Command>`) {
		t.Fatalf("渲染后的 XML 不应保留旧路径:\n%s", got)
	}
}

func TestBuildAgentEnvMerge(t *testing.T) {
	existing := "# 已有配置\nPRINT_AGENT_TOKEN=secret-token\nPRINT_AGENT_INTERVAL=3\n"
	got, updated := buildAgentEnv(existing, "https://new.example.com", "new-token", false, false)

	if !strings.Contains(got, "PRINT_AGENT_SERVER=https://new.example.com") {
		t.Fatalf("缺 SERVER 时应补齐:\n%s", got)
	}
	if !strings.Contains(got, "PRINT_AGENT_TOKEN=secret-token") {
		t.Fatalf("已有 TOKEN 应保留:\n%s", got)
	}
	if strings.Contains(got, "PRINT_AGENT_TOKEN=new-token") {
		t.Fatalf("已有 TOKEN 不应被覆盖:\n%s", got)
	}
	if len(updated) != 0 {
		t.Fatalf("未显式传入时不应有更新项, got %v", updated)
	}
}

// 回归用例:二次安装时命令行明确传了新令牌,旧值必须被覆盖 ——
// 否则表现为「安装完成却一直报令牌不正确」。
func TestBuildAgentEnvOverrideWhenExplicit(t *testing.T) {
	existing := "PRINT_AGENT_SERVER=http://old.example.com\nPRINT_AGENT_TOKEN=old-token\n"
	got, updated := buildAgentEnv(existing, "https://new.example.com", "new-token", true, true)

	for _, want := range []string{
		"PRINT_AGENT_SERVER=https://new.example.com",
		"PRINT_AGENT_TOKEN=new-token",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("显式传入应覆盖旧值, 期望含 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old-token") || strings.Contains(got, "http://old.example.com") {
		t.Fatalf("旧值应被替换:\n%s", got)
	}
	if len(updated) != 2 || updated[0] != "PRINT_AGENT_SERVER" || updated[1] != "PRINT_AGENT_TOKEN" {
		t.Fatalf("应上报两个被覆盖的键, got %v", updated)
	}
	if n := strings.Count(got, "PRINT_AGENT_TOKEN="); n != 1 {
		t.Fatalf("TOKEN 行应只有一条, got %d 条:\n%s", n, got)
	}
}

func TestBuildAgentEnvOverrideSameValueNotReported(t *testing.T) {
	existing := "PRINT_AGENT_SERVER=https://s.example.com\nPRINT_AGENT_TOKEN=tok-123\nPRINT_AGENT_LOG=print-agent.log\n"
	got, updated := buildAgentEnv(existing, "https://s.example.com", "tok-123", true, true)

	if len(updated) != 0 {
		t.Fatalf("值未变化不应计为更新, got %v", updated)
	}
	if got != existing {
		t.Fatalf("值未变化时文件内容不应改变:\n%s", got)
	}
}

func TestBuildAgentEnvNew(t *testing.T) {
	got, updated := buildAgentEnv("", "https://s.example.com", "tok-123", false, false)

	if !strings.Contains(got, "PRINT_AGENT_SERVER=https://s.example.com") {
		t.Fatalf("新文件应含 SERVER:\n%s", got)
	}
	if !strings.Contains(got, "PRINT_AGENT_TOKEN=tok-123") {
		t.Fatalf("新文件应含 TOKEN:\n%s", got)
	}
	if len(updated) != 0 {
		t.Fatalf("新文件没有「旧值被覆盖」一说, got %v", updated)
	}
}

func TestEscapeXML(t *testing.T) {
	in := `a&b<c>d"e'f`
	want := "a&amp;b&lt;c&gt;d&quot;e&apos;f"
	if got := escapeXML(in); got != want {
		t.Fatalf("escapeXML(%q)=%q, want %q", in, got, want)
	}
}

func TestCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "schtasks",
			got:  schtasksCommand(`C:\tmp\task.xml`),
			want: []string{"schtasks", "/create", "/tn", "DiningPrintAgent", "/xml", `C:\tmp\task.xml`, "/f"},
		},
		{
			name: "launchctl load",
			got:  launchctlCommand("load", "/tmp/com.cjfarm.print-agent.plist"),
			want: []string{"launchctl", "load", "/tmp/com.cjfarm.print-agent.plist"},
		},
		{
			name: "systemctl enable",
			got:  systemctlCommand("enable", "--now", "print-agent"),
			want: []string{"systemctl", "enable", "--now", "print-agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("argv=%q, want %q", tt.got, tt.want)
			}
		})
	}
}
