package middleware

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"dining-system/infrastructure/conf"
)

// 顾客端「按单号查订单」接口的单 IP 限流。
//
// 背景:GET /api/customer/order/no/:orderNo 是无鉴权的公开接口,订单号会出现在
// 小票、照片、群聊里。归属校验(tableId 参数)是第一道防线,但老二维码链接不带
// 桌台参数,仍需限流兜底 —— 零限流下遍历单号的边际成本为零。
//
// 实现为进程内固定窗口计数器:每个 IP 从首个请求起开一个 window 长度的窗口,
// 窗口内超过 max 次直接 429。不引入 redis 等外部依赖(单机 SQLite 部署用不到);
// 多实例部署时限流为各实例独立计数,整体容量 = 单实例值 × 实例数,只会更宽松,
// 不会误伤。
//
// 窗口精度取分钟:轮询类流量对「这一分钟超了 1 个」不敏感,而按秒记桶会让
// 内存里同时存在 60 倍 entry。过期 key 按窗口节流清理(见下方 lastCleanup),
// 容量上界 = 一个窗口内的独立 IP 数。

type ipBucket struct {
	windowStart time.Time // 窗口起点(该 IP 本窗口首个请求时刻)
	count       int       // 窗口内已计数
}

type ipRateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*ipBucket
	window      time.Duration
	max         int
	lastCleanup time.Time // 上次全表清理时刻(清理节流用)
}

// NewIPRateLimit 返回单 IP 限流中间件:每个 IP 每个 window 内最多 max 个请求,
// 超出返回 429(带 Retry-After,便于前端/代理克制重试)。
//
// limit <= 0 表示不启用(直接放行),便于部署侧一键关闭。
func NewIPRateLimit(window time.Duration, max int) gin.HandlerFunc {
	if max <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	lim := &ipRateLimiter{
		buckets:     map[string]*ipBucket{},
		window:      window,
		max:         max,
		lastCleanup: time.Now(),
	}
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		lim.mu.Lock()
		// 过期 key 清理按窗口节流(每个窗口最多扫一次全表):若每请求都全表扫描,
		// 大量不同 IP 的流量会让限流器自身变成 O(N²) 的放大面;按窗口清理把
		// 扫描开销摊到每分钟一次。不清理则 map 随历史 IP 数单调增长。
		if now.Sub(lim.lastCleanup) >= lim.window {
			for k, b := range lim.buckets {
				if now.Sub(b.windowStart) >= lim.window {
					delete(lim.buckets, k)
				}
			}
			lim.lastCleanup = now
		}
		b, ok := lim.buckets[ip]
		if !ok || now.Sub(b.windowStart) >= lim.window {
			b = &ipBucket{windowStart: now, count: 0}
			lim.buckets[ip] = b
		}
		b.count++
		over := b.count > lim.max
		lim.mu.Unlock()

		if over {
			// Retry-After 取本窗口剩余秒数(向上取整),告诉调用方最早何时可再试。
			retryAfter := int(time.Until(b.windowStart.Add(lim.window)).Seconds()) + 1
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "请求过于频繁，请稍后再试"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// LoadCustomerOrderQueryRateLimit 解析顾客端按单号查订单接口的单 IP 限流值
// (次/分钟,<=0 表示关闭)。与 LoadAllowedOrigins 同一模式:直接读环境变量,
// 在部署配置装配完成后调用(见 main.go 的说明)。
func LoadCustomerOrderQueryRateLimit() int {
	raw := os.Getenv(conf.EnvCustomerOrderQueryRateLimit)
	if raw == "" {
		return conf.DefaultCustomerOrderQueryRateLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return conf.DefaultCustomerOrderQueryRateLimit
	}
	return n
}
