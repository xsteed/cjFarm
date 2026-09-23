package handler

import (
	"net/http"
	"path/filepath"
	"sync"

	"github.com/gin-gonic/gin"

	"dining-system/infra/logger"
	"dining-system/internal/service"
)

// ============================================================================
// 图片内容进程内缓存
//
// 展示链路从 tb_image 读到图片后回填到此处,再次请求同一文件名即可免一次数据库查询。
// key 是文件名、内容一旦入库即不可变,因此缓存无需失效;容量受「条数上限」与
// 「总字节预算」双约束,超限时按插入序(FIFO)逐条淘汰最旧条目。相比「写满整体
// 清空重建」,这样既把峰值内存从 256 × 5MB ≈ 1.28GB 收敛到字节预算以内,
// 又保留更多命中率(只淘汰最旧,不误伤最近热点)。
//
// 取舍说明:上传侧不写缓存、只在展示回填 —— 本缓存按 spec §4.4 归属 handler 包,
// 展示侧(ServeImage)与缓存同包,便于共享包级状态。spec §4.5.2 只要求
// 「展示先查缓存、未命中回填」。上传是低频操作,首次 GET 展示即回填,
// 上传侧不写缓存不影响正确性。
const (
	// imageCacheCap 是图片缓存条数上限。
	imageCacheCap = 256
	// imageCacheMaxBytes 是图片缓存总字节预算(128MB)。
	// 单张图最多 5MB(受上传 8MB multipart 上限约束),若不限总字节,
	// 256 条峰值可达 ~1.28GB,这里用字节预算把峰值内存收敛到 128MB。
	imageCacheMaxBytes = 128 << 20
)

type imageCacheEntry struct {
	data        []byte
	contentType string
}

var (
	imageCacheMu sync.RWMutex
	imageCache   = make(map[string]imageCacheEntry, imageCacheCap)
	// imageCacheBytes 统计当前缓存内容的总字节数,与 imageCache 同步增减。
	imageCacheBytes int
	// imageCacheOrder 记录插入序(FIFO),队头最旧;淘汰时据此逐条弹出最旧条目。
	imageCacheOrder []string
)

func getImageCache(name string) (imageCacheEntry, bool) {
	imageCacheMu.RLock()
	e, ok := imageCache[name]
	imageCacheMu.RUnlock()
	return e, ok
}

func putImageCache(name string, e imageCacheEntry) {
	imageCacheMu.Lock()
	defer imageCacheMu.Unlock()
	if _, exists := imageCache[name]; exists {
		return // 已缓存,内容不可变,无需覆盖
	}
	size := len(e.data)
	// 单张图超过整个字节预算的极端情况直接不缓存:否则淘汰循环清空所有旧条目
	// 后仍放不下,预算形同虚设。正常图片不会走到这里。
	if size > imageCacheMaxBytes {
		return
	}
	// 双约束淘汰:条数上限 + 总字节预算。imageCacheOrder 与 imageCache 同步维护,
	// 队头最旧;超限时逐条淘汰最旧条目,避免整体清空重建导致的热点误伤。
	for (len(imageCache) >= imageCacheCap || imageCacheBytes+size > imageCacheMaxBytes) && len(imageCacheOrder) > 0 {
		oldest := imageCacheOrder[0]
		imageCacheOrder = imageCacheOrder[1:]
		if old, ok := imageCache[oldest]; ok {
			imageCacheBytes -= len(old.data)
			delete(imageCache, oldest)
		}
	}
	imageCache[name] = e
	imageCacheBytes += size
	imageCacheOrder = append(imageCacheOrder, name)
}

// ServeImage 处理 /uploads/* 图片请求:文件名防目录穿越,先查进程内缓存,
// 未命中再查 tb_image;命中后回填缓存,后续请求免一次库查询。
// 用自定义 handler 替代 r.Static:文件缺失时记录 [warn],避免「资源丢了却静默 404」。
func ServeImage(c *gin.Context) {
	name := filepath.Base(c.Request.URL.Path) // 防目录穿越
	var data []byte
	var contentType string
	if e, ok := getImageCache(name); ok {
		data, contentType = e.data, e.contentType
	} else if d, ct, ok := service.GetImage(name); ok {
		data, contentType = d, ct
		putImageCache(name, imageCacheEntry{data: d, contentType: ct})
	} else {
		logger.Warnf("[static][告警] 缺失资源 %s (来自 %s)", c.Request.URL.Path, c.ClientIP())
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "资源不存在"})
		return
	}
	// 文件名即内容指纹:同名内容永不变,允许浏览器/Nginx 缓存一年。
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, contentType, data)
}
