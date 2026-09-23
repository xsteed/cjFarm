package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	agentProtocolVersion = 2
	ackAttempts          = 3
)

// capabilityAckSkipped 云端能力名:能处理 ack 里的 skipped(未真正尝试)回执。
//
// 老云端不声明该能力,代理随即退回「挂起不回执」的兼容行为 —— 任务保持 claimed,
// 等 60 秒租约到期自动回到队列。效果等价,只是要等一个租约。
const capabilityAckSkipped = "ack-skipped"

// agentCapabilities 声明本代理支持的协议能力,云端据此调整下发策略。
var agentCapabilities = []string{"printer-status", "long-pull", capabilityAckSkipped}

// serverSupportsSkipped 记录最近一次 pull/ping 响应里云端是否声明了 ack-skipped。
//
// 每次响应都重新判定(而不是只置真):云端回滚/降级时代理能跟着退回兼容行为。
var serverSupportsSkipped atomic.Bool

// noteServerCapabilities 记录云端声明的能力。
func noteServerCapabilities(caps []string) {
	supported := false
	for _, c := range caps {
		if c == capabilityAckSkipped {
			supported = true
			break
		}
	}
	serverSupportsSkipped.Store(supported)
}

// ============================================================================
// 接口调用
// ============================================================================

type agentJob struct {
	JobID       int    `json:"jobId"`
	PrinterID   int    `json:"printerId"`
	PrinterName string `json:"printerName"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Copies      int    `json:"copies"`
	DocType     string `json:"docType"`
	OrderNo     string `json:"orderNo"`
	TableNo     string `json:"tableNo"`
	// DeliveryID 幂等投递号:打印成功后落盘,重发时据此去重(见 runCycle)。
	DeliveryID string `json:"deliveryId"`
	Payload    string `json:"payload"`
}

// apiResp 后端统一响应体(与浏览器端共用同一套 {code,msg,data} 约定)。
type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// post 发起一次带令牌的 POST 请求并解析统一响应体。
func post(client *http.Client, base, path string, cfg config, body interface{}, out interface{}) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", cfg.token)
	req.Header.Set("X-Agent-Version", strconv.Itoa(agentProtocolVersion))
	if cfg.name != "" {
		req.Header.Set("X-Agent-Name", cfg.name)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	var r apiResp
	if err := json.Unmarshal(payload, &r); err != nil {
		return fmt.Errorf("云端返回格式异常(HTTP %d): %s", resp.StatusCode, snippet(payload))
	}
	if r.Code != 200 {
		if r.Msg != "" {
			return errors.New(r.Msg)
		}
		return fmt.Errorf("云端返回 HTTP %d", resp.StatusCode)
	}
	if out != nil && len(r.Data) > 0 {
		if err := json.Unmarshal(r.Data, out); err != nil {
			return fmt.Errorf("解析云端数据失败: %w", err)
		}
	}
	return nil
}

// pull 取一批待打印任务。
func pull(client *http.Client, base string, cfg config) ([]agentJob, error) {
	var out struct {
		Jobs               []agentJob `json:"jobs"`
		ServerCapabilities []string   `json:"serverCapabilities"`
	}
	pullClient := client
	if cfg.wait > 0 {
		// 长轮询的 HTTP 超时必须覆盖服务端 hold 时间;Transport 复用保持连接池行为不变。
		pullClient = &http.Client{
			Timeout:   time.Duration(cfg.wait)*time.Second + 10*time.Second,
			Transport: client.Transport,
		}
	}
	err := post(pullClient, base, "/api/agent/print/pull", cfg,
		pullBody(cfg.name, cfg.limit, cfg.wait), &out)
	if err != nil {
		// 取单失败(门店断网 / 云端重启)时不能拿空响应去刷新能力清单:
		// 一次网络抖动就会把云端判成「不支持 ack-skipped」,冷却任务全部退化成
		// 挂起等租约。失败时保持上一次的能力判定。
		return nil, err
	}
	noteServerCapabilities(out.ServerCapabilities)
	return out.Jobs, nil
}

// pullBody 构造 pull 请求体。buildVersion 上报软件版本,与 version(协议版本)区分。
func pullBody(name string, limit, wait int) map[string]interface{} {
	return map[string]interface{}{
		"agentId":      name,
		"limit":        limit,
		"version":      agentProtocolVersion,
		"capabilities": agentCapabilities,
		"wait":         wait,
		"buildVersion": version,
	}
}

// ack 回报结果。
//
// 失败会重试几次:打印本身已经成功、只是回执丢了的话,任务会因租约超时被重新下发,
// 导致同一张票打两遍。重试能把这个窗口压到很小,但网络彻底中断时仍可能重复
// (宁可重打一张,也不能丢单)—— 这一点在 docs/print-agent.md 里已说明。
func ack(client *http.Client, base string, cfg config, results interface{}) {
	body := map[string]interface{}{"results": results}
	var lastErr error
	for i := 1; i <= ackAttempts; i++ {
		if err := post(client, base, "/api/agent/print/ack", cfg, body, nil); err == nil {
			if i > 1 {
				logf("回执已补送成功(第 %d 次尝试)", i)
			}
			return
		} else {
			lastErr = err
		}
		time.Sleep(time.Duration(i) * time.Second)
	}
	logf("[警告] 回执发送失败(任务已被云端标记为打印中,租约超时后会重新下发,可能重复打印): %v", lastErr)
}

// ping 启动自检:校验地址与令牌。
func ping(client *http.Client, base string, cfg config) error {
	var out struct {
		ShopName           string   `json:"shopName"`
		ServerTime         string   `json:"serverTime"`
		Pending            int      `json:"pending"`
		LatestAgentVersion string   `json:"latestAgentVersion"`
		ServerCapabilities []string `json:"serverCapabilities"`
	}
	if err := post(client, base, "/api/agent/print/ping", cfg,
		pingBody(cfg.name), &out); err != nil {
		return err
	}
	noteServerCapabilities(out.ServerCapabilities)
	logf("云端自检通过: 店铺=%q 服务器时间=%s 待送任务=%d 条 代理版本=%s",
		out.ShopName, out.ServerTime, out.Pending, version)
	if out.LatestAgentVersion != "" && out.LatestAgentVersion != version {
		logf("[更新] 云端最新版本 v%s,当前 v%s,请下载新产物替换后重启(升级步骤见 docs/print-agent.md)",
			out.LatestAgentVersion, version)
	}
	return nil
}

// pingBody 构造 ping 请求体。buildVersion 上报软件版本,与 version(协议版本)区分。
func pingBody(name string) map[string]interface{} {
	return map[string]interface{}{
		"agentId":      name,
		"version":      agentProtocolVersion,
		"capabilities": agentCapabilities,
		"buildVersion": version,
	}
}

// snippet 截取响应片段用于报错展示。
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
