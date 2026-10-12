// Package feie 飞鹅开放平台(HTTP API)适配器。
//
// 本包是「外部服务接口层」:只负责协议 —— 鉴权签名、接口调用、响应解码、
// 错误码翻译,不读业务库、不懂打印机与订单模型。账号参数由调用方在创建时
// 显式传入(infrastructure/print 负责从系统配置读取),商户改配置即刻生效、无需重启。
//
// 与 ESC/POS 直连的本质区别:后端不需要能访问打印机,只要能把请求发到飞鹅云;
// 打印机自己联网到飞鹅云取单。因此后端部署在云服务器、门店在任意城市的场景
// 只能走这条通道(直连 TCP 那种做法在云端根本够不到门店内网)。
//
// 接口规范(据官方开放平台文档):
//   - 地址: https://api.feieyun.cn/Api/Open/
//   - 方式: POST,表单编码(Content-Type: application/x-www-form-urlencoded)
//   - 公共参数:
//     user    飞鹅云后台注册用户名
//     stime   当前 UNIX 时间戳,10 位,精确到秒(与标准时间偏差过大会报 -3)
//     sig     sha1(user + UKEY + stime) 的 40 位小写十六进制
//     apiname 接口名,如 Open_printMsg
//   - 返回: {"msg":..., "ret":..., "data":..., "serverExecutedTime":...},
//     ret=0 为成功,其余为错误码(见 errorMessage)。
package feie

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultAPI 飞鹅开放平台默认接口地址(海外/私有云可在系统配置里改)。
const DefaultAPI = "https://api.feieyun.cn/Api/Open/"

// MaxBytes 单次推送内容上限。官方限制 5000 字节,这里留出余量,
// 超长内容按行切分多次推送(官方建议也是「拆成两单发」)。
const MaxBytes = 4800

// Config 飞鹅账号配置。调用方从系统配置读取后传入。
type Config struct {
	User   string
	Ukey   string
	APIURL string // 留空时取 DefaultAPI
}

// Client 飞鹅开放平台客户端(接口层,可并发复用)。
type Client struct {
	user   string
	ukey   string
	apiURL string
	hc     *http.Client
}

// NewClient 用账号配置构造客户端。参数合法性(非空)由调用方负责:
// 本包保持纯协议层,不回读任何业务配置。
func NewClient(cfg Config) *Client {
	api := strings.TrimSpace(cfg.APIURL)
	if api == "" {
		api = DefaultAPI
	}
	return &Client{
		user:   cfg.User,
		ukey:   cfg.Ukey,
		apiURL: api,
		hc:     &http.Client{Timeout: 12 * time.Second},
	}
}

// APIError 飞鹅接口返回的业务错误(ret != 0)。
//
// 返回码类错误属于「参数/配置问题」,重试也不会有不同结果,调用方应视为
// 永久性错误(不重试);网络/HTTP 层错误则是普通 error,可以重试。
type APIError struct {
	Ret int
	Msg string // 已翻译为中文可读文案
}

func (e *APIError) Error() string { return e.Msg }

// errorMessage 把返回码翻成给商户看的中文提示。
// 码值来自官方文档的「接口返回值」章节。
func errorMessage(ret int, msg string) string {
	switch ret {
	case -1:
		return "打印机编号与飞鹅账号不匹配(请在飞鹅后台把该打印机绑定到当前账号)"
	case -2:
		return "参数错误:" + msg
	case -3:
		return "签名校验失败:请检查 UKEY 是否填对(注意 UKEY 不是打印机机身 KEY),以及服务器时间是否准确"
	case -4:
		return "飞鹅账号未注册,请检查账号是否填写正确"
	case 1001:
		return "打印机编号与识别码(KEY)不匹配,请核对机身标签"
	case 1002:
		return "该打印机未注册到当前飞鹅账号"
	case 1003:
		return "打印机不在线(请检查打印机网络与电源)"
	case 1004:
		return "飞鹅云端添加打印任务失败,请稍后重试"
	case 1005:
		return "飞鹅云端未找到该打印任务"
	case 1006:
		return "订单日期格式不正确(应为 YYYY-MM-DD)"
	case 1007:
		return "打印内容过大,请减少单据内容"
	case 1008:
		return "修改打印机记录失败"
	case 1009:
		return "打印机编号或名称不能为空"
	case 1010:
		return "打印机设备编号无效"
	case 1011:
		return "该打印机已存在(若开放平台查不到该设备,请联系飞鹅售后)"
	case 1012:
		return "添加打印设备失败,请稍后重试"
	}
	if strings.TrimSpace(msg) != "" {
		return fmt.Sprintf("飞鹅接口返回错误(ret=%d): %s", ret, msg)
	}
	return fmt.Sprintf("飞鹅接口返回错误(ret=%d)", ret)
}

// resp 飞鹅统一响应体。
type resp struct {
	Msg                string          `json:"msg"`
	Ret                int             `json:"ret"`
	Data               json.RawMessage `json:"data"`
	ServerExecutedTime int             `json:"serverExecutedTime"`
}

// call 发起一次接口调用。ret != 0 时返回 *APIError(已是中文可读文案)。
func (c *Client) call(apiname string, params map[string]string) (*resp, error) {
	stime := strconv.FormatInt(time.Now().Unix(), 10)
	form := url.Values{}
	form.Set("user", c.user)
	form.Set("stime", stime)
	form.Set("sig", Sign(c.user, c.ukey, stime))
	form.Set("apiname", apiname)
	for k, v := range params {
		form.Set(k, v)
	}

	req, err := http.NewRequest(http.MethodPost, c.apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	respHTTP, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接飞鹅云失败: %v", err)
	}
	defer respHTTP.Body.Close()
	body, err := io.ReadAll(io.LimitReader(respHTTP.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取飞鹅云响应失败: %v", err)
	}
	if respHTTP.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("飞鹅云返回 HTTP %d", respHTTP.StatusCode)
	}

	var r resp
	if err := json.Unmarshal(body, &r); err != nil {
		// 飞鹅在维护或网关异常时可能返回非 JSON,把前 200 个字符带上便于排查
		// (按字符而非字节截断,避免多字节响应被切出乱码)。
		return nil, fmt.Errorf("飞鹅云响应格式异常: %s", truncateRunes(string(body), 200))
	}
	if r.Ret != 0 {
		return &r, &APIError{Ret: r.Ret, Msg: errorMessage(r.Ret, r.Msg)}
	}
	return &r, nil
}

// Sign 计算签名:sha1(user + UKEY + stime),40 位小写十六进制。
func Sign(user, ukey, stime string) string {
	sum := sha1.Sum([]byte(user + ukey + stime))
	return hex.EncodeToString(sum[:])
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// ---- data 字段解码:同一字段在不同接口下分别是 string / bool / object,按需解码 ----

func (r *resp) dataString() string {
	var s string
	if err := json.Unmarshal(r.Data, &s); err != nil {
		return ""
	}
	return s
}

func (r *resp) dataBool() bool {
	var b bool
	if err := json.Unmarshal(r.Data, &b); err != nil {
		return false
	}
	return b
}

func (r *resp) dataObject(v interface{}) error { return json.Unmarshal(r.Data, v) }

// ============================================================================
// 开放平台接口
// ============================================================================

// PrintMsg 打印订单(Open_printMsg,仅小票机)。返回飞鹅云端订单号。
func (c *Client) PrintMsg(sn, content string, times int) (string, error) {
	if times < 1 {
		times = 1
	}
	r, err := c.call("Open_printMsg", map[string]string{
		"sn": sn, "content": content, "times": strconv.Itoa(times),
	})
	if err != nil {
		return "", err
	}
	return r.dataString(), nil
}

// PrinterStatus 查询打印机状态(Open_queryPrinterStatus)。
// data 为文本: 「离线」/「在线,工作状态正常」/「在线,工作状态不正常」。
func (c *Client) PrinterStatus(sn string) (string, error) {
	r, err := c.call("Open_queryPrinterStatus", map[string]string{"sn": sn})
	if err != nil {
		return "", err
	}
	return r.dataString(), nil
}

// ClearQueue 清空待打印队列(Open_delPrinterSqs)。
// 打错单、换纸后想把积压任务清掉时用。
func (c *Client) ClearQueue(sn string) error {
	_, err := c.call("Open_delPrinterSqs", map[string]string{"sn": sn})
	return err
}

// QueryOrderState 查询某次推送是否真的打出来了(Open_queryOrderState)。
// orderID 取自 PrintMsg 的返回值。
func (c *Client) QueryOrderState(orderID string) (bool, error) {
	r, err := c.call("Open_queryOrderState", map[string]string{"orderid": orderID})
	if err != nil {
		return false, err
	}
	return r.dataBool(), nil
}

// QueryOrderInfoByDate 查询某台打印机某天的打印统计(Open_queryOrderInfoByDate)。
// date 格式 YYYY-MM-DD。
func (c *Client) QueryOrderInfoByDate(sn, date string) (printed, waiting int, err error) {
	r, err := c.call("Open_queryOrderInfoByDate", map[string]string{"sn": sn, "date": date})
	if err != nil {
		return 0, 0, err
	}
	var d struct {
		Print   int `json:"print"`
		Waiting int `json:"waiting"`
	}
	if err := r.dataObject(&d); err != nil {
		return 0, 0, err
	}
	return d.Print, d.Waiting, nil
}

// GetModel 查询设备硬件类型(Open_getModel)。
// 该接口不校验绑定关系,适合在「添加打印机」之前先验证 SN/KEY 是否填对。
// 返回: 0=58mm 热敏 1=80mm 热敏 2=标签机 3=条码扫描 4=58mm+扫描一体。
func (c *Client) GetModel(sn, key string) (int, error) {
	r, err := c.call("Open_getModel", map[string]string{"sn": sn, "key": key})
	if err != nil {
		return -1, err
	}
	var d struct {
		Model int `json:"model"`
	}
	if err := r.dataObject(&d); err != nil {
		return -1, err
	}
	return d.Model, nil
}

// AddPrinter 把打印机绑定到当前账号(Open_printerAddlist)。
// printerContent 格式: 打印机编号#识别码#备注名称#流量卡号(后两项可省),最多 100 台。
func (c *Client) AddPrinter(printerContent string) (okList, noList []string, err error) {
	r, err := c.call("Open_printerAddlist", map[string]string{"printerContent": printerContent})
	if err != nil {
		return nil, nil, err
	}
	var d struct {
		Ok []string `json:"ok"`
		No []string `json:"no"`
	}
	if err := r.dataObject(&d); err != nil {
		return nil, nil, err
	}
	return d.Ok, d.No, nil
}

// DelPrinter 从账号解绑打印机(Open_printerDelList),多台用 "-" 连接。
func (c *Client) DelPrinter(sn string) error {
	_, err := c.call("Open_printerDelList", map[string]string{"snlist": sn})
	return err
}
