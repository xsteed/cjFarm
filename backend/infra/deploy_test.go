package infra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configEnvKeys 是部署配置层可能写入的全部环境变量。
//
// 测试必须整组快照/还原:LoadDeployConfig 会直接 os.Setenv,
// 一旦泄漏(典型是 DB_DRIVER=mysql 被同包后续测试读到)会让它们去连 3306 而失败。
var configEnvKeys = []string{
	"PORT", "UPLOAD_DIR", "STATIC_DIR", "CORS_ORIGINS", "TRUSTED_PROXIES",
	"SNOWFLAKE_NODE_ID",
	"DB_DRIVER", "DB_PATH", "DB_DSN", "DB_HOST", "DB_PORT",
	"DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PARAMS",
	"CONFIG_MASTER_KEY", "MASTER_KEY_PATH", "MASTER_KEY_OLD", "TOKEN_TTL_HOURS", "HARDEN_FILE_ACL",
	"ADMIN_USER", "ADMIN_PASS",
	"ENV_FILE", "CONFIG_FILE",
}

// isolateConfigEnv 清空配置相关环境变量,并在用例结束后还原原值。
func isolateConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range configEnvKeys {
		old, existed := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
		key := k
		if existed {
			t.Cleanup(func() { os.Setenv(key, old) })
		} else {
			t.Cleanup(func() { os.Unsetenv(key) })
		}
	}
}

// writeTemp 把内容写到临时文件,返回绝对路径。
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDeployFileEnvPairs 验证完整 config.yaml 到环境变量键名的映射。
func TestDeployFileEnvPairs(t *testing.T) {
	isolateConfigEnv(t)

	yamlDoc := `
server:
  port: 9090
  upload_dir: ./up
  static_dir: /srv/www
  cors_origins:
    - https://a.example.com
    - https://b.example.com
  trusted_proxies:
    - 127.0.0.1
  snowflake_node_id: 7
database:
  driver: mysql
  sqlite:
    path: ./data/x.db
  mysql:
    host: 10.0.0.9
    port: 3307
    user: dining
    password: p@ss
    name: dining_prod
    params: charset=utf8mb4
security:
  token_ttl_hours: 48
  harden_file_acl: true
  master_key_path: ./data/k.key
  master_key_old: base64-encoded-old-key
admin:
  user: boss
  pass: s3cret
`
	path := writeTemp(t, "config.yaml", yamlDoc)
	if err := os.Setenv("CONFIG_FILE", path); err != nil {
		t.Fatal(err)
	}

	pairs, err := LoadDeployFile()
	if err != nil {
		t.Fatalf("LoadDeployFile: %v", err)
	}

	want := map[string]string{
		"PORT":              "9090",
		"UPLOAD_DIR":        "./up",
		"STATIC_DIR":        "/srv/www",
		"CORS_ORIGINS":      "https://a.example.com,https://b.example.com",
		"TRUSTED_PROXIES":   "127.0.0.1",
		"SNOWFLAKE_NODE_ID": "7",
		"DB_DRIVER":         "mysql",
		"DB_PATH":           "./data/x.db",
		"DB_HOST":           "10.0.0.9",
		"DB_PORT":           "3307",
		"DB_USER":           "dining",
		"DB_PASSWORD":       "p@ss",
		"DB_NAME":           "dining_prod",
		"DB_PARAMS":         "charset=utf8mb4",
		"TOKEN_TTL_HOURS":   "48",
		"HARDEN_FILE_ACL":   "1",
		"MASTER_KEY_PATH":   "./data/k.key",
		"MASTER_KEY_OLD":    "base64-encoded-old-key",
		"ADMIN_USER":        "boss",
		"ADMIN_PASS":        "s3cret",
	}
	if len(pairs) != len(want) {
		t.Errorf("映射条目数 = %d, 期望 %d, 实际 = %v", len(pairs), len(want), pairs)
	}
	for k, v := range want {
		if got := pairs[k]; got != v {
			t.Errorf("%s = %q, 期望 %q", k, got, v)
		}
	}
	// 未写出的字段不应进入环境,否则会以空值顶掉 .env / 默认值。
	for _, k := range []string{"DB_DSN", "CONFIG_MASTER_KEY"} {
		if _, ok := pairs[k]; ok {
			t.Errorf("未配置的 %s 不应被映射", k)
		}
	}
}

// TestDeployFileSkipsEmptyAndPartial 验证「未配置 / 空串」都不会污染环境。
func TestDeployFileSkipsEmptyAndPartial(t *testing.T) {
	isolateConfigEnv(t)

	path := writeTemp(t, "config.yaml", `
server:
  port: 8080
database:
  mysql:
    password: ""
`)
	if err := os.Setenv("CONFIG_FILE", path); err != nil {
		t.Fatal(err)
	}

	pairs, err := LoadDeployFile()
	if err != nil {
		t.Fatalf("LoadDeployFile: %v", err)
	}
	if len(pairs) != 1 || pairs["PORT"] != "8080" {
		t.Fatalf("只应映射 PORT=8080, 实际 = %v", pairs)
	}
	// 显式写 "" 视为未配置:否则会把 .env 里真正的密码顶成空值。
	if _, ok := pairs["DB_PASSWORD"]; ok {
		t.Error("空串 password 不应映射为 DB_PASSWORD")
	}
}

// TestDeployFileUnknownKey 验证键名写错时直接报错而非静默忽略。
func TestDeployFileUnknownKey(t *testing.T) {
	isolateConfigEnv(t)

	cases := map[string]string{
		"顶层键名拼错": "serverx:\n  port: 8080\n",
		"子键名拼错":  "server:\n  prot: 8080\n",
		"层级写错":   "port: 8080\n",
	}
	for name, doc := range cases {
		path := writeTemp(t, "config.yaml", doc)
		if err := os.Setenv("CONFIG_FILE", path); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadDeployFile(); err == nil {
			t.Errorf("%s: 期望报错,实际通过", name)
		} else if !strings.Contains(err.Error(), "解析") {
			t.Errorf("%s: 错误信息应带「解析」前缀,实际 = %v", name, err)
		}
	}
}

// TestDeployFileMissing 验证文件不存在时静默跳过(单机部署无需该文件)。
func TestDeployFileMissing(t *testing.T) {
	isolateConfigEnv(t)

	if err := os.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "not-exist.yaml")); err != nil {
		t.Fatal(err)
	}
	pairs, err := LoadDeployFile()
	if err != nil {
		t.Fatalf("文件缺失不应报错: %v", err)
	}
	if pairs != nil {
		t.Errorf("文件缺失应返回 nil, 实际 = %v", pairs)
	}
}

// TestLoadDeployConfigPriority 是本功能的核心契约:
// 真实环境变量 > config.yaml > .env。
func TestLoadDeployConfigPriority(t *testing.T) {
	isolateConfigEnv(t)

	envPath := writeTemp(t, "test.env", strings.Join([]string{
		"PORT=1111",
		"DB_HOST=from-dotenv",
		"DB_NAME=from-dotenv",
		"DB_PASSWORD=dotenv-pw",
		"ADMIN_USER=from-dotenv",
		"",
	}, "\n"))
	yamlPath := writeTemp(t, "config.yaml", `
server:
  port: 2222
database:
  mysql:
    host: from-yaml
    name: from-yaml
    password: yaml-pw
`)
	if err := os.Setenv("ENV_FILE", envPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("CONFIG_FILE", yamlPath); err != nil {
		t.Fatal(err)
	}
	// 真实环境变量:应当压过 yaml 与 .env 两者。
	if err := os.Setenv("DB_PASSWORD", "real-pw"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("DB_USER", "from-real"); err != nil {
		t.Fatal(err)
	}

	st, err := LoadDeployConfig()
	if err != nil {
		t.Fatalf("LoadDeployConfig: %v", err)
	}

	checks := []struct {
		key  string
		want string
		why  string
	}{
		{"PORT", "2222", "yaml 应压过 .env"},
		{"DB_HOST", "from-yaml", "yaml 应压过 .env"},
		{"DB_NAME", "from-yaml", "yaml 应压过 .env"},
		{"DB_PASSWORD", "real-pw", "真实环境变量应压过 yaml 与 .env"},
		{"DB_USER", "from-real", "真实环境变量独有,应保留"},
		{"ADMIN_USER", "from-dotenv", ".env 独有的键应生效"},
	}
	for _, c := range checks {
		if got := os.Getenv(c.key); got != c.want {
			t.Errorf("%s = %q, 期望 %q (%s)", c.key, got, c.want, c.why)
		}
	}

	if !st.YAMLFound {
		t.Error("YAMLFound 应为 true")
	}
	if st.YAMLCount != 3 {
		t.Errorf("YAMLCount = %d, 期望 3", st.YAMLCount)
	}
	// yaml 的 3 个键在 .env 里都有同键,全部构成覆盖,故 Override = 3。
	if st.YAMLOverride != 3 {
		t.Errorf("YAMLOverride = %d, 期望 3", st.YAMLOverride)
	}
	// .env 共 5 键:PORT / DB_HOST / DB_NAME / DB_PASSWORD 已被更高优先级占据,
	// 只有 ADMIN_USER 真正落地。
	if st.DotEnvCount != 1 {
		t.Errorf("DotEnvCount = %d, 期望 1", st.DotEnvCount)
	}
}

// TestLoadDeployConfigYAMLOnly 验证只用 config.yaml、不写 .env 时也能配好。
func TestLoadDeployConfigYAMLOnly(t *testing.T) {
	isolateConfigEnv(t)

	yamlPath := writeTemp(t, "config.yaml", `
server:
  port: 7777
database:
  driver: sqlite
  sqlite:
    path: ./tmp.db
admin:
  user: owner
`)
	if err := os.Setenv("CONFIG_FILE", yamlPath); err != nil {
		t.Fatal(err)
	}
	// .env 指向不存在的文件,模拟「只用 yaml」的部署形态。
	if err := os.Setenv("ENV_FILE", filepath.Join(t.TempDir(), "none.env")); err != nil {
		t.Fatal(err)
	}

	st, err := LoadDeployConfig()
	if err != nil {
		t.Fatalf("LoadDeployConfig: %v", err)
	}
	if st.DotEnvCount != 0 {
		t.Errorf("DotEnvCount = %d, 期望 0", st.DotEnvCount)
	}
	for k, want := range map[string]string{
		"PORT": "7777", "DB_DRIVER": "sqlite", "DB_PATH": "./tmp.db", "ADMIN_USER": "owner",
	} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, 期望 %q", k, got, want)
		}
	}
}

// TestLoadDeployConfigBadYAML 验证语法错误会向上抛(调用方据此终止启动)。
func TestLoadDeployConfigBadYAML(t *testing.T) {
	isolateConfigEnv(t)

	path := writeTemp(t, "config.yaml", "server:\n\tport: 8080\n")
	if err := os.Setenv("CONFIG_FILE", path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeployConfig(); err == nil {
		t.Fatal("缩进非法时应当报错")
	}
}

// TestBoolFlagAndIntToStr 覆盖两个小工具的边界。
func TestBoolFlagAndIntToStr(t *testing.T) {
	if got := boolFlag(true); got != "1" {
		t.Errorf("boolFlag(true) = %q, 期望 \"1\"", got)
	}
	if got := boolFlag(false); got != "0" {
		t.Errorf("boolFlag(false) = %q, 期望 \"0\"", got)
	}
	if got := intToStr(nil); got != "" {
		t.Errorf("intToStr(nil) = %q, 期望空串", got)
	}
	zero := 0
	if got := intToStr(&zero); got != "0" {
		t.Errorf("intToStr(&0) = %q, 期望 \"0\"", got)
	}
}
