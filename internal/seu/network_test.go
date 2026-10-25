package seu

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestParseNetMode 检查配置字符串到 NetMode 的映射, 未知值和空值都归一到 auto。
func TestParseNetMode(t *testing.T) {
	cases := map[string]NetMode{
		"":           NetAuto,
		"auto":       NetAuto,
		" campus ":   NetCampus,
		"campus":     NetCampus,
		"offcampus":  NetOffCampus,
		"OFFCAMPUS":  NetAuto, // 大小写不匹配时按默认处理, 不猜
		"something ": NetAuto,
	}
	for in, want := range cases {
		if got := ParseNetMode(in); got != want {
			t.Errorf("ParseNetMode(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestResolveNetModeForced 检查手动指定网络环境时不再做探测。
func TestResolveNetModeForced(t *testing.T) {
	for _, mode := range []NetMode{NetCampus, NetOffCampus} {
		sess := NewSession(Config{Username: "u", Password: "p", NetMode: mode})
		if got := sess.resolveNetMode(context.Background()); got != mode {
			t.Errorf("强制 %q 时 resolveNetMode = %q", mode, got)
		}
		if got := sess.Network(); got != mode {
			t.Errorf("Network() = %q, 期望 %q", got, mode)
		}
	}
}

// TestResolveNetModeAuto 检查 auto 模式一定会落到两条路之一, 不会留空。
func TestResolveNetModeAuto(t *testing.T) {
	sess := NewSession(Config{Username: "u", Password: "p"})
	got := sess.resolveNetMode(context.Background())
	if got != NetCampus && got != NetOffCampus {
		t.Fatalf("auto 探测结果 %q 不是有效路径", got)
	}
	if sess.Network() != got {
		t.Fatalf("Network() 与探测结果不一致: %q vs %q", sess.Network(), got)
	}
}

// TestIsUnreachable 是这次改造的关键判定: 只有网络层的「这条路不通」才换路。
func TestIsUnreachable(t *testing.T) {
	if IsUnreachable(nil) {
		t.Fatal("nil 不应算不可达")
	}
	if IsUnreachable(context.Canceled) {
		t.Fatal("用户主动取消不应算不可达, 否则会白换一条路")
	}
	if !IsUnreachable(context.DeadlineExceeded) {
		t.Fatal("超时应当算不可达")
	}
	// 业务失败: 网关活着但不放行, 换路没有意义。
	business := &LoginError{Stage: "aTrust 网关", Msg: "网关在线校验失败: ticket 无效"}
	if IsUnreachable(business) {
		t.Fatal("业务错误不应算不可达")
	}
	if IsUnreachable(&LoginError{Msg: "登录失败: 密码错误"}) {
		t.Fatal("密码错误不应算不可达")
	}

	// 真实的拒绝连接要能被识别, 并且要穿透 fmt.Errorf 与 LoginError 两层包装。
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1", 2*time.Second)
	if err == nil {
		_ = conn.Close()
		t.Skip("本机 127.0.0.1:1 居然连得上, 跳过网络错误用例")
	}
	wrapped := stageError("aTrust 网关", fmt.Errorf("网络请求失败: %w", err))
	if !IsUnreachable(wrapped) {
		t.Fatalf("拒绝连接应算不可达, 实际未识别: %v", wrapped)
	}
}
