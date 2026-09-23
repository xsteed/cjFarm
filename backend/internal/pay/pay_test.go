package pay

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"testing"
)

// TestRSA_SignAndVerify 验证 RSA-SHA256 签名验签闭环(微信/支付宝共用)。
func TestRSA_SignAndVerify(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	msg := "POST\n/v3/pay/transactions/native\n1700000000\nabc123\n{\"a\":1}\n"
	sig := signRSASHA256(priv, msg)
	if sig == "" {
		t.Fatal("签名结果为空")
	}
	if !verifyRSASignature(&priv.PublicKey, msg, sig) {
		t.Fatal("合法签名验签失败")
	}
	if verifyRSASignature(&priv.PublicKey, msg+"x", sig) {
		t.Fatal("篡改消息仍验签通过")
	}
	// 签名串必须是 base64
	if _, err := base64.StdEncoding.DecodeString(sig); err != nil {
		t.Fatalf("签名不是合法 base64: %v", err)
	}
}

// TestAESGCM_Decrypt 验证微信 APIv3 回调 resource 的 AES-256-GCM 解密。
func TestAESGCM_Decrypt(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	aad := []byte("transaction")
	plain := []byte(`{"out_trade_no":"D123","trade_state":"SUCCESS","amount":{"total":100}}`)

	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	ct := gcm.Seal(nil, nonce, plain, aad)

	got, err := decryptAESGCM(string(key), string(nonce), string(aad), base64.StdEncoding.EncodeToString(ct))
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("解密结果不一致: got %s", string(got))
	}
}

// TestAmountConvert 验证金额单位转换(分<->元)。
func TestAmountConvert(t *testing.T) {
	if v := centsToYuan(1234); v != "12.34" {
		t.Fatalf("centsToYuan(1234)=%s 期望 12.34", v)
	}
	if v := centsToYuan(2800); v != "28.00" {
		t.Fatalf("centsToYuan(2800)=%s 期望 28.00", v)
	}
	if v, err := yuanToCents("12.34"); err != nil || v != 1234 {
		t.Fatalf("yuanToCents(12.34)=%d err=%v 期望 1234", v, err)
	}
	if _, err := yuanToCents(""); err == nil {
		t.Fatalf("yuanToCents(\"\") 应报错,而不是静默返回 0")
	}
}
