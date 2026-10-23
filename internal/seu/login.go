package seu

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// stageFunc 接收登录进度回调 (stage 用于分组, msg 为人类可读描述)。
type stageFunc func(stage, msg string)

func (s *Session) progress(fn stageFunc, stage, msg string) {
	if fn != nil {
		fn(stage, msg)
	}
}

// Login 执行完整登录流程: CAS → aTrust 网关 → ehall 应用会话。
func (s *Session) Login(ctx context.Context, progress func(stage, msg string)) error {
	s.Reset()
	if strings.TrimSpace(s.cfg.Username) == "" {
		return &LoginError{Msg: "请填写学号"}
	}
	if s.cfg.Password == "" {
		return &LoginError{Msg: "请填写密码"}
	}
	if err := s.casLogin(ctx, progress); err != nil {
		return err
	}
	if err := s.portalLogin(ctx, progress); err != nil {
		return err
	}
	if err := s.appLogin(ctx, progress); err != nil {
		return err
	}
	s.mu.Lock()
	s.loggedIn = true
	s.mu.Unlock()
	s.progress(progress, "done", "登录成功")
	return nil
}

// casLogin: 统一身份认证, 用 RSA 加密后的密码换取 CAS 会话。
func (s *Session) casLogin(ctx context.Context, progress func(stage, msg string)) error {
	stage := "cas"
	s.progress(progress, stage, "正在验证统一身份认证账号")

	headers := map[string]string{
		"Origin":  AuthBase,
		"Referer": AuthBase + "/dist/",
	}

	if _, err := s.do(ctx, request{method: "GET", target: AuthBase + "/dist/"}); err != nil {
		return &LoginError{Stage: "统一身份认证", Msg: "无法访问认证服务器, 请检查网络: " + err.Error()}
	}

	var tgt map[string]any
	_, err := s.doJSON(ctx, request{
		method:  "POST",
		target:  AuthBase + "/auth/casback/verifyTgt",
		json:    map[string]any{"service": CASService},
		headers: headers,
	}, &tgt)
	if err != nil {
		return &LoginError{Stage: "统一身份认证", Msg: err.Error()}
	}

	var captcha map[string]any
	if _, err := s.doJSON(ctx, request{
		method:  "GET",
		target:  AuthBase + "/auth/casback/needCaptcha",
		headers: headers,
	}, &captcha); err != nil {
		return &LoginError{Stage: "统一身份认证", Msg: err.Error()}
	}
	if truthy(captcha["needStage2Validation"]) {
		return &LoginError{Msg: "该账号需要二次校验 (短信/手机验证码), 协议登录无法完成, 请先在浏览器完成一次登录"}
	}
	if code, ok := captcha["code"]; ok && rawString(code) != "200" && rawString(code) != "" {
		return &LoginError{Stage: "统一身份认证", Msg: "认证前置检查失败: " + rawString(captcha["info"])}
	}

	var keyInfo map[string]any
	if _, err := s.doJSON(ctx, request{
		method:  "POST",
		target:  AuthBase + "/auth/casback/getChiperKey",
		json:    map[string]any{},
		headers: headers,
	}, &keyInfo); err != nil {
		return &LoginError{Stage: "统一身份认证", Msg: err.Error()}
	}
	publicKey := rawString(keyInfo["publicKey"])
	if publicKey == "" {
		return &LoginError{Stage: "统一身份认证", Msg: "获取加密公钥失败"}
	}

	encrypted, err := encryptPassword(publicKey, s.cfg.Password)
	if err != nil {
		return &LoginError{Stage: "统一身份认证", Msg: err.Error()}
	}
	sum := md5.Sum([]byte(s.cfg.Username))
	payload := map[string]any{
		"service":        CASService,
		"username":       s.cfg.Username,
		"password":       encrypted,
		"captcha":        "",
		"rememberMe":     false,
		"loginType":      "account",
		"wxBinded":       false,
		"mobilePhoneNum": "",
		"fingerPrint":    "stable_" + hex.EncodeToString(sum[:]),
	}

	var result map[string]any
	if _, err := s.doJSON(ctx, request{
		method:  "POST",
		target:  AuthBase + "/auth/casback/casLogin",
		json:    payload,
		headers: headers,
	}, &result); err != nil {
		return &LoginError{Stage: "统一身份认证", Msg: err.Error()}
	}
	if !truthy(result["success"]) {
		msg := rawString(result["info"])
		if msg == "" {
			msg = "账号或密码错误"
		}
		return &LoginError{Msg: "登录失败: " + msg}
	}
	if truthy(result["needStage2Validation"]) {
		return &LoginError{Msg: "登录后要求二次校验, 协议登录无法完成"}
	}
	s.progress(progress, stage, "统一身份认证通过")
	return nil
}

// portalLogin: aTrust 网关换票 + 上报设备 + 确认在线。
func (s *Session) portalLogin(ctx context.Context, progress func(stage, msg string)) error {
	stage := "portal"
	s.progress(progress, stage, "正在建立 aTrust 网关会话")

	// 网关接口不接受 CAS 的 Origin 头。
	var ticketInfo map[string]any
	if _, err := s.doJSON(ctx, request{
		method: "POST",
		target: AuthBase + "/auth/casback/verifyTgt",
		json:   map[string]any{"service": CASService},
		headers: map[string]string{
			"Referer": AuthBase + "/dist/",
		},
	}, &ticketInfo); err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: err.Error()}
	}
	redirect := rawString(ticketInfo["redirectUrl"])
	if redirect == "" {
		return &LoginError{Stage: "aTrust 网关", Msg: "CAS 未下发网关票据: " + rawString(ticketInfo["info"])}
	}

	resp, err := s.do(ctx, request{method: "GET", target: redirect})
	if err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: err.Error()}
	}
	location := resp.header.Get("Location")
	if location == "" {
		return &LoginError{Stage: "aTrust 网关", Msg: fmt.Sprintf("网关票据兑换失败 (HTTP %d)", resp.status)}
	}

	portalURL, err := url.Parse(location)
	if err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: "网关返回的门户地址无法解析"}
	}
	var portalData map[string]any
	if err := json.Unmarshal([]byte(portalURL.Query().Get("data")), &portalData); err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: "网关返回的门户参数无法解析"}
	}
	ticket := rawString(portalData["ticket"])
	if ticket == "" {
		return &LoginError{Stage: "aTrust 网关", Msg: "网关票据为空"}
	}

	referer := resolveURL(PortalBase, location)
	s.portalReferer = referer
	if _, err := s.do(ctx, request{method: "GET", target: referer}); err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: err.Error()}
	}

	deviceID := newDeviceID()
	envHeaders := map[string]string{
		"x-csrf-token":  s.csrf,
		"x-sdp-traceid": randomHex(4),
		"Referer":       referer,
	}
	if _, err := s.do(ctx, request{
		method:  "POST",
		target:  PortalBase + "/controller/v1/public/reportEnv",
		query:   portalParams(),
		headers: envHeaders,
		json: map[string]any{
			"ticket":   ticket,
			"deviceId": deviceID,
			"env": map[string]any{
				"endpoint": map[string]any{
					"device_id": deviceID,
					"device":    map[string]any{"type": "browser"},
				},
			},
		},
	}); err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: err.Error()}
	}

	var check map[string]any
	if _, err := s.doJSON(ctx, request{
		method:  "GET",
		target:  PortalBase + "/passport/v1/auth/authCheck",
		query:   portalParams(),
		headers: envHeaders,
	}, &check); err != nil {
		return &LoginError{Stage: "aTrust 网关", Msg: err.Error()}
	}
	if rawString(check["code"]) != "0" {
		return &LoginError{Stage: "aTrust 网关", Msg: "网关在线校验失败: " + rawString(check["message"])}
	}

	onlineInfo, _ := check["data"].(map[string]any)
	info, _ := onlineInfo["onlineInfo"].(map[string]any)
	s.mu.Lock()
	s.userRaw = info
	s.user = mapUserInfo(info, s.cfg.Username)
	s.mu.Unlock()

	name := s.user.DisplayName
	if name == "" {
		name = s.user.Username
	}
	s.progress(progress, stage, "网关已放行: "+name)
	return nil
}

// appLogin: 过网关访问 ehall, 再用应用层 CAS 票据换取 ehall 会话。
//
// ehall 换票链路偶发某一跳长时间无响应 (网关侧), 因此这里做多次重试,
// 每跳使用较短超时, 一旦拿到 GS_SESSIONID 立即停止。
func (s *Session) appLogin(ctx context.Context, progress func(stage, msg string)) error {
	stage := "ehall"
	s.progress(progress, stage, "正在登录 ehall 应用")

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			s.gatewayWarmup(ctx)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		redirect, err := s.appTicket(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		if err := s.followAppTicket(ctx, redirect); err != nil {
			lastErr = err
			continue
		}
		if s.hasCookie("GS_SESSIONID") {
			s.progress(progress, stage, "已获得 ehall 会话")
			return nil
		}
		lastErr = &LoginError{Stage: "ehall", Msg: "没有拿到 ehall 会话 Cookie, 当前: " + strings.Join(s.jar.Names(), ", ")}
	}
	if lastErr == nil {
		lastErr = &LoginError{Stage: "ehall", Msg: "ehall 登录失败"}
	}
	return lastErr
}

// appTicket 申请应用层 CAS 票据。
func (s *Session) appTicket(ctx context.Context) (string, error) {
	var ticketInfo map[string]any
	if _, err := s.doJSON(ctx, request{
		method: "POST",
		target: AuthBase + "/auth/casback/verifyTgt",
		json:   map[string]any{"service": AppService},
		headers: map[string]string{
			"Referer": AuthBase + "/dist/",
		},
	}, &ticketInfo); err != nil {
		return "", &LoginError{Stage: "ehall", Msg: err.Error()}
	}
	redirect := rawString(ticketInfo["redirectUrl"])
	if redirect == "" {
		return "", &LoginError{Stage: "ehall", Msg: "应用票据申请失败: " + rawString(ticketInfo["info"])}
	}
	return redirect, nil
}

// followAppTicket 逐跳跟随后端重定向完成换票。
// 拿到会话后 ehall 会继续重定向到一个会挂起的地址, 所以拿到 Cookie 就停。
func (s *Session) followAppTicket(ctx context.Context, redirect string) error {
	current := redirect
	for hop := 0; hop < 8; hop++ {
		hopCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		resp, err := s.do(hopCtx, request{method: "GET", target: current})
		cancel()
		if err != nil {
			return err
		}
		if s.hasCookie("GS_SESSIONID") {
			return nil
		}
		if !resp.isRedirect() {
			return nil
		}
		next := resolveURL(resp.url.String(), resp.header.Get("Location"))
		if next == "" || next == current {
			return nil
		}
		current = next
	}
	return nil
}

// gatewayWarmup 预访问 ehall 首页, 跟随少量网关跳转 (忽略失败)。
func (s *Session) gatewayWarmup(ctx context.Context) {
	resp, err := s.do(ctx, request{method: "GET", target: AppURL})
	if err != nil {
		return
	}
	for i := 0; i < 6; i++ {
		if !resp.isRedirect() {
			return
		}
		location := resolveURL(resp.url.String(), resp.header.Get("Location"))
		if location == "" || strings.Contains(location, "auth.seu.edu.cn") {
			return
		}
		hopCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		resp, err = s.do(hopCtx, request{method: "GET", target: location})
		cancel()
		if err != nil {
			return
		}
	}
}

func (s *Session) hasCookie(name string) bool {
	for _, n := range s.jar.Names() {
		if n == name {
			return true
		}
	}
	return false
}

// resolveURL 等价于 Python 的 urljoin。
func resolveURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if refURL.IsAbs() {
		return refURL.String()
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(refURL).String()
}

// truthy 处理 JSON 里可能出现的 true/"true"/1/"1"。
func truthy(v any) bool {
	switch rawString(v) {
	case "1", "true", "TRUE", "True", "yes":
		return true
	}
	return false
}

// mapUserInfo 把网关返回的 onlineInfo 映射成固定结构。
func mapUserInfo(raw map[string]any, fallback string) UserInfo {
	info := UserInfo{
		Username:    rawString(raw["username"]),
		DisplayName: rawString(raw["displayName"]),
		ClientIP:    rawString(raw["clientIp"]),
		LoginTime:   rawString(raw["loginTime"]),
		Online:      truthy(raw["isOnline"]),
	}
	if info.Username == "" {
		info.Username = fallback
	}
	if info.DisplayName == "" {
		info.DisplayName = info.Username
	}
	return info
}
