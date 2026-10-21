# F13 上下架计划领域生命周期

日期：2026-10-20。依据：DEC-010、详细设计§12.1和原型vPlanCreateForm/vPlanForm/vExecutePlan。

## 本次交付

StatePlan写聚合复用ResourcePlan只读摘要，增加变更前后资源快照。创建校验当前buyer、接入历史和实际状态，记录资源revision/区域、原发起人、原因和预计时间，仅创建计划，不改变资源。

修改/取消按原发起人及计划revision校验，不因当前采购人变化授予代办资格。修改保留原资源依据，返回新版本而不改变原对象。PENDING可修改取消，BLOCKED允许原发起人取消后重新创建捕获新事实，不能直接执行或静默重置依据。

执行生成待持久化方案：原发起人、PENDING、revision匹配、实际操作确认和依据必填。due_at不是执行硬门槛，无自动到期执行。ONLINE复用当前快照判定检查模型、配置、自身有效测试。成功返回EXECUTED计划、executed_at及资源新状态/revision，不修改输入或原报告。

资源依据或上架前提冲突时返回BLOCKED计划副本、失败原因和完成依据，同时返回ErrRevisionConflict。此时不返回资源变更。

## TDD证据

先写5组测试，记录新类型缺失的[编译红色](./evidence/2026-10-20-plan-lifecycle-red.txt)，再实现。

- [最终回归](./evidence/2026-10-20-plan-lifecycle-tests.txt)：supply/API共108组顶层测试通过。
- [vet](./evidence/2026-10-20-plan-lifecycle-vet.txt)、[build](./evidence/2026-10-20-plan-lifecycle-build.txt)：ExitCode=0。

覆盖创建不改资源、绑定创建、原发起人与当前buyer分离、修改取消CAS、终态不可修改、提前执行、实际操作依据、资源冲突BLOCKED及上架测试阻断/成功。Go容器运行，无SQL或浏览器验证，本轮未改前端。

## 后续与事务要求

当前仅纯领域方案，没有事务应用服务、写HTTP和计划办理页面。需继续同事务资源/计划锁、同维度待执行唯一、修订记录、提醒失效、日志/公告/outbox。

BLOCKED+409不能直接从事务回调返回业务错误，否则会回滚失败记录：服务必须先保存BLOCKED、失败依据和日志并提交，再映射409；记录或提交失败则无部分写入。成功执行的计划、资源、日志、公告及outbox须原子提交，A不直接更新客户关系。

到期只提醒，提醒带plan revision，旧版本任务失效。PRICE_CHANGE及运行Docker/公共工作台接入均未完成；领域测试不表示完整计划流程已跑通。
