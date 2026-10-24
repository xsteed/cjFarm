package handler

import (
	"github.com/gin-gonic/gin"
)

// 运行期版本信息,由 main 包在启动时通过 InitBuildInfo 注入
// (编译期 -ldflags 注入 main.version / main.gitCommit / main.buildTime)。
// 提供 GET /api/version,用于部署后即时确认「正在运行的进程」是哪个版本,
// 不依赖回翻启动日志或读安装目录 VERSION 文件(后者可能与内存态不一致)。
var (
	buildVersion   = "unknown"
	buildGitCommit = "unknown"
	buildBuildTime = "unknown"
)

// InitBuildInfo 注入运行期版本信息。必须在任意请求到达前调用一次
// (main 启动时 setupAPI 之前)。测试可显式调用以固定返回值。
func InitBuildInfo(version, gitCommit, buildTime string) {
	if version != "" {
		buildVersion = version
	}
	if gitCommit != "" {
		buildGitCommit = gitCommit
	}
	if buildTime != "" {
		buildBuildTime = buildTime
	}
}

// Version 返回运行期版本信息(公开,无需登录)。
//
//	GET /api/version -> { code:200, data:{ version, gitCommit, buildTime } }
func Version(c *gin.Context) {
	ok(c, map[string]string{
		"version":   buildVersion,
		"gitCommit": buildGitCommit,
		"buildTime": buildBuildTime,
	})
}
