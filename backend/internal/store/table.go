package store

import (
	"crypto/rand"
	"database/sql"
	"math/big"
	"strings"

	"dining-system/internal/model"
)

// TableCols 桌台表完整列(与 ScanTable 顺序一一对应)。
const TableCols = `table_id, table_no, table_name, capacity, status, sort_order, del_flag,
	create_by, create_time, update_by, update_time, remark, table_code`

// ScanTable 扫描一行桌台记录。
func ScanTable(rows interface{ Scan(...interface{}) error }) (model.Table, error) {
	var t model.Table
	err := rows.Scan(&t.TableID, &t.TableNo, &t.TableName, &t.Capacity, &t.Status, &t.SortOrder, &t.DelFlag,
		&t.CreateBy, &t.CreateTime, &t.UpdateBy, &t.UpdateTime, &t.Remark, &t.TableCode)
	return t, err
}

// tableCodeAlphabet 去掉易混字符(0/O、1/I/L),便于人工核对与口述。
const tableCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewTableCode 生成 8 位随机桌台码(约 32^8 ≈ 1.1e12 种组合,不可枚举)。
func NewTableCode() string {
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(tableCodeAlphabet))))
		if err != nil {
			// 随机源异常时退化为顺序字符,极不可能发生。
			b[i] = tableCodeAlphabet[i%len(tableCodeAlphabet)]
			continue
		}
		b[i] = tableCodeAlphabet[n.Int64()]
	}
	return string(b)
}

// NewUniqueTableCode 生成一个当前库中尚未被占用的桌台码。
//
// SQLite 有「部分唯一索引」(idx_table_code,仅约束非空桌台码)兜底,
// 但 MySQL 不支持部分索引,该索引会降级为普通索引,因此这里统一做一次
// 存在性校验,保证两种后端下的唯一性语义一致。
func NewUniqueTableCode() string {
	for i := 0; i < 16; i++ {
		code := NewTableCode()
		var n int
		if err := DB.QueryRow(`SELECT COUNT(*) FROM tb_table WHERE table_code=?`, code).Scan(&n); err != nil {
			// 查询异常时不阻塞业务,交由索引(若存在)兜底。
			return code
		}
		if n == 0 {
			return code
		}
	}
	return NewTableCode()
}

// EnsureTableCode 为指定桌台补发稳定码(仅当当前为空时写入),返回最终码。
func EnsureTableCode(tableID int) (string, error) {
	var cur string
	err := DB.QueryRow(`SELECT COALESCE(table_code,'') FROM tb_table WHERE table_id=?`, tableID).Scan(&cur)
	if err != nil {
		return "", err
	}
	if cur != "" {
		return cur, nil
	}
	for i := 0; i < 8; i++ {
		code := NewUniqueTableCode()
		res, err := DB.Exec(`UPDATE tb_table SET table_code=? WHERE table_id=? AND COALESCE(table_code,'')=''`, code, tableID)
		if err != nil {
			// 唯一索引冲突(极端巧合)则重试
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// 并发下已被其他请求写入,回读
			DB.QueryRow(`SELECT COALESCE(table_code,'') FROM tb_table WHERE table_id=?`, tableID).Scan(&cur)
			return cur, nil
		}
		return code, nil
	}
	return "", sql.ErrNoRows
}

// GetTableByRef 按「桌台码」或「数字桌台ID」解析桌台,兼容老二维码。
// 先按桌台码解析(大小写不敏感),失败再按纯数字 ID 解析,
// 这样既支持新印制的随机码,也不影响历史已印制的 /order/1 链接。
func GetTableByRef(ref string) (model.Table, error) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		row := DB.QueryRow(`SELECT `+TableCols+` FROM tb_table WHERE table_code=? AND del_flag='0'`, strings.ToUpper(ref))
		if t, err := ScanTable(row); err == nil {
			return t, nil
		}
	}
	id, ok := parseRefID(ref)
	if !ok {
		return model.Table{}, sql.ErrNoRows
	}
	return ScanTable(DB.QueryRow(`SELECT `+TableCols+` FROM tb_table WHERE table_id=? AND del_flag='0'`, id))
}

// parseRefID 仅接受纯数字的正整数,拒绝 "1abc" / "-1" / 空串 / 超长串。
func parseRefID(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}
