package testplatform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tpdomain "model_bss/internal/domain/testplatform"
)

// 回调验签失败的两类原因，供 HTTP 层区分（都按 401 处理，但要能查日志）。
var (
	ErrUnsignedCallback = fmt.Errorf("%w: signature missing", tpdomain.ErrSignature)
	ErrInvalidSignature = fmt.Errorf("%w: signature invalid", tpdomain.ErrSignature)
)

// signatureTolerance 是契约规定的 ±5 分钟时间窗。
const signatureTolerance = 300 * time.Second

// Receiver 接收平台终态回调：验签 → 幂等 → 收口。
//
// 必须用**原始请求体字节**验签：反序列化后重新序列化会因空白/字段顺序不同导致签名不符。
type Receiver struct {
	secret    string
	service   *tpdomain.Service
	clock     func() time.Time
	tolerance time.Duration
}

// NewReceiver 构造接收器；secret 为空返回 ErrNotConfigured。
func NewReceiver(secret string, service *tpdomain.Service, clock func() time.Time) (*Receiver, error) {
	if strings.TrimSpace(secret) == "" || service == nil {
		return nil, tpdomain.ErrNotConfigured
	}
	if clock == nil {
		clock = time.Now
	}
	return &Receiver{secret: secret, service: service, clock: clock, tolerance: signatureTolerance}, nil
}

// callbackEvent 是回调信封里本系统关心的部分。
type callbackEvent struct {
	Event   string `json:"event"`
	EventID string `json:"event_id"`
	Job     struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	} `json:"job"`
}

// Receive 处理一次回调投递。duplicate=true 表示重复投递，调用方仍应回 2xx。
func (r *Receiver) Receive(ctx context.Context, body []byte, signature string) (bool, error) {
	if err := r.verify(body, signature); err != nil {
		return false, err
	}
	var event callbackEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return false, fmt.Errorf("%w: body is not valid json", tpdomain.ErrInvalidCommand)
	}
	if strings.TrimSpace(event.Job.JobID) == "" {
		return false, fmt.Errorf("%w: job.job_id is required", tpdomain.ErrInvalidCommand)
	}
	return r.service.ApplyCallback(ctx, event.EventID, event.Job.JobID, body)
}

// verify 校验 X-Signature：t=<unix 秒>,v1=<hex(HMAC-SHA256(secret, "<t>."+body))>。
func (r *Receiver) verify(body []byte, signature string) error {
	if strings.TrimSpace(signature) == "" {
		return ErrUnsignedCallback
	}
	var stamp string
	var digest string
	for _, part := range strings.Split(signature, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			stamp = value
		case "v1":
			digest = value
		}
	}
	if stamp == "" || digest == "" {
		return ErrInvalidSignature
	}
	unix, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	// 时间窗防重放：|now - t| ≤ 300s。
	if diff := r.clock().Sub(time.Unix(unix, 0)); diff > r.tolerance || diff < -r.tolerance {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(r.secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	// 常量时间比较，避免时序侧信道。
	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(digest))) {
		return ErrInvalidSignature
	}
	return nil
}
