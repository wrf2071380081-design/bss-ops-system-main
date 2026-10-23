package supplysecurity

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"model_bss/internal/domain/supply"
)

// testCredentialPurpose 是测试凭据的绑定用途，写入与解开必须一致。
const testCredentialPurpose = "test-api-key"

// testCredentialConfigID 是绑定里的配置占位：测试凭据不属于任何正式配置，
// 但 A 侧的 validBinding 要求三段都非空，所以用一个固定值占位。
const testCredentialConfigID = "test-platform"

// TestCredentialBinding 返回测试凭据的绑定参数。写入与解密都走这里，避免两边写歪。
func TestCredentialBinding(supplierID string) supply.SecretBinding {
	return supply.SecretBinding{
		Purpose:    testCredentialPurpose,
		SupplierID: supplierID,
		ConfigID:   testCredentialConfigID,
	}
}

// EncodeTestCredential 把信封编码成 `<keyVersion>:<base64(ciphertext)>` 存库。
func EncodeTestCredential(envelope supply.SecretEnvelope) string {
	return envelope.KeyVersion + ":" + base64.StdEncoding.EncodeToString(envelope.Ciphertext)
}

// ErrTestCredentialUnavailable 表示凭据不可解（未配置密钥、格式不符或绑定不匹配）。
var ErrTestCredentialUnavailable = errors.New("test credential is unavailable")

// DecodeTestCredential 解出供应商的测试 API Key。
// 仅供测试平台对接使用：这是全系统唯一会把凭据还原成明文的地方。
func DecodeTestCredential(ctx context.Context, sealer *AESGCMSealer, stored, supplierID string) (string, error) {
	if sealer == nil || strings.TrimSpace(stored) == "" || strings.TrimSpace(supplierID) == "" {
		return "", ErrTestCredentialUnavailable
	}
	version, payload, found := strings.Cut(stored, ":")
	if !found {
		return "", ErrTestCredentialUnavailable
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", ErrTestCredentialUnavailable
	}
	plaintext, err := sealer.Open(ctx, supply.SecretEnvelope{KeyVersion: version, Ciphertext: raw}, TestCredentialBinding(supplierID))
	if err != nil {
		return "", ErrTestCredentialUnavailable
	}
	return plaintext, nil
}
