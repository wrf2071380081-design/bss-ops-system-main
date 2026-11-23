package api

// BuildSHA / BuildAt 由 cmd/server 在启动时通过 SetBuildInfo 注入，
// 值来自 -ldflags "-X main.buildSHA=... -X main.buildAt=..."。
// 暴露成接口是为了让部署能自证"线上跑的是哪个 commit"，
// 避免再出现"本地有、线上没有"却没人发现（2026-11-20）。
var (
	BuildSHA = "dev"
	BuildAt  = "unknown"
)

// SetBuildInfo 由 main 调用，写入构建戳。
func SetBuildInfo(sha, at string) {
	if sha != "" {
		BuildSHA = sha
	}
	if at != "" {
		BuildAt = at
	}
}

// StressTemplateParams 是压测模板参数（启动时从测试平台拉取）。
// 为空时退回内置默认参数，保证平台不可用也能发单。
var StressTemplateParams map[string]any

// SetStressTemplateParams 由 main 在启动时注入。
func SetStressTemplateParams(params map[string]any) {
	if len(params) > 0 {
		StressTemplateParams = params
	}
}
