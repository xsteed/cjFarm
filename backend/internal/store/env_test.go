package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseDotEnvLine 覆盖 .env 常见写法。
func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		line string
		key  string
		val  string
		ok   bool
	}{
		{"DB_DRIVER=mysql", "DB_DRIVER", "mysql", true},
		{"DB_PORT = 3306", "DB_PORT", "3306", true},
		{"  DB_HOST=127.0.0.1  ", "DB_HOST", "127.0.0.1", true},
		{"export DB_NAME=dining", "DB_NAME", "dining", true},
		{`DB_PASSWORD="p@ss w#rd"`, "DB_PASSWORD", "p@ss w#rd", true},
		{`DB_PASSWORD='p@ss w#rd'`, "DB_PASSWORD", "p@ss w#rd", true},
		{"DB_NAME=dining   # 库名", "DB_NAME", "dining", true},
		// 未加引号时,密码里的 # 不应被当注释截断(只有前置空白才算注释)
		{"DB_PASSWORD=ab#cd", "DB_PASSWORD", "ab#cd", true},
		{`MSG="第一行\n第二行"`, "MSG", "第一行\n第二行", true},
		{`PATHVAL="C:\\data\\db"`, "PATHVAL", `C:\data\db`, true},
		{"", "", "", false},
		{"# 注释", "", "", false},
		{"   # 缩进注释", "", "", false},
		{"没有等号", "", "", false},
		{"=没有键", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := parseDotEnvLine(c.line)
		if ok != c.ok {
			t.Errorf("parseDotEnvLine(%q) ok=%v, 期望 %v", c.line, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if k != c.key || v != c.val {
			t.Errorf("parseDotEnvLine(%q) = (%q,%q), 期望 (%q,%q)", c.line, k, v, c.key, c.val)
		}
	}
}

// TestLoadDotEnv 验证文件加载、真实环境变量优先、文件缺失静默这三条语义。
func TestLoadDotEnv(t *testing.T) {
	// LoadDotEnv 内部用 os.Setenv 写入进程级环境变量,测试结束必须还原,
	// 否则会污染同包内后续测试(例如让 store.Init 误判为 MySQL 后端)。
	for _, k := range []string{"DB_DRIVER", "DB_HOST", "DB_PASSWORD", "QUOTED", "ENV_FILE"} {
		if old, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, old) })
		} else {
			t.Cleanup(func() { os.Unsetenv(k) })
		}
	}
	for _, k := range []string{"DB_DRIVER", "DB_PASSWORD", "QUOTED"} {
		os.Unsetenv(k)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.env")
	content := `
# 数据库
DB_DRIVER=mysql
DB_HOST=10.0.0.9
DB_PASSWORD="p#ss word"
QUOTED="含 空格"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// 预设一个真实环境变量,验证「文件不覆盖已存在的变量」。
	const preKey, preVal = "DB_HOST", "127.0.0.1"
	if err := os.Setenv(preKey, preVal); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("ENV_FILE", path); err != nil {
		t.Fatal(err)
	}

	n := LoadDotEnv()
	if n != 3 { // DB_DRIVER / DB_PASSWORD / QUOTED(DB_HOST 被真实变量拦截)
		t.Errorf("载入条目数 = %d, 期望 3", n)
	}
	if got := os.Getenv("DB_DRIVER"); got != "mysql" {
		t.Errorf("DB_DRIVER = %q, 期望 mysql", got)
	}
	if got := os.Getenv("DB_PASSWORD"); got != "p#ss word" {
		t.Errorf("DB_PASSWORD = %q, 期望 %q", got, "p#ss word")
	}
	if got := os.Getenv("QUOTED"); got != "含 空格" {
		t.Errorf("QUOTED = %q, 期望 %q", got, "含 空格")
	}
	// 真实环境变量优先:文件里的 10.0.0.9 不应生效
	if got := os.Getenv(preKey); got != preVal {
		t.Errorf("%s = %q, 期望保持 %q(真实环境变量优先)", preKey, got, preVal)
	}

	// 指向不存在的文件应静默返回 0
	if err := os.Setenv("ENV_FILE", filepath.Join(dir, "not-exist.env")); err != nil {
		t.Fatal(err)
	}
	if n := LoadDotEnv(); n != 0 {
		t.Errorf("文件不存在时返回 %d, 期望 0", n)
	}
}

// TestUnescapeDoubleQuoted 验证双引号内的转义处理。
func TestUnescapeDoubleQuoted(t *testing.T) {
	cases := map[string]string{
		`a\nb`:     "a\nb",
		`a\tb`:     "a\tb",
		`a\"b`:     `a"b`,
		`C:\\data`: `C:\data`,
		`无转义`:      `无转义`,
	}
	for in, want := range cases {
		if got := unescapeDoubleQuoted(in); got != want {
			t.Errorf("unescapeDoubleQuoted(%q) = %q, 期望 %q", in, got, want)
		}
	}
}
