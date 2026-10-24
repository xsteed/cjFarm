package po

// 评价图片相关常量。
const (
	// ReviewImagesMax 单条评价最多几张图。
	ReviewImagesMax = 3
	// ReviewImageMaxBytes 单张图片字节上限(顾客上传,比商家资源严得多)。
	ReviewImageMaxBytes = 5 * 1024 * 1024
)

// ReviewImage 对应 tb_review_image 表:顾客评价上传的图片(独立于商家资产的 tb_image)。
//
// 为什么单独一张表:评价图是用户内容,生命周期(级联删除/孤儿清理)与合规属性
// 和菜品图完全不同;独立成表才能区分业务、并在迁对象存储时整表筛选搬迁。
// 图片本体仍以 BLOB 入库(与 tb_image 同一运行时模型,展示统一走 /uploads/{name})。
type ReviewImage struct {
	ImageID     int
	ReviewID    int // 0 = 已上传未绑定评价(待提交或孤儿)
	ImgName     string
	ContentType string
	FileSize    int
	CreateTime  string
}
