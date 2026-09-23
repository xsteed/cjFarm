package dao

import (
	"database/sql"
	"errors"

	"dining-system/internal/store"
)

// SaveImage 保存一次新上传图片。
//
// 图片表的不变量是「文件名一旦写入即不可变」:上传路径理论上会生成全局唯一文件名,
// 若仍撞上唯一索引,说明调用方拿到了已存在名字,必须把冲突显式暴露给上层重试,
// 不能静默返回库里的旧内容。
func SaveImage(name, contentType string, data []byte) error {
	inserted, err := store.InsertImage(name, contentType, data)
	if err != nil {
		return err
	}
	if !inserted {
		return errors.New("文件名已存在，请重试")
	}
	return nil
}

// GetImage 按业务 URL 中的文件名读取图片内容。
//
// 业务表仍保存 /uploads/<name> 形式的路径,运行时只以 <name> 查询 tb_image,不再回退
// 读取磁盘,避免数据库与磁盘双数据源出现歧义。
func GetImage(name string) (data []byte, contentType string, ok bool) {
	// content_type 列允许 NULL(存量/手工改库可能产生),NULL 扫进 string 会直接报错
	// 把整行误判为「未命中」;用 NullString 归一为空串,交给 serveUpload 的兜底链处理。
	var ct sql.NullString
	if err := store.DB.QueryRow(`SELECT img_data, content_type FROM tb_image WHERE img_name=?`, name).Scan(&data, &ct); err != nil {
		return nil, "", false
	}
	return data, ct.String, true
}
