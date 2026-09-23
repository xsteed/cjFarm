// config.yaml 配置文件支持。
//
// 背景:早期版本用「环境变量 + .env」做部署配置。两者都是扁平 KEY=VALUE 结构,
// 键名偏底层(DB_HOST / TRUSTED_PROXIES),写出来可读性一般,也不好分层排布。
// 本文件补上 YAML 方式,让部署配置长这样:
//
//	server:
//	  port: 8080
//	database:
//	  driver: mysql
//	  mysql:
//	    host: 10.0.0.9
//
// 设计要点:
//   - **优先级:真实环境变量 > config.yaml > .env > 代码默认值**。
//     yaml 与 .env 都只是「键值表」,最终都写回进程环境,业务代码仍然只认
//     infra.Getenv(key, def),因此新增来源不需要改动任何一行业务逻辑;
//   - **只映射显式出现的字段**。yaml 里没写的键不会进入环境,继续往下回退,
//     避免零值("" / 0 / false)反而把其它来源配好的值顶掉;
//   - **未知键直接报错**(KnownFields)。部署时把 upload_dir 拼成 uploadDir
//     是最常见的坑,静默忽略会让人排查半天;
//   - 文件不存在时静默跳过,单机 SQLite 部署可以完全不建该文件。
//
// 与 .env 的关系:两者可以共存,也可以只用其中一个。同时存在时 yaml 优先,
// 启动日志会提示被 yaml 覆盖掉的 .env 条目数,便于排查「改了不生效」。
package infra

import (
	"dining-system/infra/logger"
	"dining-system/internal/conf"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeployFile 是 config.yaml 的完整结构。
//
// 标量字段一律用指针,以便区分「未配置」与「配置成零值」——
// 这是保证回退行为正确的关键。
type DeployFile struct {
	Server   ServerSection   `yaml:"server"`
	Database DatabaseSection `yaml:"database"`
	Security SecuritySection `yaml:"security"`
	Admin    AdminSection    `yaml:"admin"`
}

// ServerSection 对应 HTTP 服务与静态资源相关配置。
type ServerSection struct {
	Port           *int     `yaml:"port"`            // -> PORT,默认 8080
	UploadDir      *string  `yaml:"upload_dir"`      // -> UPLOAD_DIR,默认 ./uploads
	StaticDir      *string  `yaml:"static_dir"`      // -> STATIC_DIR,默认 ../frontend/dist
	CorsOrigins    []string `yaml:"cors_origins"`    // -> CORS_ORIGINS(逗号拼接),默认不放开跨域
	TrustedProxies []string `yaml:"trusted_proxies"` // -> TRUSTED_PROXIES(逗号拼接),默认不信任代理
	// 雪花算法节点号(0~1023):requestID 生成器使用。多实例部署时每实例应不同,
	// 否则可能生成重复 requestID(默认 1)。
	SnowflakeNodeID *int `yaml:"snowflake_node_id"` // -> SNOWFLAKE_NODE_ID,默认 1
}

// DatabaseSection 对应数据库后端选择与连接参数。
type DatabaseSection struct {
	Driver *string       `yaml:"driver"` // -> DB_DRIVER,sqlite | mysql,默认 sqlite
	SQLite SQLiteSection `yaml:"sqlite"`
	MySQL  MySQLSection  `yaml:"mysql"`
}

// SQLiteSection 对应 SQLite 后端(默认)。
type SQLiteSection struct {
	Path *string `yaml:"path"` // -> DB_PATH,默认 ./data/dining.db
}

// MySQLSection 对应 MySQL 后端。DSN 非空时优先于下面的分项配置。
type MySQLSection struct {
	DSN      *string `yaml:"dsn"`      // -> DB_DSN
	Host     *string `yaml:"host"`     // -> DB_HOST,默认 127.0.0.1
	Port     *int    `yaml:"port"`     // -> DB_PORT,默认 3306
	User     *string `yaml:"user"`     // -> DB_USER,默认 root
	Password *string `yaml:"password"` // -> DB_PASSWORD,默认空
	Name     *string `yaml:"name"`     // -> DB_NAME,默认 dining
	Params   *string `yaml:"params"`   // -> DB_PARAMS,默认 charset=utf8mb4&parseTime=true&loc=Local
}

// SecuritySection 对应敏感配置加密与登录令牌。
type SecuritySection struct {
	MasterKey     *string `yaml:"master_key"`      // -> CONFIG_MASTER_KEY,留空则用文件方式
	MasterKeyPath *string `yaml:"master_key_path"` // -> MASTER_KEY_PATH,默认 ./data/master.key
	// 轮换用旧主密钥(-> MASTER_KEY_OLD,格式同 master_key):主密钥更换后,
	// 启动时自动把旧密钥加密的敏感配置改写为新密钥加密,完成后应删除本项。
	MasterKeyOld  *string `yaml:"master_key_old"`
	TokenTTLHours *int    `yaml:"token_ttl_hours"` // -> TOKEN_TTL_HOURS,默认 24
	HardenFileACL *bool   `yaml:"harden_file_acl"` // -> HARDEN_FILE_ACL,"1" / "0"
}

// AdminSection 对应管理端初始账号(仅首次建库时写入)。
type AdminSection struct {
	User *string `yaml:"user"` // -> ADMIN_USER,默认 admin
	Pass *string `yaml:"pass"` // -> ADMIN_PASS,默认 admin123
}

// DeployStats 描述配置加载结果,供启动日志与测试使用。
type DeployStats struct {
	YAMLPath     string // 实际尝试读取的 config.yaml 路径
	YAMLCount    int    // config.yaml 生效的条目数
	YAMLFound    bool   // config.yaml 是否存在
	YAMLOverride int    // config.yaml 因优先级更高而生效、把 .env 同键顶掉的条目数

	DotEnvPath  string // 实际尝试读取的 .env 路径
	DotEnvCount int    // .env 生效的条目数
}

// LoadDeployFile 读取 config.yaml 并返回「环境变量键值对」,不写入进程环境。
//
// 查找顺序:CONFIG_FILE 环境变量指定的路径 > 当前工作目录 config.yaml。
// 文件不存在返回 (nil, nil)。
func LoadDeployFile() (map[string]string, error) {
	path := deployFilePath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("打开 %s 失败: %w", path, err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true) // 拼错的键名直接报错,不静默忽略

	var cfg DeployFile
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]string{}, nil // 空文件视为没配置
		}
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return cfg.envPairs(), nil
}

// deployFilePath 返回 config.yaml 路径:CONFIG_FILE 可覆盖。
func deployFilePath() string {
	path := strings.TrimSpace(os.Getenv(conf.EnvConfigFile))
	if path == "" {
		path = conf.DefaultConfigFile
	}
	return filepath.Clean(path)
}

// LoadDeployConfig 统一装配部署配置,是启动时唯一需要调用的入口。
//
// 命名:业务配置读取(store.GetSetting / store.LoadSettings)在 store 包,
// 本函数是进程启动的部署参数装配,故加 Deploy 前缀区分。
//
// 优先级(高 → 低):
//
//  1. 真实环境变量  —— 进程启动时已存在,本函数不会覆盖
//  2. config.yaml   —— CONFIG_FILE / ./config.yaml
//  3. .env          —— ENV_FILE / ./.env
//  4. 代码默认值    —— 由各处 Getenv(key, def) 兜底
//
// 关键实现:先落 yaml、再落 .env,且两者都遵循「已存在即不覆盖」。
// 这样 yaml 天然压过 .env,而已在进程环境里的真实变量又压过两者。
//
// config.yaml 解析失败会返回 error(调用方应终止启动):部署阶段把键名或
// 缩进写错却静默跑起来,比直接报错难排查得多。
func LoadDeployConfig() (DeployStats, error) {
	var st DeployStats

	// 第 2、3 步先各自解析成表,暂不写入 —— 否则先落地的来源会误判成
	// 「已存在的真实环境变量」,把自己的优先级抬高。
	yamlPairs, err := LoadDeployFile()
	if err != nil {
		return st, err
	}
	dotEnvPairs, err := readDotEnvFile(dotEnvPath())
	if err != nil {
		// .env 读不了不算致命(权限等),记一笔继续。
		logDeployf("读取 .env 失败,已跳过: %v", err)
	}

	st.YAMLPath, st.YAMLFound = deployFilePath(), yamlPairs != nil
	st.DotEnvPath = dotEnvPath()

	// 先统计「yaml 顶掉了多少条 .env」——必须赶在 applyPairs 之前算:
	// 落环境之后已是合并结果,再无从区分某个键来自哪个来源。
	// 被真实环境变量拦下的键不算 yaml 的功劳,故先排除。
	for k := range yamlPairs {
		if _, blocked := os.LookupEnv(k); blocked {
			continue
		}
		if _, inDotEnv := dotEnvPairs[k]; inDotEnv {
			st.YAMLOverride++
		}
	}

	// 第 2 步:config.yaml 落入环境。
	st.YAMLCount = applyPairs(yamlPairs)

	// 第 3 步:.env 只填补 yaml 与真实环境变量都没提供的键。
	st.DotEnvCount = applyPairs(dotEnvPairs)

	return st, nil
}

// envPairs 把配置结构映射为环境变量键值对。
//
// 只映射「显式写出且非空」的字段:留空的字符串视为未配置,继续回退到
// .env 或默认值。这能挡住一个常见陷阱 —— 模板里留着 `password: ""`,
// 却把真正的密码写在 .env 里,结果被空值顶掉。
func (c *DeployFile) envPairs() map[string]string {
	m := make(map[string]string)

	setStr(m, conf.EnvPort, intToStr(c.Server.Port))
	setStrPtr(m, conf.EnvUploadDir, c.Server.UploadDir)
	setStrPtr(m, conf.EnvStaticDir, c.Server.StaticDir)
	setList(m, conf.EnvCORSOrigins, c.Server.CorsOrigins)
	setList(m, conf.EnvTrustedProxies, c.Server.TrustedProxies)
	setStr(m, conf.EnvSnowflakeNodeID, intToStr(c.Server.SnowflakeNodeID))

	setStrPtr(m, conf.EnvDBDriver, c.Database.Driver)
	setStrPtr(m, conf.EnvDBPath, c.Database.SQLite.Path)
	setStrPtr(m, conf.EnvDBDSN, c.Database.MySQL.DSN)
	setStrPtr(m, conf.EnvDBHost, c.Database.MySQL.Host)
	setStr(m, conf.EnvDBPort, intToStr(c.Database.MySQL.Port))
	setStrPtr(m, conf.EnvDBUser, c.Database.MySQL.User)
	setStrPtr(m, conf.EnvDBPassword, c.Database.MySQL.Password)
	setStrPtr(m, conf.EnvDBName, c.Database.MySQL.Name)
	setStrPtr(m, conf.EnvDBParams, c.Database.MySQL.Params)

	setStrPtr(m, conf.EnvConfigMasterKey, c.Security.MasterKey)
	setStrPtr(m, conf.EnvMasterKeyPath, c.Security.MasterKeyPath)
	setStrPtr(m, conf.EnvMasterKeyOld, c.Security.MasterKeyOld)
	setStr(m, conf.EnvTokenTTLHours, intToStr(c.Security.TokenTTLHours))
	if c.Security.HardenFileACL != nil {
		m[conf.EnvHardenFileACL] = boolFlag(*c.Security.HardenFileACL)
	}

	setStrPtr(m, conf.EnvAdminUser, c.Admin.User)
	setStrPtr(m, conf.EnvAdminPass, c.Admin.Pass)

	return m
}

// ---------------- 内部工具 ----------------

// setStr 写入非空字符串;val 为空串时视为「未配置」而跳过。
func setStr(m map[string]string, key, val string) {
	if strings.TrimSpace(val) != "" {
		m[key] = val
	}
}

// setStrPtr 写入可选字符串指针;nil 或空串均视为「未配置」而跳过。
func setStrPtr(m map[string]string, key string, v *string) {
	if v == nil {
		return
	}
	setStr(m, key, *v)
}

// setList 把字符串列表拼成逗号分隔值;全为空项时跳过。
func setList(m map[string]string, key string, vals []string) {
	kept := make([]string, 0, len(vals))
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			kept = append(kept, v)
		}
	}
	if len(kept) > 0 {
		m[key] = strings.Join(kept, ",")
	}
}

// intToStr 把可选整数转成字符串;nil 返回空串(由 setStr 判定跳过)。
func intToStr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// boolFlag 把布尔值归一化成 '1' / '0',与 .env 里开关字段的取值保持一致。
func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// logDeployf 是 logger.Infof 的薄包装,便于本文件与日志格式对齐。
func logDeployf(format string, args ...interface{}) {
	logger.Infof("[deploy] "+format, args...)
}
