# F13 计划创建、修改、取消事务服务

日期：2026-10-20。依据DEC-010及详细设计§12.1，承接[领域规则](./2026-10-20-plan-lifecycle-results.md)。

## 实现

新增PlanMaintenanceService.Create/Revise/Cancel及事务端口。创建校验认证当前buyer与资源expected_revision，锁资源后检查同一ONLINE/OFFLINE维度是否已有PENDING/EXECUTING计划，分配ID，生成计划快照，写计划/修订/日志/提醒效果，不更新资源实际状态。

修改/取消从服务端锁定原计划，核对原发起人与expected_revision，不让当前新buyer代办。修改保留资源原依据，只更改预计时间/原因并升计划revision；取消释放待执行维度。返回只读ResourcePlan摘要，不回显写聚合。

PlanMaintenanceEffect要求事务适配器追加不可变修订、操作日志和提醒outbox。每次升revision使旧提醒失效，修改重新排期，取消不再排期；worker执行提醒时必须复核plan ID/revision/status，不能改变resource.state。所有效果与计划同事务，失败或commit失败返回nil。

## TDD及证据

先写4组测试，[红色](./evidence/2026-10-20-plan-maintenance-red.txt)为缺少新服务/端口的编译失败，再实现。

- [最终回归](./evidence/2026-10-20-plan-maintenance-tests.txt)：supply/API共116组顶层测试通过。
- [vet](./evidence/2026-10-20-plan-maintenance-vet.txt)、[build](./evidence/2026-10-20-plan-maintenance-build.txt)：ExitCode=0。

覆盖创建不改资源/返回副本/提醒意图、重复计划拒绝、原发起人对比当前buyer、修改前后revision、过期取消拒绝、取消停止提醒、创建/修改/取消各自日志效果与commit失败回滚、陈旧资源/错误创建人无写入。

Go容器执行，内存复制事务。测试验证效果意图，不代表真实worker已实现或提醒已送达；没有SQL唯一约束和实际并发验证。本轮未改前端。

## 接入边界

创建与办理统一资源→计划锁顺序，状态维度唯一必须在数据库边界强制，不只依赖先查布尔值。UnitOfWork加入外层幂等事务，不能独立提交；外层提交前不能向客户端承诺成功。

下一步补A写HTTP、计划列表/权限投影与原型页面；计划执行Conflict=true响应须公共幂等提交机制配合。真实新表、日志、提醒outbox/worker、主入口和Docker仍未装配。PRICE_CHANGE不是当前状态计划服务支持范围。
