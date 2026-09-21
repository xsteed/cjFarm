package handler

import (
	"dining-system/internal/logger"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 桌台 ============

func TableList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	keyword := c.Query("tableName")
	var where string
	var args []interface{}
	if keyword != "" {
		where = " WHERE del_flag='0' AND table_name LIKE ?"
		args = append(args, "%"+keyword+"%")
	} else {
		where = " WHERE del_flag='0'"
	}
	var total int
	store.DB.QueryRow(`SELECT COUNT(*) FROM tb_table`+where, args...).Scan(&total)

	rows, err := store.DB.Query(`SELECT `+store.TableCols+` FROM tb_table`+where+` ORDER BY sort_order, table_id LIMIT ? OFFSET ?`,
		append(args, pageSize, (pageNum-1)*pageSize)...)
	if err != nil {
		fail(c, err.Error())
		return
	}
	defer rows.Close()
	list := []model.Table{}
	for rows.Next() {
		if t, err := store.ScanTable(rows); err == nil {
			// 兜底:历史数据若缺稳定码则即时补发,保证二维码可生成。
			if t.TableCode == "" {
				if code, err := store.EnsureTableCode(t.TableID); err == nil {
					t.TableCode = code
				}
			}
			list = append(list, t)
		}
	}
	tableResult(c, total, list)
}

func TableSave(c *gin.Context) {
	var t model.Table
	if err := c.ShouldBindJSON(&t); err != nil {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(t.TableNo) == "" || strings.TrimSpace(t.TableName) == "" {
		fail(c, "请填写桌号和桌台名称")
		return
	}
	res, err := store.DB.Exec(`INSERT INTO tb_table(table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time)
		VALUES(?,?,?,?,?,?,?,?,?)`, t.TableNo, t.TableName, t.Capacity, t.Status, t.SortOrder, "0", store.NewUniqueTableCode(), store.Now(), store.Now())
	if err != nil {
		fail(c, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	// 兜底:若随机码插入冲突(唯一索引),重试补发,确保新桌台必有稳定码。
	if _, err := store.EnsureTableCode(int(id)); err != nil {
		logger.Warnf("EnsureTableCode(table %d): %v", id, err)
	}
	ok(c, gin.H{"tableId": id})
}

func TableUpdate(c *gin.Context) {
	var t model.Table
	if err := c.ShouldBindJSON(&t); err != nil || t.TableID == 0 {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(t.TableNo) == "" || strings.TrimSpace(t.TableName) == "" {
		fail(c, "请填写桌号和桌台名称")
		return
	}
	_, err := store.DB.Exec(`UPDATE tb_table SET table_no=?, table_name=?, capacity=?, status=?, sort_order=?, update_time=? WHERE table_id=?`,
		t.TableNo, t.TableName, t.Capacity, t.Status, t.SortOrder, store.Now(), t.TableID)
	if err != nil {
		fail(c, err.Error())
		return
	}
	okMsg(c, "修改成功")
}

func TableDelete(c *gin.Context) {
	id, okid := idParam(c)
	if !okid {
		return
	}
	store.DB.Exec(`UPDATE tb_table SET del_flag='1', update_time=? WHERE table_id=?`, store.Now(), id)
	okMsg(c, "删除成功")
}
