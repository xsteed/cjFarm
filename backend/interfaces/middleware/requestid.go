// Package middleware 提供挂在 gin.Engine 上的全局 HTTP 中间件。
//
// 与 handler 包里的业务中间件(AdminAuth / RequirePerm / AuditLog,
// 挂在特定路由组上)不同,本包是「每个请求都会经过」的全局层:
// requestID 的生成与注入,让访问日志、业务日志、panic 堆栈、响应头
// 都能以同一 ID 关联到一个请求,是排障链路追踪的基础。
package middleware

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/snowflake"
)

// GinContextKey 是 requestID 在 gin.Context 里的键(值类型 int64),
// 供 c.Get / c.Set 与日志中间件读取。
// 复用 logger 的常量:访问日志要读同一个键,两处各写字面量会静默断链。
const GinContextKey = logger.GinRequestIDKey

// HeaderRequestID 是回传给调用方的响应头名:nginx 访问日志、前端
// 报错截图里的该值可直接用于定位后端日志。
const HeaderRequestID = "X-Request-ID"

// defaultNodeID 未配置节点号时的默认值(单实例部署)。
const defaultNodeID int64 = 1

var (
	nodeOnce sync.Once
	node     *snowflake.Node
)

// RequestID 返回全局中间件:为每个请求生成雪花算法 requestID,并
//  1. 注入标准 context.Context(c.Request 随之下传,业务/仓储层用 FromContext 读取);
//  2. 写入 gin.Context(GinContextKey),供中间件链与访问日志读取;
//  3. 通过 X-Request-ID 响应头回传,供前端/网关关联。
//
// 必须挂在日志中间件之前,访问日志才能读到 ID。
func RequestID() gin.HandlerFunc {
	n := defaultNode()
	return func(c *gin.Context) {
		id := n.Generate()

		// context 通道复用 logger 的键:logger.CtxInfof(ctx, ...) 与
		// logger.WithContext(ctx) 才能读到同一个 ID,业务日志与访问日志就此串联。
		c.Request = c.Request.WithContext(logger.ContextWithRequestID(c.Request.Context(), id))
		c.Set(GinContextKey, id)
		c.Header(HeaderRequestID, strconv.FormatInt(id, 10))

		c.Next()
	}
}

// FromContext 从标准 context.Context 读取 requestID,供业务/仓储层使用。
func FromContext(ctx context.Context) (int64, bool) {
	return logger.RequestIDFromContext(ctx)
}

// FromGin 从 gin.Context 读取 requestID,供中间件链与日志使用。
func FromGin(c *gin.Context) (int64, bool) {
	v, ok := c.Get(GinContextKey)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

// GetString 返回 requestID 的十进制字符串形式(日志拼接用);不存在时返回空串。
func GetString(c *gin.Context) string {
	if id, ok := FromGin(c); ok {
		return strconv.FormatInt(id, 10)
	}
	return ""
}

// defaultNode 返回进程级雪花生成器(懒初始化,读 SNOWFLAKE_NODE_ID)。
// 配置非法(非数字 / 越界)时回退默认节点号 1,保证单实例永远可用。
func defaultNode() *snowflake.Node {
	nodeOnce.Do(func() {
		node, _ = snowflake.New(nodeIDFromEnv())
		if node == nil {
			// nodeIDFromEnv 已把输入限制在合法区间,此分支当前不可达;
			// 兜底默认节点号是防止将来改动配置解析引入 nil 指针 ——
			// RequestID 挂在每个请求上,nil 会 panic 掉全部请求而非单请求失败。
			node, _ = snowflake.New(defaultNodeID)
		}
	})
	return node
}

// nodeIDFromEnv 解析 SNOWFLAKE_NODE_ID:未配置或非法一律回退默认值。
//
// 依赖方向是 middleware → logger(单向,不成环),故这里可以打日志:
// 多实例部署时节点号配错会让 requestID 撞号,而撞号的后果(两条链路的日志
// 混在一起)排查起来极费时,因此必须显式告警而不是静默回退。
// 取值范围在 config.example.yaml 中说明。
func nodeIDFromEnv() int64 {
	v := strings.TrimSpace(os.Getenv(conf.EnvSnowflakeNodeID))
	if v == "" {
		return defaultNodeID
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > snowflake.MaxNodeID {
		logger.Warnf("[requestID] SNOWFLAKE_NODE_ID=%q 非法(应为 0~%d 的整数),已回退节点号 %d;"+
			"多实例部署撞号会让不同实例的日志串在一起,请检查配置", v, snowflake.MaxNodeID, defaultNodeID)
		return defaultNodeID
	}
	return n
}
