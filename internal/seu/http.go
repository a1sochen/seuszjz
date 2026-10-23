package seu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxBody = 8 << 20 // 8MB, 防止异常响应撑爆内存

type request struct {
	method  string
	target  string
	query   url.Values
	json    any
	form    url.Values
	headers map[string]string
	referer string
	follow  bool
}

type response struct {
	status int
	header http.Header
	body   []byte
	url    *url.URL
}

func (r *response) isRedirect() bool {
	switch r.status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// looksLikeJSON 判断响应体是否是 JSON 对象 (会话失效时服务端会返回登录页 HTML)。
func (r *response) looksLikeJSON() bool {
	return strings.HasPrefix(strings.TrimSpace(string(r.body)), "{") ||
		strings.HasPrefix(strings.TrimSpace(string(r.body)), "[")
}

func (r *response) snippet() string {
	text := strings.TrimSpace(string(r.body))
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

// do 执行一次 HTTP 请求。
func (s *Session) do(ctx context.Context, req request) (*response, error) {
	target := req.target
	if len(req.query) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + req.query.Encode()
	}

	var body io.Reader
	contentType := ""
	switch {
	case req.json != nil:
		raw, err := json.Marshal(req.json)
		if err != nil {
			return nil, fmt.Errorf("请求体编码失败: %w", err)
		}
		body = bytes.NewReader(raw)
		contentType = "application/json;charset=UTF-8"
	case req.form != nil:
		body = strings.NewReader(req.form.Encode())
		contentType = "application/x-www-form-urlencoded;charset=UTF-8"
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, target, body)
	if err != nil {
		return nil, fmt.Errorf("请求创建失败: %w", err)
	}
	httpReq.Header.Set("User-Agent", UserAgent)
	httpReq.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for k, v := range req.headers {
		if v == "" {
			continue
		}
		if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		httpReq.Header.Set(k, v)
	}
	if req.referer != "" {
		httpReq.Header.Set("Referer", req.referer)
	}

	client := s.client
	if req.follow {
		client = s.follow
	}

	s.logf("%s %s", req.method, shortenURL(target))
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("响应读取失败: %w", err)
	}
	s.logf("  -> %d %dB", resp.StatusCode, len(data))

	return &response{
		status: resp.StatusCode,
		header: resp.Header,
		body:   data,
		url:    resp.Request.URL,
	}, nil
}

// doJSON 执行请求并把 JSON 响应解析到 out。
func (s *Session) doJSON(ctx context.Context, req request, out any) (*response, error) {
	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return resp, fmt.Errorf("接口返回的不是 JSON (%d): %s", resp.status, resp.snippet())
	}
	return resp, nil
}

func shortenURL(u string) string {
	if len(u) <= 160 {
		return u
	}
	return u[:160] + "..."
}

// messageOf 从业务响应里提取提示文案。
func messageOf(data map[string]any) string {
	for _, key := range []string{"msg", "info", "message", "error"} {
		if v, ok := data[key]; ok {
			if text := rawString(v); text != "" {
				return text
			}
		}
	}
	return ""
}

// rawString 兼容接口中字符串/数字混用的情况。
func rawString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return strings.Trim(string(raw), `"`)
	}
}
