package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"dining-system/infra/logger"
	"dining-system/internal/store/seedimg"
)

// ImageMIME 按文件扩展名返回系统允许入库的图片 MIME。
//
// 这里刻意只做确定性的扩展名白名单映射,不做内容探测:上传链路已经负责图片真实性
// 校验,存量磁盘导入也必须沿用同一张白名单,避免同一文件在不同环境/库之间得到不同的
// Content-Type。设计约束见 docs/design-docs/store/image-db-storage/spec.md §4.5.1。
//
// 保留在根包(而非 dao):ImportDiskImages / EnsureSeedImages 这两个启动引导函数
// 需要它,而 store 根包不得 import dao。
func ImageMIME(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return ""
	}
}

// InsertImage 向 tb_image 写入一条图片记录(冲突即忽略),返回是否真的插入。
//
// 图片表的不变量是「文件名一旦写入即不可变」:插入冲突时 inserted=false,
// 由调用方决定是显式报错(SaveImage)还是静默成功(saveImageIgnore)。
// 供 dao 包与启动引导共用,因此导出。
func InsertImage(name, contentType string, data []byte) (bool, error) {
	stmt := InsertIgnoreInto("tb_image", "img_name", "content_type", "img_data", "file_size", "create_time")
	res, err := DB.Exec(stmt, name, contentType, data, len(data), Now())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// saveImageIgnore 幂等补齐图片内容,唯一键冲突时保持已有内容并静默成功。
//
// 该 helper 只给启动导入和出厂图补缺使用:二者都遵循「首个写入者胜」的权威顺序,
// 绝不覆盖库中已有同名图片,从而避免升级时改变商户已经可见的图片内容。
func saveImageIgnore(name, contentType string, data []byte) error {
	_, err := InsertImage(name, contentType, data)
	return err
}

// ImportDiskImages 将旧上传目录中的存量图片导入 tb_image。
//
// 旧磁盘目录只作为升级时的一次性补齐源:目录不存在不阻断启动;同名同大小视为已导入;
// 同名但大小不同只告警且以库为准不覆盖,维护「记录一旦写入不更新」的不变量。
func ImportDiskImages(dir string) (imported, skipped, mismatched int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Infof("[image] 无存量上传目录，跳过导入")
		} else {
			logger.Warnf("[image][告警] 读取存量上传目录 %s 失败: %v", dir, err)
		}
		return 0, 0, 0
	}

	for _, entry := range entries {
		if entry.IsDir() {
			logger.Infof("[image] 跳过子目录 %s", entry.Name())
			continue
		}
		name := entry.Name()
		// img_name 上限 128:MySQL 的 INSERT IGNORE 会把超长值截断入库且不报错,
		// 原名将永远查不到、每次重启都会重复「导入」。超长名字直接跳过并告警。
		if len(name) > 128 {
			logger.Warnf("[image][告警] 磁盘文件 %s 文件名超过 128 字节,跳过导入", name)
			continue
		}
		mime := ImageMIME(filepath.Ext(name))
		if mime == "" {
			logger.Infof("[image] 跳过非白名单图片文件 %s", name)
			continue
		}

		info, err := entry.Info()
		if err != nil {
			logger.Warnf("[image][告警] 读取磁盘文件 %s 信息失败: %v", name, err)
			continue
		}
		diskSize := info.Size()
		// 与上传链路对齐 5MB 上限:UPLOAD_DIR 不是可信数据源,误放入超大白名单扩展名
		// 文件(如把视频改名 .png)会把启动内存瞬间吃满;超限跳过并告警,交人工核对。
		if diskSize > 5<<20 {
			logger.Warnf("[image][告警] 磁盘文件 %s 超过 5MB 上限,跳过导入: %d 字节", name, diskSize)
			continue
		}

		var dbSize int64
		err = DB.QueryRow(`SELECT file_size FROM tb_image WHERE img_name=?`, name).Scan(&dbSize)
		switch {
		case err == nil:
			if dbSize == diskSize {
				skipped++
			} else {
				mismatched++
				logger.Warnf("[image][告警] 磁盘文件 %s 与库中同名记录大小不一致(库 %d / 盘 %d 字节)，以库为准不覆盖，请人工核对", name, dbSize, diskSize)
			}
			continue
		case errors.Is(err, sql.ErrNoRows):
			// 继续读取文件并尝试插入。
		default:
			logger.Warnf("[image][告警] 查询图片 %s 是否已入库失败: %v", name, err)
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			logger.Warnf("[image][告警] 读取磁盘文件 %s 失败: %v", name, err)
			continue
		}
		if err := saveImageIgnore(name, mime, data); err != nil {
			logger.Warnf("[image][告警] 导入磁盘文件 %s 失败: %v", name, err)
			continue
		}
		imported++
	}

	logger.Infof("[image] 磁盘图片导入完成: 导入 %d 张, 跳过 %d 张, 不一致 %d 张", imported, skipped, mismatched)
	return imported, skipped, mismatched
}

// EnsureSeedImages 将随二进制内嵌的出厂图片补齐到 tb_image。
//
// 它只补缺、从不覆盖:在升级场景下 ImportDiskImages 会先把商户磁盘上的权威内容写库,
// 出厂图随后只填真正缺失的名字,保证老部署中手工替换过的同名图片不被改回默认图。
func EnsureSeedImages() (inserted int) {
	var existed, failed int
	for name, data := range seedimg.Files() {
		var exists int
		err := DB.QueryRow(`SELECT 1 FROM tb_image WHERE img_name=? LIMIT 1`, name).Scan(&exists)
		if err == nil {
			existed++
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			failed++
			logger.Warnf("[image] 查询出厂图片 %s 是否已存在失败: %v", name, err)
			continue
		}
		if err := saveImageIgnore(name, ImageMIME(filepath.Ext(name)), data); err != nil {
			failed++
			logger.Warnf("[image] 写入出厂图片 %s 失败: %v", name, err)
			continue
		}
		inserted++
	}
	logger.Infof("[image] 出厂图片初始化完成: 新增 %d 张, 已存在 %d 张, 失败 %d 张", inserted, existed, failed)
	return inserted
}

// AuditDishImages 审计菜品图片引用是否都能在 tb_image 中找到内容。
//
// 运行时展示已迁移为读数据库,因此启动审计也必须查图片表而不是查磁盘;否则会把
// 「磁盘存在但数据库缺失」误判为可用,实际顾客端仍会 404。
func AuditDishImages() (missing []string) {
	rows, err := DB.Query(`SELECT dish_image FROM tb_dish WHERE dish_image IS NOT NULL AND dish_image <> '' AND del_flag='0'`)
	if err != nil {
		logger.Warnf("[static][告警] 读取菜品图片失败: %v", err)
		return nil
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var img string
		if err := rows.Scan(&img); err != nil {
			logger.Warnf("[static][告警] 读取菜品图片行失败: %v", err)
			continue
		}
		n++
		name := filepath.Base(strings.TrimPrefix(img, UploadURLPrefix))
		var exists int
		err := DB.QueryRow(`SELECT 1 FROM tb_image WHERE img_name=? LIMIT 1`, name).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			missing = append(missing, name)
		} else if err != nil {
			logger.Warnf("[static][告警] 查询菜品图片 %s 失败: %v", name, err)
			missing = append(missing, name)
		}
	}
	if err := rows.Err(); err != nil {
		logger.Warnf("[static][告警] 遍历菜品图片失败: %v", err)
	}

	if len(missing) == 0 {
		logger.Infof("[static] 菜品图片审计通过: %d 张图片均在库中", n)
		return nil
	}
	logger.Warnf("[static][告警] 发现 %d 张菜品图片缺失(库里有引用,图片表无内容): %s",
		len(missing), strings.Join(missing, ", "))
	return missing
}
