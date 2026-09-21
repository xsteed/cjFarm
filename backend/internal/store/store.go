// Package store 负责数据库访问:连接、建表、种子数据与各类查询。
//
// 支持两种数据库后端,通过环境变量切换(详见 dialect.go):
//   - SQLite(默认):单文件、零外部依赖,适合单机/小规模部署;
//   - MySQL:适合多实例、高并发或已有 DBA 运维体系的场景。
//
// 依赖方向:store 只依赖 model,不依赖 service/handler/print。
package store

import (
	"database/sql"
	"os"
	"time"
)

// DB 全局数据库句柄,由 Init 初始化后供各查询函数使用。
// 两种方言共用同一类型,业务代码无需感知底层是 SQLite 还是 MySQL。
var DB *sql.DB

// WithTx 在事务中执行 fn,失败自动回滚。
func WithTx(fn func(tx *sql.Tx) error) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Now 返回当前时间字符串(数据库统一格式)。
func Now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// Getenv 返回环境变量值,为空时回退到默认值。
func Getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
