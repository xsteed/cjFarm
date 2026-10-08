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
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"dining-system/internal/dto"
	"dining-system/internal/po"
	"dining-system/internal/store/dao"
)

// ---- 单号生成 ----
//
// 所有单号统一为「业务前缀 + 秒级时间戳 + 随机段」,共 20 位,形如
// D261008123456K3M9P2R(前缀 1 + yyMMddHHmmss 12 + Base32 随机 7)。
//
// 随机段必须取自 crypto/rand 而非自增/雪花等线性编号:顾客端
// `/customer/order/:orderNo`(handler/customer.go)无需登录即可凭单号查单,
// 线性编号等于开放对全量订单的遍历枚举。
//
// 长度取舍:31 位(4 位年 + 16 位十六进制随机)虽然更安全,但会占满 58mm 热敏
// 小票的一整行。实际约束只有两条 —— 撞号(唯一索引)与枚举(查单接口):
//   - 撞号:日几百单量级下 35bit 已绰绰有余;
//   - 枚举:35bit 需百亿级尝试才可能命中,配合接口限流足够安全。
//     枚举防线终究是限流与监控,位数只是抬高门槛,不应为它牺牲可读性。

// 单号首位前缀。订单号用 D(Dining);退款类单据号与其同构,仅首位不同,
// 便于在退款列表里一眼区分款项性质。
const (
	OrderNoPrefix  = "D"
	RefundNoManual = "M" // 金额不一致的人工核对项:系统自动落一条失败状态退款单,并非真实退款
	RefundNoRefund = "R" // 真实退款流水
)

// serialNoTimeLayout 单号中的时间戳段:yyMMddHHmmss,定长 12 位,保证字典序即时间序。
//
// 年份取 2 位的代价:2100 年之后时间序会回卷(00 < 99)。餐饮系统的生命周期
// 内不会触及,若将来需要,改成 4 位年即可(单号长度 +2,其余逻辑不变)。
const serialNoTimeLayout = "060102150405"

// serialNoRandomLen 单号中的随机段长度:7 位 Crockford Base32 = 35bit。
const serialNoRandomLen = 7

// SerialNoLen 单号总长度:前缀 1 + 时间戳 12 + 随机段 7 = 20 位。
// 对外契约:小票版面与前端展示按此宽度排版,调整任一组成部分时必须同步这里。
const SerialNoLen = 1 + 12 + serialNoRandomLen

// base32Alphabet Crockford Base32 字母表:剔除 I / L / O / U 四个易混字符
// (分别会被误读成 1 / 1 / 0 / V),单号需要被人工抄写或电话报读时不易出错。
// 顺序即数值 0~31,解码时按 index 还原即可。
const base32Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// GenOrderNo 生成订单号:D + 时间戳(12) + 随机段(7),共 20 位。
func GenOrderNo() string {
	return genSerialNo(OrderNoPrefix)
}

// GenRefundNo 生成退款单号,与订单号同构,首位为款项类型前缀(见 RefundNoManual/RefundNoRefund)。
//
// 此前这里写成 "R" + GenOrderNo()[1:],隐含依赖「订单号首字母恰好就是待裁掉的那一位」;
// 一旦订单号的前缀或长度调整,退款号会在无编译报错的情况下静默变形,
// 而 refund_no 上有唯一索引,错误形态会被一路写进支付流水。显式传前缀可消除该耦合。
func GenRefundNo(prefix string) string {
	if prefix == "" {
		// 兜底:宁可按真实退款处理,也不产生无前缀、无法辨识性质的单据号。
		prefix = RefundNoRefund
	}
	return genSerialNo(prefix)
}

// genSerialNo 拼接单号各段,唯一入口,保证订单号与退款单号结构永远一致。
func genSerialNo(prefix string) string {
	return prefix + time.Now().Format(serialNoTimeLayout) + randBase32(serialNoRandomLen)
}

// randBase32 返回 n 位 Crockford Base32 随机串(来自 crypto/rand),每字符 5bit。
//
// 逐字节取低 5 位索引字母表:256 能被 32 整除,每个 5bit 取值恰好命中 8 次,
// 因此分布完全均匀,不需要拒绝采样 —— 若用「读 5 字节再位运算切分」的写法,
// 反而容易在边界上引入偏差。
//
// crypto/rand 读取失败意味着操作系统熵源不可用。此时若像过去那样退化为
// time.Now().UnixNano(),产出的就是可预测、且相邻两次调用可能完全相同的值;
// 而 order_no/refund_no 上都有唯一索引,撞号会直接让下单或退款失败,
// 这种「偶发失败且现场无堆栈」的问题比显式失败更难排查。
// 因此这里选择 panic:HTTP 链路由 logger.GinRecovery() 兜转成 500,
// 并留下带 requestID 的完整堆栈。
func randBase32(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("service: crypto/rand 不可用,拒绝生成可预测的单号: %v", err))
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = base32Alphabet[v&0x1F]
	}
	return string(out)
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

// RecalcAmount 金额重算:菜品金额 + 茶位费 - 优惠,返回(菜品金额, 茶位费, 优惠, 赠送金额, 合计)。
//
// 赠送菜品(is_gift=1)不进入菜品金额与合计,但按原价累加到赠送金额 giftAmount,
// 用于让利口径统计与小票/报表展示;优惠满减仍基于「菜品金额(不含赠送)」计算。
func RecalcAmount(personCount int, items []dto.OrderItem) (dishAmount, seatFee, discount, giftAmount, total float64) {
	for _, it := range items {
		if it.IsGift == 1 {
			giftAmount += it.Price * float64(it.Quantity)
			continue
		}
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
	giftAmount = po.Round2(giftAmount)
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
		if info.DishSoldOut == 1 {
			return nil, fmt.Errorf("菜品【%s】已售罄", info.DishName)
		}
		// 库存售罄：库存为 0 即售罄（快速失败，覆盖绝大多数情况）。
		// 「库存不足但还有」这类并发下的临界情况留给事务内扣减兜底（DecrementSpecStockTx）。
		if info.SpecStock == 0 {
			return nil, fmt.Errorf("菜品【%s】已售罄", info.DishName)
		}
		if it.Quantity > maxItemQuantity {
			return nil, fmt.Errorf("菜品【%s】数量超出上限(%d)", info.DishName, maxItemQuantity)
		}
		it.DishName = info.DishName
		it.SpecName = info.SpecName
		it.Price = po.ToYuan(info.PriceCents)
		if it.IsGift == 1 {
			// 赠送菜:保留原价(供 giftAmount 统计与小票展示),计费金额记 0。
			it.Amount = 0
		} else {
			it.Amount = po.Round2(it.Price * float64(it.Quantity))
		}
		out = append(out, it)
	}
	return out, nil
}
