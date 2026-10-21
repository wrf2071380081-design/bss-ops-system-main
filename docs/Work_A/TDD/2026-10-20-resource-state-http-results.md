# F3 直接上下架 HTTP 接入记录

> 当前入口已按用户DEC-010撤下，本文仅保留历史执行事实，见[纠正记录](./2026-10-20-plan-only-correction.md)。

日期：2026-10-20。承接[领域切片](./2026-10-20-resource-state-results.md)。

## 交付

A注册器新增POST `/api/internal/resources/:id/state`，SupplyDeps.ResourceStates注入ResourceStateService或兼容接口。内部认证StaffID为操作者，RequestID来自公共追踪；原因取必填reason。复用公共幂等中间件，不实现第二套幂等存储。

请求严格接受 expected_revision（canonical正bigint字符串）、action（OFFLINE/ONLINE）、external_confirmed=true、completion_note及reason。不接受裸state、actor_id、plan_id或任意查询参数；缺字段、重复键、未知字段、非法JSON按统一严格解析拒绝。ADMIT/RETEST尚未接入此命令，返回400。

响应仅返回id/state/revision/ever_admitted/updated_at收据，不直接序列化领域Resource。检查返回ID、目标状态、revision+1及接入历史；失败不生成成功收据。页面后续需刷新安全详情，而不是用收据猜测报价可用性。

## TDD与验证

先新增3个HTTP测试组，记录缺SupplyDeps.ResourceStates编译失败，再实现端口注入/路由/处理器。

- [红色](./evidence/2026-10-20-resource-state-http-red.txt)
- [最终回归](./evidence/2026-10-20-resource-state-http-tests.txt)：supply/API共104组顶层测试通过。
- [vet](./evidence/2026-10-20-resource-state-http-vet.txt)、[build](./evidence/2026-10-20-resource-state-http-build.txt)：ExitCode=0。

测试使用真实Gin、请求追踪与公共幂等中间件，领域writer/认证上下文/幂等持久化为替身。覆盖认证StaffID与AccountID分离、同键两次仅调用一次writer、收据版本字符串、空/未知/重复字段/非法操作/未确认拒绝、查询plan_id拒绝、缺依赖503、revision冲突409和缺幂等键400。

## 未完成

没有前端按钮或平台操作登记表单；没有主应用/运行Docker API装配，也没有真实事务、日志、公告、outbox适配。公共幂等中间件先输出响应后提交的已知风险仍需公共负责人修复，当前替身测试不证明真库commit失败时HTTP响应一致。

公共装配使用SupplyDeps.ResourceStates=supply.NewResourceStateService(work,clock)。work必须加入外层幂等事务，领域内资源CAS、操作记录、公告与outbox原子提交，不能由adapter独立提交。计划执行不能借该直接buyer入口绕过原发起人授权。
