// 轻量 .env 文件加载。
//
// 目的:切换数据库、调整端口这类部署差异,写进配置文件比每次在命令行注入
// 环境变量更省事,也便于在 systemd / 启动脚本里统一引用。
//
// 设计取舍:
//   - 不引入第三方依赖(godotenv 之类),只支持 KEY=VALUE 这一种最常见的语法;
//   - **真实环境变量优先级更高**:已存在的变量绝不被文件覆盖。这样
//     「文件里放默认值 + 临时用环境变量覆盖」的用法成立,也符合 12-Factor;
//   - 支持 # 注释、单/双引号包裹、行尾注释、`export` 前缀;
//   - 文件不存在时静默跳过(默认单机 SQLite 部署完全不需要该文件)。
//
// 本文件只负责「文件 → 键值表」的解析与单文件补全;需要与 config.yaml
// 协同(三层来源统一编排)时请使用 deploy.go 的 LoadDeployConfig。
package infra

import (
	"os"
	"path/filepath"
	"strings"

	"dining-system/infra/logger"
	"dining-system/internal/conf"
)

// LoadDotEnv 加载 .env 文件(不存在则静默返回)。
//
// 查找顺序:ENV_FILE 环境变量指定的路径 > 当前目录 .env。
// 返回实际加载的条目数,便于启动日志提示。
//
// 仅做「补全」:已存在的环境变量一律不覆盖。若同时使用 config.yaml,
// 请改用 LoadDeployConfig(它负责按 环境变量 > config.yaml > .env 的顺序编排)。
func LoadDotEnv() int {
	pairs, err := readDotEnvFile(dotEnvPath())
	if err != nil {
		logger.Warnf("[env] 读取 .env 失败,已跳过: %v", err)
		return 0
	}
	return applyPairs(pairs)
}

// dotEnvPath 返回 .env 文件路径:ENV_FILE 可覆盖,默认当前工作目录下 .env。
//
// Windows 注意:filepath.Clean 会把 /tmp/x.env 变成 C:\tmp\x.env,
// 因此跨平台脚本里请写 C:/path/.env 形式。
func dotEnvPath() string {
	path := strings.TrimSpace(os.Getenv(conf.EnvEnvFile))
	if path == "" {
		path = conf.DefaultEnvFile
	}
	return filepath.Clean(path)
}

// readDotEnvFile 把 .env 解析成键值表;文件不存在返回 (nil, nil) 而非错误
// (默认 SQLite 单机部署无需该文件)。
//
// 同键重复定义时保留**首次**出现的值,与解析器「先到先得」的历史行为一致。
func readDotEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	out := make(map[string]string)
	for i, raw := range strings.Split(string(data), "\n") {
		key, val, ok := parseDotEnvLine(raw)
		if !ok {
			continue
		}
		if _, dup := out[key]; dup {
			logger.Warnf("[env] %s:%d 键 %s 重复定义,保留先出现的值", path, i+1, key)
			continue
		}
		out[key] = val
	}
	return out, nil
}

// applyPairs 把键值表写入进程环境,只填补尚未设置的键,返回实际写入数。
// 传入 nil 表示「该来源没有配置」,安全返回 0。
func applyPairs(pairs map[string]string) int {
	n := 0
	for k, v := range pairs {
		if _, exists := os.LookupEnv(k); exists {
			continue // 更高优先级的来源已提供该键
		}
		if err := os.Setenv(k, v); err != nil {
			logger.Warnf("[env] 设置 %s 失败: %v", k, err)
			continue
		}
		n++
	}
	return n
}

// parseDotEnvLine 解析一行 .env 内容。
//
// 支持:
//
//	KEY=VALUE
//	KEY = VALUE          (等号两侧空白忽略)
//	export KEY=VALUE
//	KEY="含 空格 的值"    (单/双引号包裹,内部保留空白)
//	KEY=value   # 说明    (行尾注释,需 # 前有空白)
//	# 整行注释           (忽略)
func parseDotEnvLine(raw string) (key, val string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

	eq := strings.Index(line, "=")
	if eq <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	if key == "" || strings.ContainsAny(key, " \t\"'") {
		return "", "", false // 非法键名,跳过
	}
	val = strings.TrimSpace(line[eq+1:])

	// 引号包裹:取引号内的原文(允许包含 # 与空白)。
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			quote := val[0]
			val = val[1 : len(val)-1]
			if quote == '"' {
				// 双引号内支持 \n \t \" \\ 转义
				val = unescapeDoubleQuoted(val)
			}
			return key, val, true
		}
	}

	// 未加引号:去掉行尾注释(要求 # 之前是空白,避免把密码里的 # 截断)。
	if i := strings.Index(val, " #"); i >= 0 {
		val = strings.TrimSpace(val[:i])
	} else if i := strings.Index(val, "\t#"); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	return key, val, true
}

// unescapeDoubleQuoted 处理双引号包裹值里的转义序列。
func unescapeDoubleQuoted(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i == len(s)-1 {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(s[i]) // \" \\ \' 等:原样输出
		}
	}
	return b.String()
}
