package pay

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dining-system/internal/store"
)

// ---- 测试辅助 ----

// newTestCert 生成一张自签证书并写入 dir/name,返回「大写十六进制序列号」与私钥。
// 序列号格式与微信回调头 Wechatpay-Serial 保持一致。
func newTestCert(t *testing.T, dir, name string, serial int64) (string, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("生成证书失败: %v", err)
	}
	writePEM(t, filepath.Join(dir, name), "CERTIFICATE", der)
	return big.NewInt(serial).Text(16), priv
}

// writePEM 将 DER 按指定块类型写入 PEM 文件。
func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 %s 失败: %v", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatalf("写入 PEM 失败: %v", err)
	}
}

// initPayDB 为支付测试准备独立的配置库(配置项统一走 store 读取)。
func initPayDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "pay.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// TestCertSerialEqual 回归测试:微信 Wechatpay-Serial 是大写十六进制字符串,
// 而 x509 解析出的是 big.Int —— 必须统一进制比较。
// 历史实现直接比较 big.Int 的十进制字符串,导致回调验签恒不通过。
func TestCertSerialEqual(t *testing.T) {
	n := big.NewInt(0x5157F09E)
	if !certSerialEqual(n, "5157F09E") {
		t.Fatal("大写十六进制序列号应匹配")
	}
	if !certSerialEqual(n, "5157f09e") {
		t.Fatal("小写十六进制序列号应匹配")
	}
	if certSerialEqual(n, n.String()) {
		t.Fatal("十进制字符串不应匹配(说明仍在按十进制比较)")
	}
	if certSerialEqual(n, "") {
		t.Fatal("空序列号不应匹配")
	}
	if certSerialEqual(nil, "5157F09E") {
		t.Fatal("空证书序列号不应匹配")
	}
}

// TestPlatformCertDirRouting 验证平台证书「目录」模式下按序列号选到正确证书。
// 微信换证有 24 小时灰度期,回调会同时出现新旧两个序列号,必须支持多张证书并存。
func TestPlatformCertDirRouting(t *testing.T) {
	initPayDB(t)
	dir := t.TempDir()

	serialOld, privOld := newTestCert(t, dir, "wechatpay_old.pem", 1001)
	serialNew, privNew := newTestCert(t, dir, "wechatpay_new.pem", 2002)

	if err := store.SetCfg("wxpay_platform_cert_path", dir); err != nil {
		t.Fatal(err)
	}

	p := &Wxpay{}
	msg := "1700000000\nnonce123\n{\"id\":\"evt\"}\n"

	pubNew, err := p.platformCertPubKey(serialNew)
	if err != nil {
		t.Fatalf("按新序列号取公钥失败: %v", err)
	}
	if !verifyRSASignature(pubNew, msg, signRSASHA256(privNew, msg)) {
		t.Fatal("新证书验签失败")
	}

	pubOld, err := p.platformCertPubKey(serialOld)
	if err != nil {
		t.Fatalf("按旧序列号取公钥失败: %v", err)
	}
	if !verifyRSASignature(pubOld, msg, signRSASHA256(privOld, msg)) {
		t.Fatal("旧证书验签失败(灰度期新旧证书应并存)")
	}

	// 两张证书必须是不同的公钥,确保路由真的按序列号区分
	if pubNew.N.Cmp(pubOld.N) == 0 {
		t.Fatal("新旧证书公钥相同,路由未生效")
	}

	// 未登记的序列号应明确报错,便于运维定位证书未更新
	if _, err := p.platformCertPubKey("DEADBEEF"); err == nil {
		t.Fatal("未知序列号应报错")
	}
}

// TestPlatformCertSingleFile 验证单个 .pem 文件模式仍然可用(向后兼容)。
func TestPlatformCertSingleFile(t *testing.T) {
	initPayDB(t)
	dir := t.TempDir()
	serial, priv := newTestCert(t, dir, "cert.pem", 3003)
	if err := store.SetCfg("wxpay_platform_cert_path", filepath.Join(dir, "cert.pem")); err != nil {
		t.Fatal(err)
	}

	p := &Wxpay{}
	pub, err := p.platformCertPubKey(serial)
	if err != nil {
		t.Fatalf("单文件模式取公钥失败: %v", err)
	}
	const msg = "m"
	if !verifyRSASignature(pub, msg, signRSASHA256(priv, msg)) {
		t.Fatal("单文件模式验签失败")
	}
}

// TestVerifyPubKeyMode 验证配置微信支付公钥后,回调按公钥模式验签(无需平台证书)。
func TestVerifyPubKeyMode(t *testing.T) {
	initPayDB(t)
	dir := t.TempDir()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPath := filepath.Join(dir, "pub_key.pem")
	writePEM(t, pubPath, "PUBLIC KEY", der)

	const pubKeyID = "PUB_KEY_ID_0112345678901234"
	if err := store.SetCfg("wxpay_pubkey_id", pubKeyID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCfg("wxpay_pubkey_path", pubPath); err != nil {
		t.Fatal(err)
	}

	p := &Wxpay{}
	pub, err := p.verifyPubKey(pubKeyID)
	if err != nil {
		t.Fatalf("公钥模式取公钥失败: %v", err)
	}
	msg := "1700000000\nnonce123\n{\"id\":\"evt\"}\n"
	if !verifyRSASignature(pub, msg, signRSASHA256(priv, msg)) {
		t.Fatal("公钥模式验签失败")
	}

	// 公钥ID 大小写不敏感
	if _, err := p.verifyPubKey("pub_key_id_0112345678901234"); err != nil {
		t.Fatalf("公钥ID 应大小写不敏感: %v", err)
	}

	// 未配置平台证书时,陌生序列号应明确报错而不是静默通过
	if _, err := p.verifyPubKey("UNKNOWN_SERIAL"); err == nil {
		t.Fatal("陌生序列号且无平台证书时应报错")
	}
	// 空序列号直接拒绝
	if _, err := p.verifyPubKey(""); err == nil {
		t.Fatal("空序列号应报错")
	}
}

// TestWxpayEnabledRequiresVerifyMaterial 验证缺少验签材料时不得判定为「已开通」,
// 否则会出现「能下单但回调必然失败」的掉单事故。
func TestWxpayEnabledRequiresVerifyMaterial(t *testing.T) {
	initPayDB(t)
	for k, v := range map[string]string{
		"wxpay_enabled":          "1",
		"wxpay_mchid":            "1900000109",
		"wxpay_apiv3_key":        "0123456789abcdef0123456789abcdef",
		"wxpay_serial_no":        "ABCDEF123456",
		"wxpay_private_key_path": "/tmp/apiclient_key.pem",
	} {
		if err := store.SetCfg(k, v); err != nil {
			t.Fatal(err)
		}
	}

	p := &Wxpay{}
	if p.Enabled() {
		t.Fatal("未配置任何验签材料时不应判定为已开通")
	}
	if err := store.SetCfg("wxpay_platform_cert_path", "/tmp/certs"); err != nil {
		t.Fatal(err)
	}
	if !p.Enabled() {
		t.Fatal("已配置平台证书时应判定为已开通")
	}
}
