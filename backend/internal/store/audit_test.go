package store

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMaskParamsHidesSecrets 审计日志最怕的不是「漏记」,而是「把密钥也记了进来」。
//
// 操作日志页的读者比「能改系统配置」的人多得多(店长也能看),
// 一旦系统配置的提交体原样落库,wxpay_apiv3_key 就变相泄露给了更多人。
// 这个测试把「敏感字段必须脱敏、普通字段必须保留」变成可执行的不变式 ——
// 后者同样重要:只剩一堆 ****** 的日志,排查时毫无用处。
func TestMaskParamsHidesSecrets(t *testing.T) {
	raw := `{"username":"admin","password":"p@ss","oldPassword":"oldPwd","newPassword":"newPwd",
		"wxpay_apiv3_key":"deadbeef","feie_ukey":"ukey-123","wxpay_private_key_path":"/data/key.pem",
		"roleKey":"manager","shop_name":"老王烧烤","items":[{"dishId":1,"remark":"不要辣"}]}`

	got := MaskParams(raw)
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("脱敏结果不是合法 JSON: %v (%s)", err, got)
	}
	// 逐个「值」比对,不能用 strings.Contains(got, "old") ——
	// 键名 oldPassword 本身就含 "old",会误判。
	values := collectStrings(decoded)
	for _, secret := range []string{"p@ss", "oldPwd", "newPwd", "deadbeef", "ukey-123"} {
		for _, v := range values {
			if v == secret {
				t.Errorf("敏感值 %q 未被脱敏: %s", secret, got)
			}
		}
	}
	for _, keep := range []string{"admin", "manager", "老王烧烤", "不要辣", "/data/key.pem"} {
		if !containsValue(values, keep) {
			t.Errorf("非敏感内容 %q 不应被脱敏: %s", keep, got)
		}
	}
	if decoded["password"] != "******" {
		t.Errorf("password 应被替换为 ******,实际 %v", decoded["password"])
	}
	if decoded["roleKey"] != "manager" {
		t.Errorf("roleKey 不是敏感项,应保持原值,实际 %v", decoded["roleKey"])
	}
}

func TestMaskParamsNonJSON(t *testing.T) {
	if got := MaskParams(""); got != "" {
		t.Errorf("空串应返回空串,实际 %q", got)
	}
	if got := MaskParams("not a json"); got != "not a json" {
		t.Errorf("非 JSON 应原样返回,实际 %q", got)
	}
}

// TestMaskParamsRespectsColumnWidth oper_param 列是 VARCHAR(2000):
// MySQL STRICT 模式下超长会拒绝整条 INSERT,审计记录将静默丢失。
// 因此无论输入多长(含截断标记本身),返回值都必须 ≤ 2000 字符。
func TestMaskParamsRespectsColumnWidth(t *testing.T) {
	cases := map[string]string{
		"非JSON纯文本": strings.Repeat("啊", 2500),
		"JSON大对象":    `{"items":[` + strings.Repeat(`{"dishId":1,"specId":2,"quantity":9},`, 200) + `{"dishId":1}]}`,
	}
	for name, raw := range cases {
		got := MaskParams(raw)
		if n := len([]rune(got)); n > 2000 {
			t.Errorf("%s: 截断后 %d 字符仍超出列宽 2000", name, n)
		}
	}
	// 正常长度的 JSON 不应被误截断。
	small := MaskParams(`{"orderId":88}`)
	if !strings.Contains(small, "88") {
		t.Errorf("正常长度 JSON 不应被截断: %s", small)
	}
}

// collectStrings 递归收集 JSON 里的全部字符串值(用于脱敏断言)。
func collectStrings(v interface{}) []string {
	switch t := v.(type) {
	case map[string]interface{}:
		out := []string{}
		for _, x := range t {
			out = append(out, collectStrings(x)...)
		}
		return out
	case []interface{}:
		out := []string{}
		for _, x := range t {
			out = append(out, collectStrings(x)...)
		}
		return out
	case string:
		return []string{t}
	default:
		return nil
	}
}

func containsValue(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
