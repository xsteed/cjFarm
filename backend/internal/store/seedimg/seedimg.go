package seedimg

import "embed"

// 出厂种子图随包内嵌到后端二进制中。
//
// 为什么内嵌:图片存储已迁移到数据库,出厂图需要随后端二进制一起分发,
// 并在首次启动时写入 tb_image,避免发布包和线上目录之间再维护一份磁盘种子图。
// 设计详见 docs/design-docs/store/image-db-storage/spec.md。
//
// 为什么是独立子包:go:embed 只能嵌入本包目录下的文件,图片文件必须和声明它的
// Go 源文件放在同一包目录中;store 包通过 Files 读取这些内容。
//
//go:embed *.png *.jpg *.jpeg
var files embed.FS

var cachedFiles = loadFiles()

// Files 返回「文件名 -> 图片内容」的出厂种子图集合。
func Files() map[string][]byte {
	return cachedFiles
}

// Count 返回内嵌的出厂种子图数量。
func Count() int {
	return len(cachedFiles)
}

func loadFiles() map[string][]byte {
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	out := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		data, err := files.ReadFile(name)
		if err != nil {
			panic(err)
		}
		out[name] = data
	}
	return out
}
