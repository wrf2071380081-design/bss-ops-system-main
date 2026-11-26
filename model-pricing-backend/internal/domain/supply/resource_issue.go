package supply

import "errors"

// ResourceDataError 说明「一条资源 / 一个标准模型为什么投影不出快照」。
//
// 背景（2026-11-26 事故）：模型广场是「一条脏数据 → 整页 500」的重灾区。
// 这些原因必须能按行返回给前端展示，而不是让整页失败；同时既有调用方仍然用
// errors.Is(err, ErrReadContract) 判断「数据不合契约」，所以 Is 里把两者等价，
// 只有确实需要区分原因的地方才用 errors.As 取 Code/Message。
type ResourceDataError struct {
	Code    string // 稳定的机器码，前端与日志用它区分原因
	Message string // 直接给用户看的中文说明
}

func (e *ResourceDataError) Error() string { return e.Code + ": " + e.Message }

// Is 让 ResourceDataError 同时被认作 ErrReadContract，保持既有调用方语义不变。
func (e *ResourceDataError) Is(target error) bool { return target == ErrReadContract }

func dataIssue(code, message string) *ResourceDataError {
	return &ResourceDataError{Code: code, Message: message}
}

// ResourceIssue 是挂在模型行上、给前端展示的「这条资源有什么问题」。
//
// 它是展示信息，不参与准入/报价判定；统计口径里这些问题行不计入
// 供应来源数、资源数、可报价数与系数区间，避免把没接入完成的数据算成货源。
type ResourceIssue struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	ResourceID   string `json:"resource_id,omitempty"`
	SupplierID   string `json:"supplier_id,omitempty"`
	SupplierName string `json:"supplier_name,omitempty"`
}

// ResourceIssueOf 把内部错误翻译成可展示的问题。
//
// 返回 false 表示这不是「数据问题」（例如连接失败、SQL 语法错误、事务超时），
// 调用方必须继续把它当致命错误抛出，避免把基础设施故障伪装成脏数据。
func ResourceIssueOf(err error) (ResourceIssue, bool) {
	var dataErr *ResourceDataError
	if errors.As(err, &dataErr) {
		return ResourceIssue{Code: dataErr.Code, Message: dataErr.Message}, true
	}
	if errors.Is(err, ErrNotFound) {
		return ResourceIssue{Code: "RESOURCE_REFERENCE_MISSING", Message: "关联数据不存在（标准模型或测试报告已被删除）"}, true
	}
	if errors.Is(err, ErrReadContract) {
		return ResourceIssue{Code: "RESOURCE_DATA_INVALID", Message: "资源数据不符合约定"}, true
	}
	return ResourceIssue{}, false
}
