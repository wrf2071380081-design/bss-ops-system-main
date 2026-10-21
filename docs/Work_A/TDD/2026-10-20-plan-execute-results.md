# F13 计划执行事务服务

日期：2026-10-20。承接[生命周期领域](./2026-10-20-plan-lifecycle-results.md)和DEC-010。

## 实现

新增PlanExecutionService.Execute：接收认证WriteMeta、plan_id/revision及实际操作确认/完成依据，事务中读取计划与当前资源一致性事实，复用StatePlan执行规则。操作者必须原发起人，当前资源采购人改变不转移计划办理权限。

成功时CAS保存资源、计划新版本，并追加执行效果；效果端口必须把操作记录、完成依据、自动公告和outbox写入同事务。事件SOURCE由公共适配器分配并防重，影响范围PENDING，A不直接更新客户配置。

资源前提冲突生成BLOCKED，不更新资源；保存计划版本、失败原因、完成依据及日志，且不生成资源变更事件/公告。PlanExecutionResult返回Conflict=true，业务冲突不作为事务回调错误，否则BLOCKED会被回滚。未授权、陈旧计划revision、未确认/缺依据拒绝，不保存BLOCKED代替真正权限错误。

任一资源/计划/效果/提交错误返回nil，所有写入回滚。结果只读Plan摘要，不回传持久化快照。

## TDD验证

先写4个测试组并记录[新端口缺失编译红色](./evidence/2026-10-20-plan-execute-red.txt)，再实现事务服务。

- [回归](./evidence/2026-10-20-plan-execute-tests.txt)：supply/API共112组顶层测试通过。
- [vet](./evidence/2026-10-20-plan-execute-vet.txt)、[build](./evidence/2026-10-20-plan-execute-build.txt)：ExitCode=0。

测试使用内存复制事务，覆盖原发起人与当前buyer不同、执行成功效果、冲突后BLOCKED保留且资源不变、成功及BLOCKED两种路径的计划/资源/日志事件/commit失败回滚，以及未授权/陈旧revision/缺确认/缺依据无写入。Go容器执行，本轮没有前端改动。

## 公共接入要求与剩余工作

LockPlanExecution先解析关联资源，按资源→计划固定顺序加锁并复核关系；创建/修改/取消同样顺序，同一状态维度待执行唯一需要真库约束。没有新建第二套事务/日志工具。

WithinPlanExecution必须加入外层幂等事务；此时服务返回nil只表示已暂存，不能代表外层已经提交。公共HTTP/幂等装配须在外层提交成功后，把Conflict=true映射409并保存该幂等响应；保存BLOCKED时不能把冲突当技术失败回滚。当前公共中间件的响应/提交顺序尚未修复，因此此服务未注册执行HTTP，不声称真库及commit失败HTTP语义已正确。

计划创建/修改/取消应用服务、完整写接口、计划列表和办理页面仍待完成。真实数据库、日志/公告/outbox及Docker联调未完成，测试替身不证明生产事务/锁/事件唯一性。
