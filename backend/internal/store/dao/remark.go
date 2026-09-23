package dao

import (
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// 备注仓储封装备注明细相关数据库操作。

// ListRemarks 查询未删除的备注列表。
func ListRemarks() ([]po.Remark, error) {
	list := []po.Remark{}
	rows, err := store.DB.Query(`SELECT remark_id, option_name, sort_order, del_flag, create_time, update_time
		FROM tb_remark WHERE del_flag='0' ORDER BY sort_order, remark_id`)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var r po.Remark
		rows.Scan(&r.RemarkID, &r.OptionName, &r.SortOrder, &r.DelFlag, &r.CreateTime, &r.UpdateTime)
		list = append(list, r)
	}
	return list, nil
}

// InsertRemark 新增备注并返回自增 ID。
func InsertRemark(r po.Remark) (int64, error) {
	res, err := store.DB.Exec(`INSERT INTO tb_remark(option_name, sort_order, del_flag, create_time, update_time)
		VALUES(?,?,?,?,?)`, r.OptionName, r.SortOrder, "0", store.Now(), store.Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// UpdateRemark 更新备注信息。
func UpdateRemark(r po.Remark) error {
	_, err := store.DB.Exec(`UPDATE tb_remark SET option_name=?, sort_order=?, update_time=? WHERE remark_id=?`,
		r.OptionName, r.SortOrder, store.Now(), r.RemarkID)
	return err
}

// DeleteRemark 软删除备注。
func DeleteRemark(id int) error {
	_, err := store.DB.Exec(`UPDATE tb_remark SET del_flag='1', update_time=? WHERE remark_id=?`, store.Now(), id)
	return err
}
