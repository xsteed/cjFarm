// Package geoip 提供 IP → 地理位置 的离线解析(城市级)。
//
// 数据源是 ip2region v2 的离线库(data/ip2region.xdb,约 11MB,gitignored
// 运行期资产),查询纯内存、无任何外呼 —— 与本仓库「内网可用、不依赖第三方
// 在线服务」的基调一致。库文件缺失时一切查询返回空串并只告警一次:
// 城市是锦上添花,缺库绝不能影响打点主流程。
//
// 返回格式是归一后的展示值,如「中国 江苏省 南京市」「内网」;解析不出给空串。
package geoip

import (
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"

	"dining-system/infrastructure"
	"dining-system/infrastructure/conf"
	"dining-system/infrastructure/logger"
)

var (
	once     sync.Once
	searcher *xdb.Searcher
)

// load 惰性加载:首次查询时把 xdb 整体读进内存(约 11MB,线程安全)。
// 数据文件路径可用 IP2REGION_PATH 覆盖(默认 data/ip2region.xdb)。
// 只在 once 里执行一次,成功与否都只留一条日志。
func load() {
	path := strings.TrimSpace(infrastructure.Getenv(conf.EnvVisitIP2RegionPath, conf.DefaultVisitIP2RegionPath))
	header, err := xdb.LoadHeaderFromFile(path)
	if err != nil {
		logger.Warnf("[geoip] IP 城市库不可用(%v),访问记录的 city 字段将为空;"+
			"放置 ip2region.xdb 于 %s 后重启即可启用", err, path)
		return
	}
	version, err := xdb.VersionFromHeader(header)
	if err != nil {
		logger.Warnf("[geoip] 识别 IP 城市库版本失败(%v),city 字段将为空", err)
		return
	}
	content, err := xdb.LoadContentFromFile(path)
	if err != nil {
		logger.Warnf("[geoip] 加载 IP 城市库内容失败(%v),city 字段将为空", err)
		return
	}
	s, err := xdb.NewWithBuffer(version, content)
	if err != nil {
		logger.Warnf("[geoip] 构建 IP 城市查询器失败(%v),city 字段将为空", err)
		return
	}
	searcher = s
	logger.Infof("[geoip] IP 城市库已就绪(%s, %s)", path, version)
}

// CityOf 返回 IP 的地理位置描述;解析失败或未知一律空串。
// 结果示例:「中国 江苏省 南京市」「内网」「美国」。
func CityOf(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	once.Do(load)
	if searcher == nil {
		return ""
	}
	region, err := searcher.Search(ip)
	if err != nil || region == "" {
		return ""
	}
	// ip2region 原始格式:国家|区域|省份|城市|ISP
	parts := strings.Split(region, "|")
	if len(parts) < 5 {
		return ""
	}
	if strings.Contains(region, "内网IP") {
		return "内网"
	}
	seg := make([]string, 0, 3)
	for _, p := range []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])} {
		if p != "" && p != "0" {
			seg = append(seg, p)
		}
	}
	return strings.Join(seg, " ")
}
