package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/conf"
)

// LoadAllowedOrigins 解析 CORS_ORIGINS 环境变量(逗号分隔)得到跨域白名单。
// 未配置时不放开任何跨域——本项目前后端同源(经 Nginx 反代),生产无需跨域;
// 仅当 H5 点餐页单独部署在其他域名时,才需要配置具体 origin。
//
// 必须在部署配置(config.yaml/.env)写入进程环境之后调用,否则配置文件里的
// cors_origins 会静默失效。本包不能 import infra(infra → infra/logger →
// middleware 会形成循环依赖),因此直接读 os.Getenv 并沿用 conf 默认值,
// 语义与 infra.Getenv(key, def) 一致(默认值均为空)。
func LoadAllowedOrigins() map[string]bool {
	raw := os.Getenv(conf.EnvCORSOrigins)
	if raw == "" {
		raw = conf.DefaultCORSOrigins
	}
	m := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			m[o] = true
		}
	}
	return m
}

// LoadTrustedProxies 解析 TRUSTED_PROXIES 环境变量(逗号分隔的 IP/CIDR)作为可信代理列表。
// 默认返回 nil(不信任任何代理),此时 gin 的 ClientIP() 取 RemoteAddr,可防止客户端伪造 XFF。
// 经 nginx 反代部署时,需设置 TRUSTED_PROXIES(如 127.0.0.1 或 nginx 的 IP),
// 登录限流才能取到真实客户端 IP(否则所有请求共享 nginx 来源,退化为全局粒度)。
func LoadTrustedProxies() []string {
	raw := os.Getenv(conf.EnvTrustedProxies)
	if raw == "" {
		raw = conf.DefaultTrustedProxies
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NewCORS 返回跨域中间件:仅对白名单内的 Origin 放开跨域;不携带凭据,降低 CSRF 面。
// allowedOrigins 由调用方在部署配置装配完成后求值后传入,避免在包级 var 初始化
// 阶段(早于 config.yaml/.env 写入进程环境)提前求值。
func NewCORS(allowedOrigins map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Max-Age", "86400")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
