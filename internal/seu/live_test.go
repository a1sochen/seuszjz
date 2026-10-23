//go:build live

// 真实服务器联调测试: 需要联网与有效账号。
// 运行: $env:SEU_USER="..."; $env:SEU_PASS="..."; go test -tags live ./internal/seu -run TestLive -v
package seu

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	user, pass := os.Getenv("SEU_USER"), os.Getenv("SEU_PASS")
	if user == "" || pass == "" {
		t.Skip("缺少 SEU_USER / SEU_PASS 环境变量")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	session := NewSession(Config{Username: user, Password: pass, Debug: true, Logf: func(format string, args ...any) {
		t.Logf(format, args...)
	}})
	steps := []string{}
	if err := session.Login(ctx, func(stage, msg string) {
		steps = append(steps, stage+": "+msg)
	}); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	t.Logf("登录成功: %+v", session.User())

	lectures, clock, err := session.Activities(ctx)
	if err != nil {
		t.Fatalf("讲座列表失败: %v", err)
	}
	t.Logf("服务器时间 %s, 共 %d 条讲座", clock.Format(TimeLayout), len(lectures))
	for _, l := range lectures {
		t.Logf("[%s] %s | %s | %s ~ %s | 名额 %d/%d | 状态 %s",
			l.WID[:8], l.Name, l.Venue, l.BookStartAt, l.BookEndAt, l.Booked, l.Capacity, l.Status)
	}

	my, _, err := session.MyActivities(ctx)
	if err != nil {
		t.Fatalf("我的预约失败: %v", err)
	}
	t.Logf("我的预约 %d 条", len(my))
	for _, m := range my {
		t.Logf("  %s | %s | 预约码 %s | 状态 %s", m.Name, m.StartAt, m.Code, m.Status)
	}

	notices, _, err := session.Notices(ctx)
	if err != nil {
		t.Fatalf("通知公告失败: %v", err)
	}
	t.Logf("通知公告 %d 条", len(notices))
	for _, n := range notices {
		t.Logf("  [%s] %s", n.Date, n.Title)
	}

	if len(lectures) > 0 {
		res, err := session.CheckAppointment(ctx, lectures[0].WID)
		if err != nil {
			t.Fatalf("预约前置校验失败: %v", err)
		}
		t.Logf("前置校验: success=%v kind=%s msg=%s", res.Success, res.Kind, res.Message)

		image, err := session.FetchCaptcha(ctx)
		if err != nil {
			t.Fatalf("验证码获取失败: %v", err)
		}
		path := filepath.Join(os.TempDir(), "seu_vcode_live.jpg")
		if err := os.WriteFile(path, image, 0o644); err != nil {
			t.Fatalf("验证码保存失败: %v", err)
		}
		t.Logf("验证码已保存: %s (%d 字节)", path, len(image))

		// 用错误验证码提交一次, 观察服务端返回文案 (不会产生预约)。
		res, err = session.Reserve(ctx, lectures[0].WID, "0000")
		if err != nil {
			t.Fatalf("预约请求异常: %v", err)
		}
		t.Logf("错误验证码提交: success=%v kind=%s msg=%s", res.Success, res.Kind, res.Message)
	}

	// 导出/导入 Cookie 后应能直接复用会话
	cookies := session.Cookies()
	blob, _ := json.MarshalIndent(cookies, "", " ")
	t.Logf("会话 Cookie 共 %d 项:\n%s", len(cookies), string(blob))

	restored := NewSession(Config{Username: user, Password: pass})
	restored.LoadCookies(cookies)
	if _, _, err := restored.Activities(ctx); err != nil {
		t.Fatalf("复用 Cookie 失败: %v", err)
	}
	t.Logf("Cookie 复用成功")
}
