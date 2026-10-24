package dao

import (
	"database/sql"
	"fmt"
	"time"

	"dining-system/infrastructure/conf"
)

// 时间列读取适配层。
//
// 背景:时间列在两种方言下的**物理类型不同**,驱动交回来的 Go 值也不同:
//
//	MySQL  —— DATETIME(DSN 默认带 parseTime=true) → driver 返回 time.Time
//	SQLite —— VARCHAR(32)(SQLite 没有真正的时间类型)→ driver 返回 string
//
// 业务层(service / handler / 前端 JSON)只认 conf.TimeLayout 的字符串,因此所有
// 「把时间列扫进字符串」的地方都通过 TS() 收口:无论驱动给的是 time.Time 还是
// string,落到结构体里的都是 'YYYY-MM-DD HH:MM:SS'。
//
// 为什么用「包一层 Scan 参数」而不是「自定义 Scanner 类型替换 sql.NullString」:
// 现有扫描写法是「先声明局部变量 → Scan → 再赋给结构体字段」,换类型要同时改声明、
// 赋值与判空三处,而包参数只改一个 token,下游的 nsPtr(x) / x.String / x.Valid
// 全部照旧 —— 改造面最小,回归风险最低。
//
// TS() 的入参用 any 而不是三套重载(Go 没有重载),因为扫描目标只有三种形态:
//
//	*string           —— 非空字段(空值落空串)
//	**string          —— 可空字段(空值落 nil,与 nsPtr(x) 语义一致)
//	*sql.NullString   —— 可空字段的可空载体(保留 Valid 语义)
//
// 顺带修掉一个坑:MySQL 在非严格模式下可能存有 '0000-00-00 00:00:00' 这类历史
// 零值,直接使用会得到无意义的「零年」字符串,这里统一折叠为空串/nil,与 SQLite
// 侧的 NULL 语义对齐。

// TS 包装时间列的扫描目标,兼容 MySQL(time.Time)与 SQLite(string)。
//
//	rows.Scan(&o.OrderID, TS(&o.CreateTime), TS(&o.PayTime))
func TS(dst any) sql.Scanner { return timeTarget{dst: dst} }

type timeTarget struct{ dst any }

func (t timeTarget) Scan(v any) error {
	s, isNil := timeValueToString(v)
	switch d := t.dst.(type) {
	case *string:
		if isNil {
			*d = ""
			return nil
		}
		*d = s
		return nil
	case **string:
		if isNil || s == "" {
			*d = nil
			return nil
		}
		val := s
		*d = &val
		return nil
	case *sql.NullString:
		if isNil {
			return d.Scan(nil)
		}
		return d.Scan(s)
	default:
		return fmt.Errorf("dao.TS: 不支持的时间列扫描目标 %T(仅支持 *string / **string / *sql.NullString)", t.dst)
	}
}

// timeValueToString 把驱动返回的时间值折算成 conf.TimeLayout 字符串。
// 第二个返回值为 true 表示「数据库里没有值」。
func timeValueToString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case time.Time:
		if x.IsZero() {
			return "", true
		}
		s := x.Format(conf.TimeLayout)
		if isZeroDate(s) {
			return "", true
		}
		return s, false
	case []byte:
		return normalizeDateString(string(x))
	case string:
		return normalizeDateString(x)
	default:
		// 未知类型不在这里猜:交给 Stringer 兜底,避免把错误吞成「没有值」。
		return fmt.Sprint(x), false
	}
}

// normalizeDateString 把数据库里的时间文本规整成 conf.TimeLayout:
// 空串与 MySQL 历史零值视为「没有值」;带 T/时区的 ISO 形态(外部导入数据)折算成本地时间文本。
func normalizeDateString(s string) (string, bool) {
	if s == "" || isZeroDate(s) {
		return "", true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		// 带 T/时区的输入要先折算成本地时间再格式化:ParseInLocation 对带
		// Z/偏移的串会保留原 zone,直接 Format 出来的是 UTC 墙钟时间,
		// 与注释承诺的「折算成本地时间文本」不符(差 8 小时)。
		return t.In(time.Local).Format(conf.TimeLayout), false
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); err == nil {
		return t.Format(conf.TimeLayout), false
	}
	return s, false
}

// isZeroDate MySQL 历史零值('0000-00-00 00:00:00' / '0000-00-00')。
func isZeroDate(s string) bool {
	switch s {
	case "0000-00-00 00:00:00", "0000-00-00", "0000-00-00T00:00:00Z", "0000-00-00T00:00:00+00:00":
		return true
	}
	return false
}
