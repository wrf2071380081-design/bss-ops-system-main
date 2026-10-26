// Package testplatform 是测试平台（open-api v1）的出站客户端。
//
// 只负责"把请求发出去、把响应收回来并翻译成 Go 值"，
// 不含业务判定（谁该测、算不算通过、要不要落库）——那些在 domain/testplatform。
//
// 契约来源：文档资料/开放接口对接说明.md
//   - 基础路径 /api/v1，响应统一信封 {code,message,data,errors}
//   - 鉴权 Authorization: Bearer <token>
//   - 创建任务可带 Idempotency-Key；同键异体返回 409/40902
package testplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config 是连接测试平台的参数。
type Config struct {
	// BaseURL 是站点根，例如 http://10.19.218.33:18080/open-api。
	// 不要求带 /api/v1，带了也不会重复拼接。
	BaseURL string
	// Token 是实名调用方 token（cons- 前缀）。
	Token string
	// Timeout 是单次 HTTP 请求超时，<=0 时取 15s。
	Timeout time.Duration
}

// ErrNotConfigured 表示未配置测试平台地址或 token，调用方应跳过而不是报错。
var ErrNotConfigured = fmt.Errorf("test platform is not configured")

// APIError 是测试平台返回的业务错误（HTTP 非 2xx 或 code != 0）。
type APIError struct {
	HTTPStatus int
	Code       int
	Message    string
	Fields     []FieldError
}

func (e *APIError) Error() string {
	if len(e.Fields) > 0 {
		parts := make([]string, 0, len(e.Fields))
		for _, f := range e.Fields {
			parts = append(parts, f.Field+": "+f.Reason)
		}
		return fmt.Sprintf("test platform %d: %s (%s)", e.HTTPStatus, e.Message, strings.Join(parts, "; "))
	}
	return fmt.Sprintf("test platform %d: %s", e.HTTPStatus, e.Message)
}

// Retryable 表示同样请求稍后重发可能有不同结果（服务端故障/限流）。
// 参数错误、鉴权失败、越权、幂等冲突一律不可重试。
func (e *APIError) Retryable() bool {
	switch e.HTTPStatus {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
		return true
	}
	return false
}

// FieldError 是参数校验失败的字段级原因。
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Client 是测试平台客户端。零值不可用，必须经 New 构造。
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New 构造客户端；地址非法或 token 为空返回 ErrNotConfigured。
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" || strings.TrimSpace(cfg.Token) == "" {
		return nil, ErrNotConfigured
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("test platform base url is invalid: %q", cfg.BaseURL)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{base: base, token: cfg.Token, http: &http.Client{Timeout: timeout}}, nil
}

// endpoint 把 /api/v1 前缀补到 base 上，已带则原样拼接。
func (c *Client) endpoint(path string) string {
	base := c.base
	if !strings.HasSuffix(base, "/api/v1") {
		base += "/api/v1"
	}
	return base + path
}

// envelope 是平台统一响应信封。
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Errors  []FieldError    `json:"errors"`
}

// do 发送请求并解开信封，成功时把 data 反序列化到 out。
func (c *Client) do(ctx context.Context, method, path string, body []byte, idempotencyKey string, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// 报告下载返回裸字节，由 Report 单独处理；这里只处理 JSON。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{HTTPStatus: resp.StatusCode, Message: "响应不是合法 JSON"}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || env.Code != 0 {
		return &APIError{HTTPStatus: resp.StatusCode, Code: env.Code, Message: env.Message, Fields: env.Errors}
	}
	if out == nil {
		return nil
	}
	if len(env.Data) == 0 {
		return &APIError{HTTPStatus: resp.StatusCode, Message: "响应缺少 data"}
	}
	return json.Unmarshal(env.Data, out)
}
