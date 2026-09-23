package dao

import (
	"path/filepath"
	"reflect"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// logDaoInitDB 初始化打印日志测试用的临时库。
func logDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "printlog.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// logDaoInsert 写入一条打印日志,返回日志 ID。
func logDaoInsert(t *testing.T, l po.PrintLog) int {
	t.Helper()
	id, err := InsertPrintLogReturningID(l)
	if err != nil {
		t.Fatalf("写入打印日志失败: %v", err)
	}
	return id
}

func TestPrintLogInsertAndLoad(t *testing.T) {
	logDaoInitDB(t)

	// 不返回 ID 的写入分支。
	if err := InsertPrintLog(po.PrintLog{OrderID: 1, OrderNo: "NO1", Status: po.PrintStatusFailed, Detail: "no-id"}); err != nil {
		t.Fatalf("写入打印日志失败: %v", err)
	}

	id := logDaoInsert(t, po.PrintLog{
		OrderID: 2, OrderNo: "NO2", TableNo: "01", TableName: "大厅01桌",
		PrinterID: 1, PrinterName: "测试打印机", PrinterType: po.PrinterTypeKitchen, Provider: po.PrinterProviderAgent,
		DocType: po.PrintDocKitchen, Copies: 2, Status: po.PrintStatusQueued, RemoteID: "remote-1",
		Detail: "排队中", TriggerBy: po.PrintTriggerOrder, Operator: "boss", CostMs: 0, CreateTime: store.Now(),
	})
	if id <= 0 {
		t.Fatalf("应返回自增日志 ID, got %d", id)
	}

	got, err := LoadPrintLog(id)
	if err != nil {
		t.Fatalf("回读打印日志失败: %v", err)
	}
	if got.OrderNo != "NO2" || got.DocType != po.PrintDocKitchen || got.Copies != 2 || got.Status != po.PrintStatusQueued {
		t.Fatalf("打印日志回读异常: %+v", got)
	}
	if _, err := LoadPrintLog(999999); err == nil {
		t.Fatal("查询不存在的日志应报错")
	}
}

func TestLoadPrinter(t *testing.T) {
	logDaoInitDB(t)
	// 种子打印机 id=1。
	p, err := LoadPrinter(1)
	if err != nil {
		t.Fatalf("回读打印机失败: %v", err)
	}
	if p.PrinterID != 1 || p.Provider != po.PrinterProviderTCP {
		t.Fatalf("打印机回读异常: %+v", p)
	}
	if _, err := LoadPrinter(999999); err == nil {
		t.Fatal("查询不存在的打印机应报错")
	}
}

func TestListPrintLogsFilters(t *testing.T) {
	logDaoInitDB(t)

	logDaoInsert(t, po.PrintLog{OrderNo: "L-0001", DocType: po.PrintDocKitchen, Provider: po.PrinterProviderAgent, PrinterID: 1, Status: po.PrintStatusQueued, CreateTime: store.Now()})
	logDaoInsert(t, po.PrintLog{OrderNo: "L-0002", DocType: po.PrintDocGuest, Provider: po.PrinterProviderTCP, PrinterID: 2, Status: po.PrintStatusSuccess, CreateTime: store.Now()})
	logDaoInsert(t, po.PrintLog{OrderNo: "X-0003", DocType: po.PrintDocKitchen, Provider: po.PrinterProviderAgent, PrinterID: 1, Status: po.PrintStatusQueued, CreateTime: store.Now()})

	total, list, err := ListPrintLogs(PrintLogQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("查询打印日志失败: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("应查到 3 条日志, got total=%d len=%d", total, len(list))
	}

	queued := po.PrintStatusQueued
	_, list, err = ListPrintLogs(PrintLogQuery{Status: &queued}, 1, 100)
	if err != nil || len(list) != 2 {
		t.Fatalf("按状态筛选应命中 2 条, got %d err=%v", len(list), err)
	}

	_, list, err = ListPrintLogs(PrintLogQuery{DocType: po.PrintDocGuest}, 1, 100)
	if err != nil || len(list) != 1 || list[0].OrderNo != "L-0002" {
		t.Fatalf("按单据类型筛选失败: %v (err=%v)", list, err)
	}

	_, list, err = ListPrintLogs(PrintLogQuery{Provider: po.PrinterProviderTCP}, 1, 100)
	if err != nil || len(list) != 1 || list[0].OrderNo != "L-0002" {
		t.Fatalf("按接入方式筛选失败: %v (err=%v)", list, err)
	}

	pid := 1
	_, list, err = ListPrintLogs(PrintLogQuery{PrinterID: &pid}, 1, 100)
	if err != nil || len(list) != 2 {
		t.Fatalf("按打印机筛选失败: %v (err=%v)", list, err)
	}

	_, list, err = ListPrintLogs(PrintLogQuery{OrderNo: "L-000"}, 1, 100)
	if err != nil || len(list) != 2 {
		t.Fatalf("按单号模糊筛选应命中 2 条, got %d err=%v", len(list), err)
	}

	// 分页。
	total, page1, err := ListPrintLogs(PrintLogQuery{}, 1, 2)
	if err != nil || total != 3 || len(page1) != 2 {
		t.Fatalf("分页异常: total=%d len=%d err=%v", total, len(page1), err)
	}
}

func TestParseIDList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []int
	}{
		{"空串", "", nil},
		{"仅空白", "  ,  ", []int{}},
		{"正常", "1,2,3", []int{1, 2, 3}},
		{"带空白", " 1 , 2 ", []int{1, 2}},
		{"非法项", "1,a,3", []int{1, 3}},
		{"零与负数", "0,-1,2", []int{2}},
		{"前导零", "007", []int{7}},
		{"空项", "1,,3", []int{1, 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseIDList(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ParseIDList(%q)=%v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestJoinIDList(t *testing.T) {
	if got := JoinIDList([]int{1, 2, 2, 3, 0, -1, 1}); got != "1,2,3" {
		t.Fatalf("JoinIDList 应去重并忽略非正数, got %q", got)
	}
	if got := JoinIDList(nil); got != "" {
		t.Fatalf("空列表应返回空串, got %q", got)
	}
}

func TestAttachItemCategories(t *testing.T) {
	logDaoInitDB(t)

	if got := AttachItemCategories(nil); got != nil {
		t.Fatalf("空明细应返回 nil, got %v", got)
	}

	// 无 dish_id 的明细:直接回填,分类为 0。
	noDish := AttachItemCategories([]po.OrderItem{{DishName: "无菜品"}})
	if len(noDish) != 1 || noDish[0].CategoryID != 0 {
		t.Fatalf("无菜品明细应返回分类 0, got %+v", noDish)
	}

	// 有 dish_id:按菜品补齐分类。
	rows := AttachItemCategories([]po.OrderItem{
		{DishID: 1, DishName: "凉拌青瓜"},
		{DishID: 999999, DishName: "不存在"},
		{DishID: 1, DishName: "重复菜品"},
	})
	if len(rows) != 3 {
		t.Fatalf("应返回 3 行, got %d", len(rows))
	}
	if rows[0].CategoryID != 1 {
		t.Fatalf("菜品 1 应命中分类 1, got %d", rows[0].CategoryID)
	}
	if rows[1].CategoryID != 0 {
		t.Fatalf("不存在菜品分类应为 0, got %d", rows[1].CategoryID)
	}
}
