package po

import "math"

// Round2 将金额四舍五入到分,规避浮点误差。
func Round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// ToCents 将元转为分(四舍五入到整数分)。
func ToCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

// ToYuan 将分转为元。
func ToYuan(cents int64) float64 {
	return float64(cents) / 100.0
}
