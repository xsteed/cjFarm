// Package infra 提供部署配置与运行环境工具。
package infra

import "os"

// Getenv 返回环境变量值,为空时回退到默认值。
func Getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
