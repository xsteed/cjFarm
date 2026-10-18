package po

import (
	"fmt"
	"math"
)

// Round2 将金额四舍五入到分,规避浮点误差。
func Round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// FormatYuan 把「分」格式化成给顾客看的金额串(1234 → "12.34")。
//
// 用于错误提示与审计文案:直接 %d 分会把「上限 5000 元」说成「500000」,
// 门店按提示操作时会以为是自己看错了单位。
func FormatYuan(cents int64) string {
	if cents < 0 {
		return "-" + FormatYuan(-cents)
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// ToCents 将元转为分(四舍五入到整数分)。
func ToCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

// ToYuan 将分转为元。
func ToYuan(cents int64) float64 {
	return float64(cents) / 100.0
}
