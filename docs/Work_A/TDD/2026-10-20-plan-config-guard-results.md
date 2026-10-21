# F13 创建计划的待应用配置变更门槛

日期：2026-10-20。原型依据：v0.22第539行vCreatePlan明确阻止“已有待执行计划或已通过但尚未应用的配置变更”。此前PlanMaintenanceService仅检查前者，后者遗漏，本次修正。

## 变更

PlanMaintenanceTransaction新增HasPendingConfigurationChange(ctx, resourceID)，由B提供当前资源已通过但尚未应用的变更事实。Create在同一资源锁下检查计划冲突后查询该事实，再分配计划ID/写计划。结果为true返回ErrRevisionConflict；依赖错误原样返回，不当成false。创建失败不写计划/提醒/日志，也不修改资源。

此项查询是必须实现的事务端口，不设置“缺实现则允许创建”降级。B审批/应用配置变更与A计划创建需统一资源锁或等价并发约束，避免先检查无变更、随后双方同时创建/应用的竞态。禁止锁内远程HTTP；B提供同事务查询能力或稳定事务适配。

## TDD证据

新增TestStatePlanCreationBlocksUnappliedConfigurationChange，覆盖pending和unavailable两条路径；先运行记录[业务断言红色](./evidence/2026-10-20-plan-config-guard-red.txt)，证明原代码在两种情况下都错误创建计划。随后增加端口及调用。

- [最终回归](./evidence/2026-10-20-plan-config-guard-tests.txt)：supply/API共125组顶层测试通过。
- [vet](./evidence/2026-10-20-plan-config-guard-vet.txt)、[build](./evidence/2026-10-20-plan-config-guard-build.txt)：ExitCode=0。

Go容器执行，B查询和事务使用内存替身；尚不能证明真实B状态映射、SQL及并发锁正确。原有前端计划列表改动保留，本轮因原型门槛遗漏先补后端；创建计划页面尚未交付。修改均保留工作区，未提交或推送。
