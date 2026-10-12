package dao

import (
	"database/sql"
	"errors"
	"testing"

	"dining-system/infrastructure/store"
)

// TestNextPickupNoTx 守护取餐号的三条性质:当天从 1 递增、按营业日独立、随事务回滚。
//
// 这三条是「顾客报号」可用性的底线:
//   - 递增出错 → 同一天出现两个相同号,服务员叫号有歧义;
//   - 按天不独立 → 号会随时间无限增长,失去「短」的意义;
//   - 不随事务回滚 → 会出现「号被占用却查不到订单」的空洞,且无法自愈。
func TestNextPickupNoTx(t *testing.T) {
	orderDaoInitDB(t)

	t.Run("当天首单为1并依次递增", func(t *testing.T) {
		for i, want := range []int{1, 2, 3} {
			if got := pickupNoInTx(t, "2026-10-09"); got != want {
				t.Fatalf("同一营业日第 %d 次取号 = %d, want %d", i+1, got, want)
			}
		}
	})

	t.Run("按营业日各自独立", func(t *testing.T) {
		if got := pickupNoInTx(t, "2026-10-10"); got != 1 {
			t.Fatalf("新营业日应从 1 重新开始, got %d", got)
		}
		if got := pickupNoInTx(t, "2026-10-09"); got != 4 {
			t.Fatalf("新的一天不应影响既有营业日的计数, got %d", got)
		}
	})

	t.Run("事务回滚则号退回", func(t *testing.T) {
		const bizDate = "2026-10-11"
		if got := pickupNoInTx(t, bizDate); got != 1 {
			t.Fatalf("首单应取到 1, got %d", got)
		}
		// 取号后再回滚:号必须退回,否则会留下无法自愈的空洞。
		err := store.WithTx(func(tx *sql.Tx) error {
			n, err := NextPickupNoTx(tx, bizDate)
			if err != nil {
				return err
			}
			if n != 2 {
				t.Errorf("回滚前应取到 2, got %d", n)
			}
			return errors.New("故意回滚")
		})
		if err == nil {
			t.Fatal("本事务应返回错误")
		}
		if got := pickupNoInTx(t, bizDate); got != 2 {
			t.Fatalf("回滚后应重新取到 2, got %d", got)
		}
	})

	t.Run("空营业日拒绝取号", func(t *testing.T) {
		err := store.WithTx(func(tx *sql.Tx) error {
			_, err := NextPickupNoTx(tx, "")
			return err
		})
		if err == nil {
			t.Fatal("空营业日应报错,不应静默返回 0")
		}
	})
}

// TestPickupNoUniqueIndexGuardsManualInsert 确认唯一索引确实拦得住重复号。
// 正常路径由 NextPickupNoTx 的行锁保证唯一;这条索引是「有人跳过该函数直接插订单」时的兜底。
func TestPickupNoUniqueIndexGuardsManualInsert(t *testing.T) {
	orderDaoInitDB(t)
	bizDate := store.Now()[:10]
	// 两次写入必须给不同的 order_no:否则先撞的是 idx_order_no,
	// 验证的就不再是本次要测的 idx_order_pickup 了。
	insert := func(orderNo string, pickupNo int) error {
		return store.WithTx(func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO tb_order(order_no, biz_date, pickup_no, table_id, table_no, person_count, create_time)
				VALUES(?,?,?,?,?,?,?)`,
				orderNo, bizDate, pickupNo, 1, "01", 1, store.Now())
			return err
		})
	}
	if err := insert("D-PICKUP-TEST-1", 7); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := insert("D-PICKUP-TEST-2", 7); err == nil {
		t.Fatalf("同一营业日的重复取餐号应被唯一索引拒绝")
	}
}

// pickupNoInTx 在事务里取一次号并正常提交。
func pickupNoInTx(t *testing.T, bizDate string) int {
	t.Helper()
	var n int
	if err := store.WithTx(func(tx *sql.Tx) error {
		var err error
		n, err = NextPickupNoTx(tx, bizDate)
		return err
	}); err != nil {
		t.Fatalf("取餐号失败: %v", err)
	}
	return n
}
