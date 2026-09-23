package service

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// menuInitDB 初始化 SQLite 临时库并清空菜单相关种子,保证断言精确。
func menuInitDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DRIVER", string(store.DialectSQLite))
	t.Setenv("DB_DSN", "")
	t.Setenv("UPLOAD_DIR", filepath.Join(t.TempDir(), "missing-uploads"))
	store.Init(filepath.Join(t.TempDir(), "menu.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
	for _, stmt := range []string{
		`DELETE FROM tb_spec`, `DELETE FROM tb_dish`, `DELETE FROM tb_category`, `DELETE FROM tb_remark`,
	} {
		if _, err := store.DB.Exec(stmt); err != nil {
			t.Fatalf("清空表失败(%s): %v", stmt, err)
		}
	}
}

// menuSeedCategory 通过 dao 播种一个分类,返回分类 ID。
func menuSeedCategory(t *testing.T, name string) int {
	t.Helper()
	id, err := dao.InsertCategory(po.Category{CategoryName: name})
	if err != nil {
		t.Fatalf("播种分类失败: %v", err)
	}
	return int(id)
}

// menuSeedDish 通过 dao 播种一道菜品+单个规格,返回菜品 ID。
func menuSeedDish(t *testing.T, catID int, name string, status int, priceCents int64, specName string) int {
	t.Helper()
	id, err := dao.CreateDish(po.Dish{CategoryID: catID, DishName: name, Status: status, SortOrder: 1},
		[]po.Spec{{SpecName: specName, Price: priceCents}})
	if err != nil {
		t.Fatalf("播种菜品失败: %v", err)
	}
	return int(id)
}

// TestDishList 校验菜品分页查询、规格批量补齐与筛选。
func TestDishList(t *testing.T) {
	menuInitDB(t)
	cat1 := menuSeedCategory(t, "凉菜")
	cat2 := menuSeedCategory(t, "热菜")
	menuSeedDish(t, cat1, "拍黄瓜", 1, 1000, "份")
	menuSeedDish(t, cat2, "红烧肉", 1, 2000, "大份")

	total, list, err := DishList("", "", 1, 10)
	if err != nil {
		t.Fatalf("DishList 失败: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(list))
	}
	for _, d := range list {
		if len(d.Specs) != 1 {
			t.Fatalf("菜品 %q 规格数=%d, want 1", d.DishName, len(d.Specs))
		}
	}

	// 名称筛选。
	total, list, err = DishList("红烧", "", 1, 10)
	if err != nil || total != 1 || list[0].DishName != "红烧肉" {
		t.Fatalf("名称筛选异常: total=%d list=%+v err=%v", total, list, err)
	}
	// 分类筛选。
	total, list, err = DishList("", strconv.Itoa(cat2), 1, 10)
	if err != nil || total != 1 || list[0].CategoryName != "热菜" {
		t.Fatalf("分类筛选异常: total=%d list=%+v err=%v", total, list, err)
	}
}

// TestDishGet 校验单道菜品查询与规格加载。
func TestDishGet(t *testing.T) {
	menuInitDB(t)
	cat := menuSeedCategory(t, "特色菜")
	id := menuSeedDish(t, cat, "五指毛桃鸡", 1, 9800, "只")

	got, err := DishGet(id)
	if err != nil {
		t.Fatalf("DishGet 失败: %v", err)
	}
	if got.DishName != "五指毛桃鸡" || got.CategoryName != "特色菜" {
		t.Fatalf("DishGet 结果异常: %+v", got)
	}
	if len(got.Specs) != 1 || got.Specs[0].SpecName != "只" {
		t.Fatalf("规格加载异常: %+v", got.Specs)
	}
	if math.Abs(got.Specs[0].Price-98.0) > 1e-6 {
		t.Fatalf("规格价格 = %v, want 98", got.Specs[0].Price)
	}

	if _, err := DishGet(99999); err == nil {
		t.Fatal("不存在的菜品应返回错误")
	}
}

// TestSaveDish 校验新增菜品的参数校验与规格落库。
func TestSaveDish(t *testing.T) {
	menuInitDB(t)
	cat := menuSeedCategory(t, "分类")

	if _, err := SaveDish(dto.Dish{DishName: "无分类"}); err == nil || !strings.Contains(err.Error(), "请选择分类并填写菜品名称") {
		t.Fatalf("缺分类应报错, got %v", err)
	}
	if _, err := SaveDish(dto.Dish{CategoryID: cat, DishName: "  "}); err == nil {
		t.Fatal("空菜名应报错")
	}
	if _, err := SaveDish(dto.Dish{CategoryID: cat, DishName: "无规格"}); err == nil || !strings.Contains(err.Error(), "请至少添加一个规格") {
		t.Fatalf("缺规格应报错, got %v", err)
	}

	id, err := SaveDish(dto.Dish{CategoryID: cat, DishName: "新菜", Specs: []dto.Spec{{SpecName: "份", Price: 12.5}}})
	if err != nil {
		t.Fatalf("SaveDish 失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("SaveDish 应返回正数 ID, got %d", id)
	}
	got, err := DishGet(int(id))
	if err != nil {
		t.Fatalf("DishGet 失败: %v", err)
	}
	if len(got.Specs) != 1 || got.Specs[0].SpecName != "份" || math.Abs(got.Specs[0].Price-12.5) > 1e-6 {
		t.Fatalf("规格落库异常: %+v", got.Specs)
	}
}

// TestUpdateDish 校验更新菜品的参数校验与规格重建。
func TestUpdateDish(t *testing.T) {
	menuInitDB(t)
	cat := menuSeedCategory(t, "分类")
	id := menuSeedDish(t, cat, "旧菜", 1, 1000, "份")

	if err := UpdateDish(dto.Dish{DishID: id, CategoryID: cat, DishName: "旧菜"}); err == nil {
		t.Fatal("无规格更新应报错")
	}
	if err := UpdateDish(dto.Dish{DishID: id, CategoryID: 0, DishName: "旧菜", Specs: []dto.Spec{{SpecName: "份", Price: 10}}}); err == nil {
		t.Fatal("缺分类更新应报错")
	}

	err := UpdateDish(dto.Dish{DishID: id, CategoryID: cat, DishName: "新菜", Specs: []dto.Spec{
		{SpecName: "大份", Price: 20}, {SpecName: "小份", Price: 12},
	}})
	if err != nil {
		t.Fatalf("UpdateDish 失败: %v", err)
	}
	got, err := DishGet(id)
	if err != nil {
		t.Fatalf("DishGet 失败: %v", err)
	}
	if got.DishName != "新菜" || len(got.Specs) != 2 {
		t.Fatalf("更新结果异常: name=%q specs=%d", got.DishName, len(got.Specs))
	}
}

// TestDeleteDish 校验软删除后不再出现在列表。
func TestDeleteDish(t *testing.T) {
	menuInitDB(t)
	cat := menuSeedCategory(t, "分类")
	id := menuSeedDish(t, cat, "待删菜", 1, 1000, "份")

	if err := DeleteDish(id); err != nil {
		t.Fatalf("DeleteDish 失败: %v", err)
	}
	total, _, err := DishList("待删", "", 1, 10)
	if err != nil {
		t.Fatalf("DishList 失败: %v", err)
	}
	if total != 0 {
		t.Fatalf("软删除后仍可查到, total=%d", total)
	}
}

// TestCategoryCRUD 校验分类列表、新增校验、更新与软删除。
func TestCategoryCRUD(t *testing.T) {
	menuInitDB(t)

	list, err := CategoryList()
	if err != nil || len(list) != 0 {
		t.Fatalf("初始分类应为空: len=%d err=%v", len(list), err)
	}

	if _, err := SaveCategory(dto.Category{CategoryName: "  "}); err == nil {
		t.Fatal("空分类名应报错")
	}
	id, err := SaveCategory(dto.Category{CategoryName: "凉菜"})
	if err != nil || id <= 0 {
		t.Fatalf("SaveCategory 异常: id=%d err=%v", id, err)
	}
	list, _ = CategoryList()
	if len(list) != 1 || list[0].CategoryName != "凉菜" {
		t.Fatalf("分类列表异常: %+v", list)
	}

	if err := UpdateCategory(dto.Category{CategoryID: int(id), CategoryName: ""}); err == nil {
		t.Fatal("空分类名更新应报错")
	}
	if err := UpdateCategory(dto.Category{CategoryID: int(id), CategoryName: "热菜"}); err != nil {
		t.Fatalf("UpdateCategory 失败: %v", err)
	}

	if err := DeleteCategory(int(id)); err != nil {
		t.Fatalf("DeleteCategory 失败: %v", err)
	}
	list, _ = CategoryList()
	if len(list) != 0 {
		t.Fatalf("软删除后分类应不可见, len=%d", len(list))
	}
}

// TestRemarkCRUD 校验备注列表、新增校验、更新与软删除。
func TestRemarkCRUD(t *testing.T) {
	menuInitDB(t)

	list, err := RemarkList()
	if err != nil || len(list) != 0 {
		t.Fatalf("初始备注应为空: len=%d err=%v", len(list), err)
	}

	if _, err := SaveRemark(dto.Remark{OptionName: " "}); err == nil {
		t.Fatal("空备注名应报错")
	}
	id, err := SaveRemark(dto.Remark{OptionName: "加辣"})
	if err != nil || id <= 0 {
		t.Fatalf("SaveRemark 异常: id=%d err=%v", id, err)
	}
	list, _ = RemarkList()
	if len(list) != 1 || list[0].OptionName != "加辣" {
		t.Fatalf("备注列表异常: %+v", list)
	}

	if err := UpdateRemark(dto.Remark{RemarkID: int(id), OptionName: ""}); err == nil {
		t.Fatal("空备注名更新应报错")
	}
	if err := UpdateRemark(dto.Remark{RemarkID: int(id), OptionName: "微辣"}); err != nil {
		t.Fatalf("UpdateRemark 失败: %v", err)
	}

	if err := DeleteRemark(int(id)); err != nil {
		t.Fatalf("DeleteRemark 失败: %v", err)
	}
	list, _ = RemarkList()
	if len(list) != 0 {
		t.Fatalf("软删除后备注应不可见, len=%d", len(list))
	}
}
