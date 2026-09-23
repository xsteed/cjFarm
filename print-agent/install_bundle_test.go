package main

// ============================================================================
// macOS .app bundle 部署相关测试
//
// 这一块没有在 CI 里真跑 launchctl / codesign:被测的都是纯函数(路径推导、模板
// 渲染、文件搬运),正是「改错了也不会当场报错」的那部分,所以必须逐条钉住。
// ============================================================================

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// assertWellFormedXML 断言内容是合法 XML。
//
// 渲染 plist 靠的是字符串拼接(与仓库既有的 renderDarwinPlist 一致),拼错一个标签
// 就会让 launchd 直接拒绝加载自启动,而那个失败发生在门店机器上、现场极难定位 ——
// 所以放在这里挡一道。
func assertWellFormedXML(t *testing.T, what, content string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(content))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s 不是合法 XML: %v", what, err)
		}
	}
}

// ============================================================================
// launchd plist:AssociatedBundleIdentifiers
// ============================================================================

func TestBundleWithAssociatedBundleIdentifiers(t *testing.T) {
	tpl := deployTemplate("deploy/print-agent.plist")
	if tpl == "" {
		t.Fatal("内嵌的 macOS LaunchAgent 模板缺失")
	}

	got := withAssociatedBundleIdentifiers(tpl, darwinBundleID)
	if !strings.Contains(got, "<key>AssociatedBundleIdentifiers</key>") {
		t.Fatal("应写入 AssociatedBundleIdentifiers")
	}
	if !strings.Contains(got, "<string>"+darwinBundleID+"</string>") {
		t.Fatalf("应写入 bundle id %s", darwinBundleID)
	}
	// 这个键必须落在 dict 里,而不是掉进注释段 —— 注释里的内容 plist 解析器不认。
	commentEnd := strings.Index(got, "-->")
	at := strings.Index(got, "<key>AssociatedBundleIdentifiers</key>")
	if commentEnd < 0 || at < commentEnd {
		t.Fatal("AssociatedBundleIdentifiers 应写在注释之后(真正生效的 dict 里)")
	}

	// 幂等:重复调用不得产生第二份(plist 里同名键重复时行为未定义)。
	twice := withAssociatedBundleIdentifiers(got, darwinBundleID)
	if strings.Count(twice, "<key>AssociatedBundleIdentifiers</key>") != 1 {
		t.Fatalf("重复注入应保持一份, got %d 份",
			strings.Count(twice, "<key>AssociatedBundleIdentifiers</key>"))
	}
}

func TestBundleDarwinPlistWrapsWithCaffeinate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "com.cjfarm.print-agent.plist")
	tpl := deployTemplate("deploy/print-agent.plist")

	// 普通版:模板头部注释里也出现过 caffeinate 字样,若按全文匹配就会误判成「已启用」,
	// 于是非交互重装时会保留一个错误结论。
	_ = os.WriteFile(p, []byte(renderDarwinPlist(tpl, "/tmp/print-agent", false)), 0o644)
	if darwinPlistWrapsWithCaffeinate(p) {
		t.Fatal("普通版不应被判为已启用防睡眠(注释里的 caffeinate 字样不算)")
	}

	_ = os.WriteFile(p, []byte(renderDarwinPlist(tpl, "/tmp/print-agent", true)), 0o644)
	if !darwinPlistWrapsWithCaffeinate(p) {
		t.Fatal("防睡眠版应被判为已启用")
	}

	// 首次安装没有 plist:按未启用处理。
	if darwinPlistWrapsWithCaffeinate(filepath.Join(dir, "nope.plist")) {
		t.Fatal("plist 不存在时应返回 false")
	}
}

func TestBundleResolveDarwinNoSleep(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "com.cjfarm.print-agent.plist")
	tpl := deployTemplate("deploy/print-agent.plist")
	writePlist := func(noSleep bool) {
		_ = os.WriteFile(p, []byte(renderDarwinPlist(tpl, "/tmp/x/print-agent", noSleep)), 0o644)
	}
	askYes := func() (bool, bool) { return true, true }
	askNo := func() (bool, bool) { return false, true }
	askSilent := func() (bool, bool) { return false, false } // 非交互:问不出来

	t.Run("显式指定优先且不再询问", func(t *testing.T) {
		writePlist(false)
		if v, note := resolveDarwinNoSleep(config{noSleep: true, noSleepExplicit: true}, p, askNo); !v || note != "" {
			t.Fatalf("显式 true 应生效且无需提示, got %v %q", v, note)
		}
		writePlist(true)
		if v, _ := resolveDarwinNoSleep(config{noSleep: false, noSleepExplicit: true}, p, askYes); v {
			t.Fatal("显式 false 应生效(不因现存配置而被覆盖)")
		}
	})

	t.Run("问得出答案时以答案为准", func(t *testing.T) {
		writePlist(false)
		if v, note := resolveDarwinNoSleep(config{}, p, askYes); !v || note != "" {
			t.Fatalf("答 y 应启用, got %v %q", v, note)
		}
		writePlist(true)
		if v, _ := resolveDarwinNoSleep(config{}, p, askNo); v {
			t.Fatal("答 N 应关闭")
		}
	})

	t.Run("问不出来时沿用现状", func(t *testing.T) {
		// 核心断言:非交互重装不得把门店先前启用的防睡眠关掉。
		writePlist(true)
		v, note := resolveDarwinNoSleep(config{}, p, askSilent)
		if !v {
			t.Fatal("现有配置是「启用」时,非交互重装必须保持启用")
		}
		if note == "" {
			t.Fatal("沿用了现有配置时应给出提示,否则门店不知道发生了什么")
		}
		// 提示里不得混入「自检页」那套话术(onOff 会带出「开(会真的出纸)」),
		// 那是另一个开关的措辞,拼进防睡眠提示里纯属胡话。
		if strings.Contains(note, "出纸") {
			t.Fatalf("提示措辞串了场景: %q", note)
		}
		// 反向:现有是关闭,就不要自作主张打开。
		writePlist(false)
		if v, _ := resolveDarwinNoSleep(config{}, p, askSilent); v {
			t.Fatal("现有配置是「关闭」时,非交互重装不应擅自打开")
		}
		// 首次安装(没有 plist):按关闭处理,并给出提示。
		if v, note := resolveDarwinNoSleep(config{}, filepath.Join(dir, "nope.plist"), askSilent); v || note == "" {
			t.Fatalf("无 plist 时应为关闭且给出提示, got %v %q", v, note)
		}
	})
}

func TestBundlePlistLabelMatchesAgentID(t *testing.T) {
	// --doctor 用 darwinBundleID 去 launchctl 里找作业,而作业名来自模板里的 Label。
	// 两者一旦不一致,体检会永远报「未运行」。
	tpl := deployTemplate("deploy/print-agent.plist")
	if !strings.Contains(tpl, "<key>Label</key>") ||
		!strings.Contains(tpl, "<string>"+darwinBundleID+"</string>") {
		t.Fatalf("模板 Label 必须等于 %s", darwinBundleID)
	}
}

func TestBundlePlistRenderCombination(t *testing.T) {
	// 两个渲染步骤串联后,ProgramArguments 仍应干净(不带 --server/--token 残留),
	// 同时带上 bundle 关联 —— 这是 installDarwin 实际写入的最终形态。
	tpl := deployTemplate("deploy/print-agent.plist")
	exe := "/Users/x/Applications/PrintAgent.app/Contents/MacOS/print-agent"
	got := withAssociatedBundleIdentifiers(renderDarwinPlist(tpl, exe, true), darwinBundleID)

	if !strings.Contains(got, "<string>"+exe+"</string>") {
		t.Fatal("ProgramArguments 应指向 bundle 内的可执行文件")
	}
	if !strings.Contains(got, "<string>/usr/bin/caffeinate</string>") {
		t.Fatal("防睡眠模式下应保留 caffeinate 包装")
	}
	// -i 与 -s 两个都要有:man caffeinate 写明 -s 的断言「仅在交流电源下有效」,
	// 只给 -s 时电池供电仍会空闲睡眠;-i 不受电源限制。
	for _, flag := range []string{"<string>-i</string>", "<string>-s</string>"} {
		if !strings.Contains(got, flag) {
			t.Fatalf("防睡眠模式应带 %s", flag)
		}
	}
	// 关掉防睡眠时必须回到「仅 exe」的单元素形态,不能残留 caffeinate。
	// 只看 ProgramArguments 之后的段:模板头部注释里就有 caffeinate 字样,
	// 按整段字符串判断会永远为真(install_test.go 里也踩过同一个坑)。
	plain := withAssociatedBundleIdentifiers(renderDarwinPlist(tpl, exe, false), darwinBundleID)
	if plainArgs := programArgumentsOfText(plain); strings.Contains(plainArgs, "caffeinate") {
		t.Fatalf("未启用防睡眠时 ProgramArguments 不应出现 caffeinate:\n%s", plainArgs)
	}
	argsAt := strings.Index(got, "<key>ProgramArguments</key>")
	args := got[argsAt:]
	for _, bad := range []string{"--server", "--token", "CHANGEME_TOKEN", "https://localhost:8080"} {
		if strings.Contains(args, bad) {
			t.Fatalf("ProgramArguments 不应残留 %s", bad)
		}
	}
}

func TestBundleRenderedPlistIsWellFormed(t *testing.T) {
	tpl := deployTemplate("deploy/print-agent.plist")
	exe := "/Users/x/Applications/PrintAgent.app/Contents/MacOS/print-agent"

	// 两种形态都要能过 XML 解析:注入 AssociatedBundleIdentifiers 的位置一旦拼错,
	// launchd 加载时会直接失败,而那是在门店机器上才暴露的问题。
	for _, noSleep := range []bool{false, true} {
		got := withAssociatedBundleIdentifiers(renderDarwinPlist(tpl, exe, noSleep), darwinBundleID)
		assertWellFormedXML(t, "渲染后的 LaunchAgent plist", got)
	}
}

func TestBundleIconAsset(t *testing.T) {
	icon, err := deployFS.ReadFile(darwinBundleIconSrc)
	if err != nil {
		t.Fatalf("内嵌图标缺失(%s),App 会变成白板图标: %v", darwinBundleIconSrc, err)
	}
	// icns 文件头是 4 字节魔数 "icns" + 4 字节长度。图标坏掉只会在门店机器上、
	// 以「图标变白板」的形式暴露,所以在这里钉住。
	if len(icon) < 8 || string(icon[:4]) != "icns" {
		t.Fatalf("内嵌图标不是合法 icns(前 8 字节: %q)", icon[:min(8, len(icon))])
	}
	// Info.plist 里的 CFBundleIconFile 必须与写进 Resources 的文件名对应,
	// 否则系统找不到图标 —— 同样是「只在真机上看得见」的问题。
	want := strings.TrimSuffix(darwinBundleIconName, ".icns")
	if !strings.Contains(deployTemplate("deploy/PrintAgent-Info.plist"), "<string>"+want+"</string>") {
		t.Fatalf("Info.plist 的 CFBundleIconFile 应为 %q(与 %s 对应)", want, darwinBundleIconName)
	}
}

func TestBundleRenderedInfoPlistIsWellFormed(t *testing.T) {
	got := renderDarwinInfoPlist(deployTemplate("deploy/PrintAgent-Info.plist"))
	assertWellFormedXML(t, "渲染后的 App Info.plist", got)
}

// ============================================================================
// Info.plist 渲染
// ============================================================================

func TestBundleRenderInfoPlist(t *testing.T) {
	tpl := deployTemplate("deploy/PrintAgent-Info.plist")
	if tpl == "" {
		t.Fatal("内嵌的 Info.plist 模板缺失")
	}
	got := renderDarwinInfoPlist(tpl)
	if strings.Contains(got, "__VERSION__") {
		t.Fatal("版本占位符应被替换")
	}
	if !strings.Contains(got, "<key>NSLocalNetworkUsageDescription</key>") {
		// 这一项是 macOS 弹授权窗时给用户看的说明,缺失会削弱授权的可理解性。
		t.Fatal("应带 NSLocalNetworkUsageDescription")
	}
	if !strings.Contains(got, "<key>CFBundleIdentifier</key>") ||
		!strings.Contains(got, "<string>"+darwinBundleID+"</string>") {
		t.Fatal("CFBundleIdentifier 必须与 launchd plist 的关联标识一致")
	}
	if !strings.Contains(got, "<string>"+darwinBundleExecName+"</string>") {
		t.Fatal("CFBundleExecutable 必须与 bundle 内文件名一致")
	}
}

func TestBundleInfoPlistVersionSanitize(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	version = "1.2.3"
	if got := renderDarwinInfoPlist("__VERSION__"); got != "1.2.3" {
		t.Fatalf("正式版本号应原样写入, got %q", got)
	}
	// 开发态构建的 version 是 "dev":CFBundleShortVersionString 只吃数字与点,
	// 直接写进去系统认不出这个 App。
	version = "dev"
	if got := renderDarwinInfoPlist("__VERSION__"); got != "0.0.0" {
		t.Fatalf("非数字版本号应替换为兜底值, got %q", got)
	}
}

func TestBundleIsNumericVersion(t *testing.T) {
	for _, v := range []string{"1", "1.0", "1.2.3", "0.0.0"} {
		if !isNumericVersion(v) {
			t.Fatalf("%q 应判定为合法版本号", v)
		}
	}
	for _, v := range []string{"", "dev", "v1.0.0", "1.0.0-beta", "1.0.0 "} {
		if isNumericVersion(v) {
			t.Fatalf("%q 不应判定为合法版本号", v)
		}
	}
}

// ============================================================================
// 路径推导
// ============================================================================

func TestBundleBundleDataDir(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("仅在 macOS 上验证 Application Support 落点")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("无法确定用户目录: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", darwinDataDirName)

	got, ok := bundleDataDir("/Users/x/Applications/PrintAgent.app/Contents/MacOS/print-agent")
	if !ok || got != want {
		t.Fatalf("bundle 内可执行文件应映射到 %s, got %q ok=%v", want, got, ok)
	}

	// 判定必须收得很紧:普通目录(哪怕正好叫 Contents/MacOS)不能被当成 bundle,
	// 否则会把门店的 agent.env 挪到别处去,表现为「装完找不到配置」。
	for _, p := range []string{
		"/Users/x/print-agent",
		"/Users/x/Contents/print-agent",
		"/Users/x/Contents/MacOS/print-agent",
		"/Users/x/Foo.app/print-agent",
		"/Users/x/Foo.app/Contents/print-agent",
	} {
		if _, ok := bundleDataDir(p); ok {
			t.Fatalf("%s 不应被判定为 bundle", p)
		}
	}
}

func TestBundleDataDirForPlainBinaryStaysBesideExe(t *testing.T) {
	// 非 bundle 部署必须与改造前完全一致(Windows/Linux/直接解压运行的 macOS):
	// 数据就落在程序同目录,「一个文件夹打包部署」的形态不能变。
	orig := os.Getenv("PRINT_AGENT_DATA_DIR")
	t.Cleanup(func() { _ = os.Setenv("PRINT_AGENT_DATA_DIR", orig) })
	_ = os.Unsetenv("PRINT_AGENT_DATA_DIR")

	exe := filepath.Join(t.TempDir(), "print-agent")
	if got := dataDirFor(exe); got != filepath.Dir(exe) {
		t.Fatalf("普通部署应落在程序同目录, got %q", got)
	}
}

func TestBundleDataDirEnvOverride(t *testing.T) {
	orig := os.Getenv("PRINT_AGENT_DATA_DIR")
	t.Cleanup(func() { _ = os.Setenv("PRINT_AGENT_DATA_DIR", orig) })

	dir := t.TempDir()
	_ = os.Setenv("PRINT_AGENT_DATA_DIR", dir)
	// 显式指定优先于一切推导,包括 bundle 形态。
	if got := dataDirFor("/Users/x/Applications/PrintAgent.app/Contents/MacOS/print-agent"); got != dir {
		t.Fatalf("显式配置应优先, got %q", got)
	}
}

func TestBundleExePath(t *testing.T) {
	got := darwinBundleExePath("/Users/x/Applications/PrintAgent.app")
	want := filepath.Join("/Users/x/Applications/PrintAgent.app", "Contents", "MacOS", "print-agent")
	if got != want {
		t.Fatalf("bundle 内可执行文件路径错误: got %q want %q", got, want)
	}
	// bundle 名与 CFBundleExecutable 必须和 Info.plist 常量对得上。
	if !strings.HasSuffix(got, filepath.Join(darwinBundleName, "Contents", "MacOS", darwinBundleExecName)) {
		t.Fatalf("bundle 结构常量不一致: %q", got)
	}
}

// ============================================================================
// 合盖继续运行:电源状态读取
// ============================================================================

func TestBundleReadDarwinSleepState(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("仅在 macOS 上验证 ioreg/pmset 的调用与输出解析")
	}
	st := readDarwinSleepState()
	// known=false 意味着 ioreg 没跑通或输出格式变了 —— 那样安装时的「缺 ② 步」告警
	// 会永远不出现,而门店会以为配置完整。这正是必须钉住它的原因。
	if !st.known {
		t.Fatalf("应能从 ioreg 读到 SleepDisabled 状态;请核对 argv 与解析逻辑")
	}
	if got := ioregSleepDisabledHint; !strings.Contains(got, "SleepDisabled") {
		t.Fatalf("核对命令提示应包含 SleepDisabled, got %q", got)
	}
}

func TestBundleSleepSwitchCommands(t *testing.T) {
	// 这两条命令会被打印给门店照着执行、也被写进文档,拼错等于让门店白跑一趟。
	if got := darwinDisableSleepCommand(); got != "sudo pmset -a disablesleep 1" {
		t.Fatalf("关闭合盖睡眠的命令不符: %q", got)
	}
	if got := darwinEnableSleepCommand(); got != "sudo pmset -a disablesleep 0" {
		t.Fatalf("恢复合盖睡眠的命令不符: %q", got)
	}
}

// ============================================================================
// agent.env 单项改写(--setup-cups 打开通道时用)
// ============================================================================

func TestBundleUpsertEnvKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "agent.env")

	t.Run("文件不存在时新建", func(t *testing.T) {
		if err := upsertEnvKey(p, "PRINT_AGENT_PRINT_VIA", "auto"); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "PRINT_AGENT_PRINT_VIA=auto\n" {
			t.Fatalf("新文件内容不符: %q", got)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat 失败: %v", err)
		}
		// 内含云端地址/令牌的同级信息,权限必须收紧。
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("权限应为 0600, got %v", fi.Mode().Perm())
		}
	})

	t.Run("已有生效行则就地改写", func(t *testing.T) {
		_ = os.WriteFile(p, []byte("PRINT_AGENT_SERVER=http://x\nPRINT_AGENT_PRINT_VIA=tcp\n"), 0o600)
		if err := upsertEnvKey(p, "PRINT_AGENT_PRINT_VIA", "auto"); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		got, _ := os.ReadFile(p)
		if strings.Count(string(got), "PRINT_AGENT_PRINT_VIA=") != 1 ||
			!strings.Contains(string(got), "PRINT_AGENT_PRINT_VIA=auto") ||
			strings.Contains(string(got), "PRINT_AGENT_PRINT_VIA=tcp") {
			t.Fatalf("应就地改写且不重复, got %q", got)
		}
	})

	t.Run("模板里注释掉的提示行就地激活", func(t *testing.T) {
		// --install 生成的 agent.env 里带着 `# PRINT_AGENT_PRINT_VIA=auto` 这类提示。
		// 直接再追加一行会同时留下「注释版」和「生效版」,门店看着困惑。
		_ = os.WriteFile(p, []byte("# 打印通道说明\n# PRINT_AGENT_PRINT_VIA=auto\nPRINT_AGENT_SERVER=http://x\n"), 0o600)
		if err := upsertEnvKey(p, "PRINT_AGENT_PRINT_VIA", "cups"); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		got, _ := os.ReadFile(p)
		if strings.Contains(string(got), "# PRINT_AGENT_PRINT_VIA=") {
			t.Fatalf("注释行应被激活,不该留两行: %q", got)
		}
		if !strings.Contains(string(got), "\nPRINT_AGENT_PRINT_VIA=cups\n") {
			t.Fatalf("应写入 active 行, got %q", got)
		}
		if !strings.Contains(string(got), "# 打印通道说明") {
			t.Fatalf("无关注释不应被删, got %q", got)
		}
	})

	t.Run("不误伤同名前缀的其它键", func(t *testing.T) {
		_ = os.WriteFile(p, []byte("PRINT_AGENT_PRINT_VIA_EXTRA=keep\n"), 0o600)
		if err := upsertEnvKey(p, "PRINT_AGENT_PRINT_VIA", "auto"); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		got, _ := os.ReadFile(p)
		if !strings.Contains(string(got), "PRINT_AGENT_PRINT_VIA_EXTRA=keep") {
			t.Fatalf("同前缀的键不应被改写: %q", got)
		}
	})
}

func TestBundleReadEnvValue(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "agent.env")
	// 模板里带了不少 `# KEY=示例` 的提示行。若把它们也当生效值读出来,重装时会
	// 悄悄把示例地址(或示例令牌)写进配置 —— 那是「装完就连不上云端」的经典成因。
	content := "# PRINT_AGENT_SERVER=https://示例地址\n" +
		"PRINT_AGENT_SERVER=https://real.example.com\n" +
		"PRINT_AGENT_TOKEN=  abc123  \n" +
		"PRINT_AGENT_OTHER=x\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	for _, c := range []struct{ key, want string }{
		{"PRINT_AGENT_SERVER", "https://real.example.com"},
		{"PRINT_AGENT_TOKEN", "abc123"},
		{"PRINT_AGENT_MISSING", ""},
		{"PRINT_AGENT_SERVE", ""}, // 不做前缀模糊匹配
	} {
		if got := readEnvValue(p, c.key); got != c.want {
			t.Fatalf("readEnvValue(%s)=%q, want %q", c.key, got, c.want)
		}
	}
	// 文件不存在时返回空串(首次安装的正常路径),不该报错。
	if got := readEnvValue(filepath.Join(dir, "nope"), "PRINT_AGENT_SERVER"); got != "" {
		t.Fatalf("文件不存在应返回空串, got %q", got)
	}
}

// ============================================================================
// 文件搬运
// ============================================================================

func TestBundleCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "sub", "dst")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}

	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatalf("copyFile 失败: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读目标文件失败: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("内容不一致: %q", data)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	// 可执行位必须落地,否则 launchd 拉起时报权限错误。
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("权限应为 0755, got %v", fi.Mode().Perm())
	}
	// 临时文件不得残留(否则 bundle 里多一个陌生可执行文件)。
	if _, err := os.Stat(dst + ".new"); !os.IsNotExist(err) {
		t.Fatal("临时文件应已改名,不应残留")
	}
}

func TestBundleSamePath(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "print-agent")
	if err := os.WriteFile(f, []byte("x"), 0o755); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(f, link); err != nil {
		t.Fatalf("建符号链接失败: %v", err)
	}
	if !samePath(f, f) {
		t.Fatal("同一路径应判定相等")
	}
	if !samePath(f, link) {
		t.Fatal("符号链接应解析后比较")
	}
	if samePath(f, filepath.Join(dir, "other")) {
		t.Fatal("不同路径不应判定相等")
	}
}

// ============================================================================
// 旧数据目录搬迁
// ============================================================================

func TestBundleMigrateFromOldDir(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "old")
	newDir := filepath.Join(oldDir, "..", "new")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "agent.env"), []byte("PRINT_AGENT_TOKEN=abc\n"), 0o600); err != nil {
		t.Fatalf("写旧配置失败: %v", err)
	}
	// 去重状态必须一起搬:换目录却丢掉它,云端重发同一条任务就会重复出纸。
	if err := os.WriteFile(filepath.Join(oldDir, "print-agent.state"), []byte(`{"printed":{"d1":1}}`), 0o600); err != nil {
		t.Fatalf("写旧状态失败: %v", err)
	}

	migrateFromOldDir(oldDir, newDir)

	for name, want := range map[string]string{
		"agent.env":         "PRINT_AGENT_TOKEN=abc",
		"print-agent.state": `{"printed":{"d1":1}}`,
	} {
		got, err := os.ReadFile(filepath.Join(newDir, name))
		if err != nil {
			t.Fatalf("%s 应被搬过来: %v", name, err)
		}
		if !strings.Contains(string(got), want) {
			t.Fatalf("%s 内容不一致: %q", name, got)
		}
	}

	// 新版与旧版复制到同一目录后重装(macOS 非 bundle 形态):源与目标同目录,应原地不动。
	before, err := os.ReadFile(filepath.Join(newDir, "agent.env"))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	migrateFromOldDir(newDir, newDir)
	after, _ := os.ReadFile(filepath.Join(newDir, "agent.env"))
	if string(before) != string(after) {
		t.Fatal("同目录搬迁不应改动内容")
	}
}

func TestBundleMigrateFromOldDirDoesNotOverwrite(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "agent.env"), []byte("PRINT_AGENT_TOKEN=old\n"), 0o600); err != nil {
		t.Fatalf("写旧配置失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "agent.env"), []byte("PRINT_AGENT_TOKEN=current\n"), 0o600); err != nil {
		t.Fatalf("写新配置失败: %v", err)
	}

	migrateFromOldDir(oldDir, newDir)

	got, _ := os.ReadFile(filepath.Join(newDir, "agent.env"))
	if !strings.Contains(string(got), "current") {
		t.Fatalf("新位置已有配置时不应被覆盖: %q", got)
	}
}

func TestBundleMigrateFromOldDirMissingSource(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	migrateFromOldDir(oldDir, newDir)
	for _, name := range []string{"agent.env", "print-agent.state"} {
		if _, err := os.Stat(filepath.Join(newDir, name)); !os.IsNotExist(err) {
			t.Fatalf("源不存在时不应创建 %s", name)
		}
	}
	// 空目录参数不得 panic,也不应把文件写到当前工作目录。
	migrateFromOldDir("", newDir)
	migrateFromOldDir(oldDir, "")
}

func TestBundleMigrateFileMode(t *testing.T) {
	oldDir := t.TempDir()
	src := filepath.Join(oldDir, "src")
	dst := filepath.Join(t.TempDir(), "sub", "dst")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	migrateFile(src, dst, "测试文件")
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("应已搬过来: %v", err)
	}
	// 两个被搬的文件都可能含敏感内容(令牌 / 已打单据号),权限必须收紧到 0600。
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("权限应为 0600, got %v", fi.Mode().Perm())
	}
}
