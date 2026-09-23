// Package conf 定义零依赖的跨包共享常量（时间格式、环境变量键名、部署默认值）。
//
// 本包禁止依赖任何其他 internal 包，避免基础常量形成反向依赖。
package conf

// ==================== 时间格式 ====================

// TimeLayout 是数据库时间列统一使用的格式。
const TimeLayout = "2006-01-02 15:04:05"

// ==================== 服务与路径 ====================

const (
	// EnvPort 是 HTTP 服务监听端口环境变量。
	EnvPort = "PORT"
	// EnvUploadDir 是上传文件目录环境变量。
	EnvUploadDir = "UPLOAD_DIR"
	// EnvStaticDir 是前端静态资源目录环境变量。
	EnvStaticDir = "STATIC_DIR"
	// EnvCORSOrigins 是跨域白名单环境变量。
	EnvCORSOrigins = "CORS_ORIGINS"
	// EnvTrustedProxies 是可信代理列表环境变量。
	EnvTrustedProxies = "TRUSTED_PROXIES"
	// EnvSnowflakeNodeID 是雪花算法节点号环境变量（多实例部署时每实例应不同）。
	EnvSnowflakeNodeID = "SNOWFLAKE_NODE_ID"
	// EnvConfigFile 是 YAML 部署配置文件路径环境变量。
	EnvConfigFile = "CONFIG_FILE"
	// EnvEnvFile 是 .env 文件路径环境变量。
	EnvEnvFile = "ENV_FILE"
	// EnvAgentJobRetentionDays 是本地打印代理已结案任务保留天数环境变量。
	EnvAgentJobRetentionDays = "AGENT_JOB_RETENTION_DAYS"
	// EnvAgentJobTTLTestMin 是测试打印任务过期分钟数环境变量。
	EnvAgentJobTTLTestMin = "AGENT_JOB_TTL_TEST_MIN"
	// EnvAgentJobTTLKitchenMin 是厨房单任务过期分钟数环境变量。
	EnvAgentJobTTLKitchenMin = "AGENT_JOB_TTL_KITCHEN_MIN"
	// EnvAgentJobTTLGuestMin 是食客小票任务过期分钟数环境变量。
	EnvAgentJobTTLGuestMin = "AGENT_JOB_TTL_GUEST_MIN"
	// EnvAgentToken 是云端预置本地打印代理初始令牌的环境变量。
	// 注意:与 EnvPrintAgentToken(PRINT_AGENT_TOKEN)是两个不同角色 ——
	// 本常量由「云端服务端」启动时读取,用于回填 tb_config.agent_token;
	// EnvPrintAgentToken 是「门店代理程序」读自己的 agent.env 里的令牌。
	// 两者值相同才能连通,但变量名与所在位置不同,不要混用。
	EnvAgentToken = "AGENT_TOKEN"
	// EnvPrintAgentServer 是本地打印代理云端地址环境变量。
	EnvPrintAgentServer = "PRINT_AGENT_SERVER"
	// EnvPrintAgentToken 是本地打印代理令牌环境变量。
	EnvPrintAgentToken = "PRINT_AGENT_TOKEN"
	// EnvPrintAgentInterval 是本地打印代理轮询间隔环境变量。
	EnvPrintAgentInterval = "PRINT_AGENT_INTERVAL"
	// EnvPrintAgentLimit 是本地打印代理单次拉取上限环境变量。
	EnvPrintAgentLimit = "PRINT_AGENT_LIMIT"
	// EnvPrintAgentName 是本地打印代理标识环境变量。
	EnvPrintAgentName = "PRINT_AGENT_NAME"
	// EnvPrintAgentState 是本地打印代理状态文件路径环境变量。
	EnvPrintAgentState = "PRINT_AGENT_STATE"
	// EnvPrintAgentInsecure 是本地打印代理跳过 TLS 校验开关环境变量。
	EnvPrintAgentInsecure = "PRINT_AGENT_INSECURE"
	// EnvAgentBinDir 是服务端存放门店打印代理产物的目录（供代理自助升级下载）。
	EnvAgentBinDir = "AGENT_BIN_DIR"
)

const (
	// DefaultPort 是 HTTP 服务默认监听端口。
	DefaultPort = "8080"
	// DefaultUploadDir 是上传文件默认目录。
	DefaultUploadDir = "./uploads"
	// DefaultStaticDir 是前端静态资源默认目录。
	DefaultStaticDir = "../frontend/dist"
	// DefaultCORSOrigins 是跨域白名单默认值（空表示不放开跨域）。
	DefaultCORSOrigins = ""
	// DefaultTrustedProxies 是可信代理默认值（空表示不信任代理）。
	DefaultTrustedProxies = ""
	// DefaultSnowflakeNodeID 是雪花算法默认节点号（单实例部署）。
	DefaultSnowflakeNodeID = "1"
	// DefaultConfigFile 是 YAML 部署配置默认文件名。
	DefaultConfigFile = "config.yaml"
	// DefaultEnvFile 是 .env 默认文件名。
	DefaultEnvFile = ".env"
	// DefaultAgentJobRetentionDays 是本地打印代理已结案任务默认保留天数。
	DefaultAgentJobRetentionDays = "7"
	// DefaultAgentJobTTLTestMin 是测试打印任务默认过期分钟数。
	DefaultAgentJobTTLTestMin = "30"
	// DefaultAgentJobTTLKitchenMin 是厨房单任务默认过期分钟数。
	DefaultAgentJobTTLKitchenMin = "120"
	// DefaultAgentJobTTLGuestMin 是食客小票任务默认过期分钟数。
	DefaultAgentJobTTLGuestMin = "1440"
	// DefaultPrintAgentEnvFile 是本地打印代理默认配置文件名。
	DefaultPrintAgentEnvFile = "agent.env"
	// DefaultAgentBinDir 是代理产物默认目录（发布包内 print-agent/bin，与后端可执行文件同层级）。
	DefaultAgentBinDir = "print-agent/bin"
	// DefaultPrintAgentServer 是本地打印代理云端地址默认值。
	DefaultPrintAgentServer = ""
	// DefaultPrintAgentToken 是本地打印代理令牌默认值。
	DefaultPrintAgentToken = ""
	// DefaultPrintAgentInterval 是本地打印代理默认轮询间隔（秒）。
	DefaultPrintAgentInterval = 3
	// DefaultPrintAgentLimit 是本地打印代理默认单次拉取上限。
	DefaultPrintAgentLimit = 10
	// DefaultPrintAgentName 是本地打印代理标识默认值（空时取主机名或 print-agent）。
	DefaultPrintAgentName = ""
	// DefaultPrintAgentState 是本地打印代理状态文件默认值（空时取程序同目录）。
	DefaultPrintAgentState = ""
	// DefaultPrintAgentInsecure 是本地打印代理跳过 TLS 校验默认值。
	DefaultPrintAgentInsecure = ""
)

// ==================== 数据库 ====================

const (
	// EnvDBDriver 是数据库驱动选择环境变量。
	EnvDBDriver = "DB_DRIVER"
	// EnvDBDSN 是数据库完整连接串环境变量。
	EnvDBDSN = "DB_DSN"
	// EnvDBParams 是 MySQL 连接参数环境变量。
	EnvDBParams = "DB_PARAMS"
	// EnvDBUser 是 MySQL 用户名环境变量。
	EnvDBUser = "DB_USER"
	// EnvDBPassword 是 MySQL 密码环境变量。
	EnvDBPassword = "DB_PASSWORD"
	// EnvDBHost 是 MySQL 主机环境变量。
	EnvDBHost = "DB_HOST"
	// EnvDBPort 是 MySQL 端口环境变量。
	EnvDBPort = "DB_PORT"
	// EnvDBName 是 MySQL 数据库名环境变量。
	EnvDBName = "DB_NAME"
	// EnvDBPath 是 SQLite 数据文件路径环境变量。
	EnvDBPath = "DB_PATH"
)

const (
	// DefaultDBDriver 是默认数据库驱动。
	DefaultDBDriver = "sqlite"
	// DefaultDBDSN 是数据库完整连接串默认值。
	DefaultDBDSN = ""
	// DefaultDBParams 是 MySQL 默认连接参数。
	DefaultDBParams = "charset=utf8mb4&parseTime=true&loc=Local&maxAllowedPacket=67108864"
	// DefaultDBUser 是 MySQL 默认用户名。
	DefaultDBUser = "root"
	// DefaultDBPassword 是 MySQL 默认密码。
	DefaultDBPassword = ""
	// DefaultDBHost 是 MySQL 默认主机。
	DefaultDBHost = "127.0.0.1"
	// DefaultDBPort 是 MySQL 默认端口。
	DefaultDBPort = "3306"
	// DefaultDBName 是 MySQL 默认数据库名。
	DefaultDBName = "dining"
	// DefaultDBPath 是 SQLite 默认数据文件路径。
	DefaultDBPath = "./data/dining.db"
)

// ==================== 密钥与安全 ====================

const (
	// EnvConfigMasterKey 是直接提供配置加密主密钥的环境变量。
	EnvConfigMasterKey = "CONFIG_MASTER_KEY"
	// EnvMasterKeyPath 是配置加密主密钥文件路径环境变量。
	EnvMasterKeyPath = "MASTER_KEY_PATH"
	// EnvMasterKeyOld 是配置加密旧主密钥环境变量。
	EnvMasterKeyOld = "MASTER_KEY_OLD"
	// EnvTokenTTLHours 是管理端登录令牌有效期环境变量。
	EnvTokenTTLHours = "TOKEN_TTL_HOURS"
	// EnvTokenSecret 是登录令牌签名密钥(优先级最高,hex 或原文均可)。
	EnvTokenSecret = "TOKEN_SECRET"
	// EnvAuthKeyPath 是登录令牌签名密钥文件路径环境变量。
	EnvAuthKeyPath = "AUTH_KEY_PATH"
	// EnvHardenFileACL 是 Windows 下收紧主密钥文件 ACL 的环境变量。
	EnvHardenFileACL = "HARDEN_FILE_ACL"
)

const (
	// DefaultConfigMasterKey 是配置加密主密钥默认值（空表示不直接提供）。
	DefaultConfigMasterKey = ""
	// DefaultMasterKeyPath 是配置加密主密钥默认文件路径。
	DefaultMasterKeyPath = "data/master.key"
	// DefaultAuthKeyPath 是登录令牌签名密钥默认文件路径。
	DefaultAuthKeyPath = "data/auth.key"
	// DefaultMasterKeyOld 是配置加密旧主密钥默认值（空表示不轮换）。
	DefaultMasterKeyOld = ""
	// DefaultTokenTTLHours 是管理端登录令牌默认有效期（小时）。
	DefaultTokenTTLHours = "24"
	// DefaultHardenFileACL 是 Windows 下收紧主密钥文件 ACL 的默认开关值。
	DefaultHardenFileACL = ""
)

// ==================== 日志 ====================

const (
	// EnvLogPath 是日志文件路径环境变量。
	EnvLogPath = "LOG_PATH"
	// EnvLogAgentPath 是门店打印代理专属日志文件路径环境变量(默认 ./logs/agent.log,
	// 设为 off/none/false/0/- 时回落并入主日志文件, 口径见 logger.resolveAgentLogPath)。
	EnvLogAgentPath = "LOG_AGENT_PATH"
	// EnvLogLevel 是日志级别环境变量。
	EnvLogLevel = "LOG_LEVEL"
	// EnvLogMaxSizeMB 是日志轮转单文件最大尺寸环境变量。
	EnvLogMaxSizeMB = "LOG_MAX_SIZE_MB"
	// EnvLogMaxAgeDays 是日志轮转最大保留天数环境变量。
	EnvLogMaxAgeDays = "LOG_MAX_AGE_DAYS"
	// EnvLogMaxBackups 是日志轮转最大备份数环境变量。
	EnvLogMaxBackups = "LOG_MAX_BACKUPS"
)

const (
	// DefaultLogPath 是日志文件默认路径。
	DefaultLogPath = "./logs/app.log"
	// DefaultLogLevel 是默认日志级别。
	DefaultLogLevel = "info"
	// DefaultLogMaxSizeMB 是日志轮转默认单文件最大尺寸（MB）。
	DefaultLogMaxSizeMB = 50
	// DefaultLogMaxAgeDays 是日志轮转默认最大保留天数。
	DefaultLogMaxAgeDays = 30
	// DefaultLogMaxBackups 是日志轮转默认最大备份数。
	DefaultLogMaxBackups = 7
)

// ==================== 审计 ====================

const (
	// EnvAuditLogEnabled 是操作审计日志总开关环境变量。
	EnvAuditLogEnabled = "AUDIT_LOG_ENABLED"
	// EnvAuditLogGet 是只读请求审计日志开关环境变量。
	EnvAuditLogGet = "AUDIT_LOG_GET"
	// EnvAuditRetentionDays 是操作审计日志保留天数环境变量。
	EnvAuditRetentionDays = "AUDIT_RETENTION_DAYS"
)

const (
	// DefaultAuditLogEnabled 是操作审计日志总开关默认值。
	DefaultAuditLogEnabled = "1"
	// DefaultAuditLogGet 是只读请求审计日志开关默认值。
	DefaultAuditLogGet = "0"
	// DefaultAuditRetentionDays 是操作审计日志默认保留天数。
	DefaultAuditRetentionDays = "365"
)

// ==================== 管理员 ====================

const (
	// EnvAdminUser 是初始超级管理员用户名环境变量。
	EnvAdminUser = "ADMIN_USER"
	// EnvAdminPass 是初始超级管理员密码环境变量。
	EnvAdminPass = "ADMIN_PASS"
)

const (
	// DefaultAdminUser 是初始超级管理员默认用户名。
	DefaultAdminUser = "admin"
	// DefaultAdminPass 是初始超级管理员默认密码。
	DefaultAdminPass = "admin123"
)
