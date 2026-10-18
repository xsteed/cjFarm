package pay

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"dining-system/infrastructure/store/dao"
)

// TestPaySign JSAPI 调起参数的关键约定。
//
// 这一段最容易错、也最难在真机上定位：签名串必须是
// `appId\ntimeStamp\nnonceStr\npackage\n`（package 形如 prepay_id=wx…）。
// 少一个换行、或误用请求头签名串，收银台只会返回「签名错误」，看不出原因。
// 所以这里用公钥把签名验一遍，而不只是断言字段非空。
func TestPaySign(t *testing.T) {
	initPayDB(t)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("编码私钥失败: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "apiclient_key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("写入私钥失败: %v", err)
	}
	if err := dao.SetSetting("wxpay_private_key_path", keyPath); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	params, err := (&Wxpay{}).paySign("wxappid-123", "wx-prepay-abc")
	if err != nil {
		t.Fatalf("签发调起参数失败: %v", err)
	}
	if params.Package != "prepay_id=wx-prepay-abc" {
		t.Fatalf("package 格式错误: %q", params.Package)
	}
	if params.SignType != "RSA" {
		t.Fatalf("signType 应为 RSA, got %q", params.SignType)
	}
	if params.TimeStamp == "" || params.NonceStr == "" || params.PaySign == "" {
		t.Fatalf("调起参数不应有空字段: %+v", params)
	}

	// 微信约定的签名串（末尾必须有换行）
	msg := "wxappid-123" + "\n" + params.TimeStamp + "\n" + params.NonceStr + "\n" + params.Package + "\n"
	if !verifyRSASignature(&priv.PublicKey, msg, params.PaySign) {
		t.Fatal("paySign 与约定的签名串不匹配（检查换行与字段顺序）")
	}
	// 只差一个换行也必须验不过 —— 防止「碰巧能用」的实现混进来
	if verifyRSASignature(&priv.PublicKey, msg[:len(msg)-1], params.PaySign) {
		t.Fatal("缺少末尾换行的签名串不应通过验签")
	}
}

// TestCreateJsapiRequiresOpenID 没有 openid 就不该发请求（微信必拒，还白费一次调用）。
func TestCreateJsapiRequiresOpenID(t *testing.T) {
	initPayDB(t)
	if _, err := (&Wxpay{}).CreateJsapi(PayReq{OrderNo: "D001", AmountCents: 100}); err == nil {
		t.Fatal("缺少 openid 应返回错误")
	}
}

// TestAlipayNoJsapi 支付宝没有「小程序内调起」通道，必须明确报错。
// 返回空结构会让上层以为下单成功，把顾客送进一个永远拉不起的收银台。
func TestAlipayNoJsapi(t *testing.T) {
	initPayDB(t)
	if _, err := (&Alipay{}).CreateJsapi(PayReq{OrderNo: "D001", AmountCents: 100}); err == nil {
		t.Fatal("支付宝不应支持 JSAPI")
	}
}
