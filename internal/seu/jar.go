package seu

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/cookiejar"
)

func newCookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

// randomUUID 生成一个 v4 UUID 字符串 (用作 x-csrf-token)。
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// randomHex 生成 n 字节的十六进制串。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i * 7)
		}
	}
	return hex.EncodeToString(b)
}

// newDeviceID 生成 aTrust 网关要求的设备号 ("00" + 66 位十六进制)。
func newDeviceID() string {
	return "00" + randomHex(33)
}
