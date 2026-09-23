package dto

import "dining-system/internal/po"

// Printer 对应 tb_printer 表(API 出参)。CategoryIDList/OnlineStatus/CategoryNames 为运行时补算,不入库。
type Printer struct {
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	PrinterType int    `json:"printerType"` // 1=厨房单 2=食客小票
	Provider    string `json:"provider"`    // tcp=网络直连 / feie=飞鹅云
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	FeieSN      string `json:"feieSn"`     // 飞鹅打印机编号(机身标签上的 SN)
	PaperWidth  int    `json:"paperWidth"` // 32=58mm 48=80mm
	Copies      int    `json:"copies"`     // 打印份数(1-5)
	CategoryIDs string `json:"categoryIds"`
	Status      int    `json:"status"` // 0=停用 1=启用
	DelFlag     string `json:"delFlag"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`

	// 以下为运行时补算,不入库。
	CategoryIDList []int   `json:"categoryIdList,omitempty"`
	OnlineStatus   string  `json:"onlineStatus,omitempty"` // 飞鹅在线状态文本(仅 provider=feie 且主动查询时填充)
	CategoryNames  *string `json:"categoryNames,omitempty"`
}

// FromPrinter 将持久化对象与运行时补算结果组装为 API 出参。
func FromPrinter(p po.Printer, categoryIDList []int, onlineStatus string, categoryNames *string) Printer {
	return Printer{
		PrinterID:      p.PrinterID,
		PrinterName:    p.PrinterName,
		PrinterType:    p.PrinterType,
		Provider:       p.Provider,
		IP:             p.IP,
		Port:           p.Port,
		FeieSN:         p.FeieSN,
		PaperWidth:     p.PaperWidth,
		Copies:         p.Copies,
		CategoryIDs:    p.CategoryIDs,
		Status:         p.Status,
		DelFlag:        p.DelFlag,
		CreateTime:     p.CreateTime,
		UpdateTime:     p.UpdateTime,
		CategoryIDList: categoryIDList,
		OnlineStatus:   onlineStatus,
		CategoryNames:  categoryNames,
	}
}

// ToPO 将 API 出参转回持久化对象,CategoryIDList/OnlineStatus/CategoryNames 不入库。
func (p Printer) ToPO() po.Printer {
	return po.Printer{
		PrinterID:   p.PrinterID,
		PrinterName: p.PrinterName,
		PrinterType: p.PrinterType,
		Provider:    p.Provider,
		IP:          p.IP,
		Port:        p.Port,
		FeieSN:      p.FeieSN,
		PaperWidth:  p.PaperWidth,
		Copies:      p.Copies,
		CategoryIDs: p.CategoryIDs,
		Status:      p.Status,
		DelFlag:     p.DelFlag,
		CreateTime:  p.CreateTime,
		UpdateTime:  p.UpdateTime,
	}
}
