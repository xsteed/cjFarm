package handler

import (
	"io"

	"github.com/gin-gonic/gin"

	"dining-system/internal/service"
)

// ============ 上传 ============

func Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		fail(c, "文件不能为空")
		return
	}
	// 大小限制 5MB
	if file.Size > service.MaxImageBytes {
		fail(c, "文件大小不能超过 5MB")
		return
	}

	// 内容校验前先把文件读入内存:file.Size 只是 multipart 头声明,可伪造,
	// 不能作为唯一的 5MB 防线(否则攻击者可声明小文件、上传超大实体拖垮内存)。
	src, err := file.Open()
	if err != nil {
		fail(c, "文件读取失败")
		return
	}
	data, err := io.ReadAll(io.LimitReader(src, service.MaxImageBytes+1))
	src.Close()
	if err != nil || len(data) == 0 {
		fail(c, "文件读取失败")
		return
	}

	url, err := service.SaveUpload(file.Filename, data)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"fileName": file.Filename, "url": url})
}
