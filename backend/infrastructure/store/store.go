// Package store 负责数据库访问:连接、建表、种子数据与各类查询。
//
// 支持两种数据库后端,通过环境变量切换(详见 dialect.go):
//   - SQLite(默认):单文件、零外部依赖,适合单机/小规模部署;
//   - MySQL:适合多实例、高并发或已有 DBA 运维体系的场景。
//
// 依赖方向:store 依赖 po + conf(常量) + infra(env/logger/secret) + store/seedimg(种子图片),
// 不依赖 dto/service/handler/print。
package store

import (
	"database/sql"
	"strings"
	"time"

	"dining-system/infrastructure/conf"
)

// DB 全局数据库句柄,由 Init 初始化后供各查询函数使用。
// 两种方言共用同一类型,业务代码无需感知底层是 SQLite 还是 MySQL。
var DB *sql.DB

// likeEscapeChar LIKE 通配符的转义符。与 dao 层各模糊查询的 ESCAPE 子句一致。
const likeEscapeChar = `\`

// EscapeLike 转义用户输入里的 LIKE 通配符(% _ \)。
//
// 模糊查询的惯用写法是 LIKE '%'||输入||'%':输入里的 % 或 _ 不过转义就会
// 被当作通配符 —— 搜一个 % 匹配全表、搜 a_b 把 axb/ayb 都算进来,搜索结果
// 失真。不是 SQL 注入(值仍走参数绑定),但属于必须处理的输入卫生问题。
// 配合 SQL 里的 ESCAPE '\' 子句使用(SQLite 与 MySQL 均支持)。
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, likeEscapeChar, likeEscapeChar+likeEscapeChar)
	s = strings.ReplaceAll(s, "%", likeEscapeChar+"%")
	s = strings.ReplaceAll(s, "_", likeEscapeChar+"_")
	return s
}

// LikeContains 返回「包含 kw」的转义后模式串(两侧带 %)。
func LikeContains(kw string) string {
	return "%" + EscapeLike(kw) + "%"
}

// WithTx 在事务中执行 fn,失败自动回滚。
func WithTx(fn func(tx *sql.Tx) error) (err error) {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	// fn 内 panic 时必须回滚:否则该连接携带未提交事务与行锁,一直占用到连接超时,
	// 后续对同一行的写会全部卡住(下单/支付/退款全都走这个函数,影响面很大)。
	// 回滚后原样重新抛出,交由上层的 Gin Recovery 统一处理。
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Now 返回当前时间字符串(数据库统一格式)。
func Now() string {
	return time.Now().Format(conf.TimeLayout)
}
