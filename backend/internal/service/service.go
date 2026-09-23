// Package service 承载全店业务用例层,按域划分:订单、认证与用户、顾客端、
// 菜单(菜品/分类/备注)、支付、打印、报表、设置、桌台、上传、审计与权限。
//
// 数据访问统一经 store/dao 下沉,本层只做业务判断与编排。
//
// 依赖方向:service 依赖 po + dto + store + dao(及 infra 基础设施),
// 不依赖 handler/print/pay;handler 与 pay/print 反向依赖 service。
package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store/dao"
)

// GenOrderNo 生成订单号:"D" + 秒级时间戳 + 64bit 密码学安全随机数(16 位十六进制)。
// 随机部分来自 crypto/rand,使订单号不可预测、不可枚举(替代原 math/rand 的 3 位随机数)。
func GenOrderNo() string {
	return "D" + time.Now().Format("20060102150405") + randHex(16)
}

// randHex 返回 n 位十六进制随机串(来自 crypto/rand)。极端失败时退化为时间戳,不影响可用性。
func randHex(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ---- 订单状态机 ----
// 状态定义:1已下单 2制作中 3已上齐(用餐中) 4已完成 5已取消(与 po.Order.OrderStatus 一致)。
const (
	OrderStatusPlaced   = 1 // 已下单
	OrderStatusCooking  = 2 // 制作中
	OrderStatusDining   = 3 // 已上齐(用餐中)
	OrderStatusFinished = 4 // 已完成
	OrderStatusCanceled = 5 // 已取消
)

// orderTransitions 合法状态跳转表:key 为当前状态,value 为允许跳转到的目标状态。
// 采用严格线性流转 1→2→3→4,任意进行中状态可取消(5);
// 已完成(4)/已取消(5)为终态,不可再跳转。禁止跳级(如 1 直接到 4),避免误操作跳过制作/上齐。
var orderTransitions = map[int]map[int]bool{
	OrderStatusPlaced:   {OrderStatusCooking: true, OrderStatusCanceled: true},
	OrderStatusCooking:  {OrderStatusDining: true, OrderStatusCanceled: true},
	OrderStatusDining:   {OrderStatusFinished: true, OrderStatusCanceled: true},
	OrderStatusFinished: {},
	OrderStatusCanceled: {},
}

// CanTransition 判断订单状态是否可从 from 合法跳转到 to。
func CanTransition(from, to int) bool {
	return orderTransitions[from][to]
}

// ActiveStatus 判断是否为进行中状态(可取消/改单/加菜/收款)。
func ActiveStatus(s int) bool {
	return s == OrderStatusPlaced || s == OrderStatusCooking || s == OrderStatusDining
}

// ValidOrderStatus 判断订单状态是否合法(1~5)。
func ValidOrderStatus(s int) bool {
	return s >= 1 && s <= 5
}

// ---- 结算方式 ----
// 三种结算方式:
//   - normal 正常收款:实收 = 应收,款项当场结清;
//   - free   免单:商家让利,应收照常记账但实收为 0,需填写免单原因;
//   - credit 挂账(记账):先把账记下,订单可正常完成、桌台释放,后续在挂账管理中核销收款。
const (
	SettleTypeNormal = "normal"
	SettleTypeFree   = "free"
	SettleTypeCredit = "credit"
)

// 挂账状态:0 非挂账 1 待收款 2 已结清。
const (
	CreditStatusNone    = 0
	CreditStatusPending = 1
	CreditStatusSettled = 2
)

// ValidSettleType 判断结算方式是否合法;空值按正常收款处理(兼容旧客户端)。
func ValidSettleType(t string) bool {
	switch t {
	case "", SettleTypeNormal, SettleTypeFree, SettleTypeCredit:
		return true
	}
	return false
}

// NormalizeSettleType 归一化结算方式:空值为正常收款。
func NormalizeSettleType(t string) string {
	if t == "" {
		return SettleTypeNormal
	}
	return t
}

// NormalizePayType 归一化支付方式:空白默认「现金」,超长文本裁剪(最长 16 个字符),
// 避免前端传入任意长文本污染数据。
func NormalizePayType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "现金"
	}
	r := []rune(t)
	if len(r) > 16 {
		t = string(r[:16])
	}
	return t
}

// SettleResult 描述一次结算应写入的金额与挂账状态。
type SettleResult struct {
	PaidCents    int64 // 实收金额(分)
	CreditStatus int   // 挂账状态
	CreditCents  int64 // 挂账金额(分)
}

// CalcSettle 按结算方式计算实收/挂账金额。
//   - normal:实收 = 应收;
//   - free:实收 = 0(全部让利);
//   - credit:实收 = 0,挂账金额 = 应收,状态置为待收款。
func CalcSettle(settleType string, totalCents int64) SettleResult {
	switch settleType {
	case SettleTypeFree:
		return SettleResult{PaidCents: 0, CreditStatus: CreditStatusNone, CreditCents: 0}
	case SettleTypeCredit:
		return SettleResult{PaidCents: 0, CreditStatus: CreditStatusPending, CreditCents: totalCents}
	default:
		return SettleResult{PaidCents: totalCents, CreditStatus: CreditStatusNone, CreditCents: 0}
	}
}

// RecalcAmount 金额重算:菜品金额 + 餐位费 - 优惠,返回(菜品金额, 餐位费, 优惠, 合计)。
func RecalcAmount(personCount int, items []dto.OrderItem) (dishAmount, seatFee, discount, total float64) {
	for _, it := range items {
		dishAmount += it.Amount
	}
	settings := dao.LoadSettings()
	if settings["seat_fee_enabled"] == "1" {
		f, _ := strconv.ParseFloat(settings["seat_fee"], 64)
		seatFee = po.Round2(float64(personCount) * f)
	}
	if settings["promotion_enabled"] == "1" {
		threshold, _ := strconv.ParseFloat(settings["promotion_threshold"], 64)
		d, _ := strconv.ParseFloat(settings["promotion_discount"], 64)
		// 每满 threshold 减 d(可叠加),优惠不超过菜品金额本身。
		if threshold > 0 && d > 0 && dishAmount >= threshold {
			discount = po.Round2(math.Min(math.Floor(dishAmount/threshold)*d, dishAmount))
		}
	}
	dishAmount = po.Round2(dishAmount)
	total = po.Round2(dishAmount + seatFee - discount)
	if total < 0 {
		total = 0
	}
	return
}

// 单次下单/加菜的明细条数与单品数量上限,防止异常请求导致数据库膨胀。
const (
	maxOrderItems   = 50 // 单次下单/加菜明细条数上限
	maxItemQuantity = 99 // 单个菜品数量上限
)

// ResolveOrderItems 以数据库为准校验并重建订单明细,防止客户端篡改价格/名称。
//
// 菜品/规格信息改为一次 IN 查询批量取出(见 dao.LoadSpecsForOrder):单次下单最多
// 50 条明细,旧实现逐条 GetSpecForOrder 会发起 50 次查询,这里固定 1 次。
// 校验语义不变:规格必须存在且归属请求的菜品,菜品须在售(未删除且 status=1),
// 数量须在上限内。
func ResolveOrderItems(items []dto.OrderItem) ([]dto.OrderItem, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("订单明细不能为空")
	}
	if len(items) > maxOrderItems {
		return nil, fmt.Errorf("单次点菜过多(最多%d项)", maxOrderItems)
	}

	// 收集全部规格 ID(去重),并先做不依赖数据库的参数校验。
	specIDs := make([]int, 0, len(items))
	seen := map[int]bool{}
	for _, it := range items {
		if it.SpecID == 0 {
			return nil, fmt.Errorf("菜品规格无效")
		}
		if !seen[it.SpecID] {
			seen[it.SpecID] = true
			specIDs = append(specIDs, it.SpecID)
		}
	}
	specMap, err := dao.LoadSpecsForOrder(specIDs)
	if err != nil {
		return nil, fmt.Errorf("部分菜品不存在，请刷新后重试")
	}

	out := make([]dto.OrderItem, 0, len(items))
	for _, it := range items {
		if it.Quantity < 1 {
			it.Quantity = 1
		}
		info, ok := specMap[it.SpecID]
		// 规格不存在,或规格不属于请求的菜品,均等价于旧查询
		// s.spec_id=? AND s.dish_id=? 未命中:提示刷新。
		if !ok || info.DishID != it.DishID {
			return nil, fmt.Errorf("部分菜品不存在，请刷新后重试")
		}
		if info.DelFlag != po.DelFlagOK || info.DishStatus != 1 {
			return nil, fmt.Errorf("菜品【%s】已下架", info.DishName)
		}
		if it.Quantity > maxItemQuantity {
			return nil, fmt.Errorf("菜品【%s】数量超出上限(%d)", info.DishName, maxItemQuantity)
		}
		it.DishName = info.DishName
		it.SpecName = info.SpecName
		it.Price = po.ToYuan(info.PriceCents)
		it.Amount = po.Round2(it.Price * float64(it.Quantity))
		out = append(out, it)
	}
	return out, nil
}
