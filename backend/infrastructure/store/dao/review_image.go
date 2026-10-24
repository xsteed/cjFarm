package dao

import (
	"database/sql"
	"strings"

	"dining-system/infrastructure/store"
)

// ==================== 评价图片(独立于 tb_image 的用户内容) ====================

// SaveReviewImage 落一张评价图片(review_id=0 待绑定)。
// 文件名唯一索引兜底并发同名(概率可忽略,撞上由上层提示重试)。
func SaveReviewImage(name, contentType string, data []byte) error {
	_, err := store.DB.Exec(`INSERT INTO tb_review_image(img_name, content_type, img_data, file_size, create_time)
		VALUES(?,?,?,?,?)`, name, contentType, data, len(data), store.Now())
	return err
}

// GetReviewImage 按文件名读图片内容(展示链路 /uploads/{name} 的第一级来源,
// 未命中再回落 tb_image —— 见 service.GetImage)。
func GetReviewImage(name string) (data []byte, contentType string, ok bool) {
	var ct sql.NullString
	err := store.DB.QueryRow(`SELECT img_data, content_type FROM tb_review_image WHERE img_name=?`, name).
		Scan(&data, &ct)
	if err != nil {
		return nil, "", false
	}
	return data, ct.String, true
}

// BindReviewImagesTx 事务内把「未绑定」的图片绑到评价上,返回实际绑定张数。
//
// 只认 review_id=0 的行:已被其它评价绑定的图名不再改绑 —— img_name 虽不可预测,
// 提交体仍可能引用他人上传的图名,这里按「先到先得」挡住。
func BindReviewImagesTx(tx *sql.Tx, reviewID int64, names []string) (int64, error) {
	if len(names) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(names)), ",")
	// 占位符顺序与 SQL 一致:SET review_id, SET update_time, IN(...)。
	args := make([]interface{}, 0, len(names)+2)
	args = append(args, reviewID, store.Now())
	for _, n := range names {
		args = append(args, n)
	}
	res, err := tx.Exec(`UPDATE tb_review_image SET review_id=?, update_time=?
		WHERE review_id=0 AND img_name IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListReviewImagesByReviewIDs 批量取多条评价的图片名(管理端列表展示),
// 返回 review_id → [img_name...]。
func ListReviewImagesByReviewIDs(reviewIDs []int) map[int][]string {
	out := map[int][]string{}
	if len(reviewIDs) == 0 {
		return out
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(reviewIDs)), ",")
	args := make([]interface{}, 0, len(reviewIDs))
	for _, id := range reviewIDs {
		args = append(args, id)
	}
	rows, err := store.DB.Query(`SELECT review_id, img_name FROM tb_review_image
		WHERE review_id IN (`+placeholders+`) ORDER BY image_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var rid int
		var name string
		if err := rows.Scan(&rid, &name); err == nil {
			out[rid] = append(out[rid], name)
		}
	}
	return out
}

// DeleteReviewImagesByReview 删除某评价的全部图片(管理端删评价级联)。
func DeleteReviewImagesByReview(reviewID int) error {
	_, err := store.DB.Exec(`DELETE FROM tb_review_image WHERE review_id=?`, reviewID)
	return err
}

// CleanOrphanReviewImages 清理「已上传但一直未绑定评价」的图片:
// 顾客上传后放弃提交/提交失败留下的孤儿。cutoff 之前的 review_id=0 行整行删除
// (BLOB 大对象,不留软删除)。返回清理张数。
func CleanOrphanReviewImages(cutoff string) (int64, error) {
	res, err := store.DB.Exec(`DELETE FROM tb_review_image WHERE review_id=0 AND create_time<?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
