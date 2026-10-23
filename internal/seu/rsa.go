package seu

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// encryptPassword 使用 CAS 下发的公钥 (PKCS#1 v1.5) 加密密码并 base64 编码,
// 与前端 JSEncrypt 的行为一致。
func encryptPassword(publicKey, password string) (string, error) {
	key, err := parsePublicKey(publicKey)
	if err != nil {
		return "", err
	}
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, key, []byte(password))
	if err != nil {
		return "", fmt.Errorf("密码加密失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// parsePublicKey 解析 CAS 返回的公钥, 支持 PEM 与裸 DER (base64/base64url)。
func parsePublicKey(publicKey string) (*rsa.PublicKey, error) {
	text := strings.TrimSpace(publicKey)
	if text == "" {
		return nil, errors.New("公钥为空")
	}

	if strings.Contains(text, "BEGIN") {
		block, _ := pem.Decode([]byte(text))
		if block == nil {
			return nil, errors.New("公钥 PEM 解析失败")
		}
		return publicKeyFromDER(block.Bytes)
	}

	der, err := decodeBase64Loose(text)
	if err != nil {
		return nil, fmt.Errorf("公钥不是合法的 base64: %w", err)
	}
	return publicKeyFromDER(der)
}

func publicKeyFromDER(der []byte) (*rsa.PublicKey, error) {
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		if key, ok := parsed.(*rsa.PublicKey); ok {
			return key, nil
		}
		return nil, errors.New("公钥不是 RSA 类型")
	}
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("公钥格式无法识别")
}

// decodeBase64Loose 兼容标准/URL 安全字母表与缺失的填充。
func decodeBase64Loose(text string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', ' ':
			return -1
		}
		return r
	}, text)

	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		if der, err := enc.DecodeString(cleaned); err == nil {
			return der, nil
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}
