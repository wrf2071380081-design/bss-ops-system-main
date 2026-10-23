package testplatform

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// TargetFingerprint 计算「测试目标」的指纹（Q6 决策）。
//
// 参与计算的只有"测的是什么"：
//   - 标准模型 ID
//   - 接口模型名（供应商自己的模型标识）
//   - 测试地址
//   - 凭据（用密文，不用明文——加密带随机 nonce，同一 Key 只写一次，密文稳定）
//
// 刻意不含采购系数与报价：改价不代表测试目标变了，否则供应商每改一次价都要重测。
//
// 入参统一做 trim/小写归一，避免大小写或尾部斜杠造成"假失效"。
func TargetFingerprint(standardModelID int64, supplierModelName, baseURL, credentialRef string) string {
	parts := []string{
		strconv.FormatInt(standardModelID, 10),
		strings.ToLower(strings.TrimSpace(supplierModelName)),
		strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/"),
		strings.TrimSpace(credentialRef),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
