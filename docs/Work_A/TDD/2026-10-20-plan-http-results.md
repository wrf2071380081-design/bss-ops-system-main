# F13 计划维护 HTTP 切片

日期：2026-10-20。依据DEC-010、[事务服务](./2026-10-20-plan-maintenance-results.md)。

## 接口交付

A注册器增加SupplyDeps.PlanWriter，接PlanMaintenanceService：

| 接口 | 严格请求字段 |
|---|---|
| POST /api/internal/plans | resource_id、expected_resource_revision、kind(ONLINE/OFFLINE)、due_at、reason |
| PUT /api/internal/plans/:id | expected_revision、due_at、reason |
| POST /api/internal/plans/:id/cancel | expected_revision、reason |

ID/revision为canonical正bigint字符串；日期为带时区RFC3339时间，转UTC。未知/重复/非规范字段、缺字段、无效类型和查询参数拒绝；不接受created_by/actor_id/状态/快照或PRICE_CHANGE替代请求。操作者来自认证StaffID，RequestID来自公共追踪，统一原因校验。复用已有幂等中间件，无独立幂等实现。

结果返回ResourcePlan安全摘要，不回显StatePlan原始快照；核对返回对象、状态、版本和原发起人等关键字段。匿名/其他门户由A注册器统一拒绝，业务层继续强制权限和CAS。缺writer503，非发起人/非绑定创建403，冲突409。

## TDD证据

先写4组HTTP测试，记录缺PlanWriter的[编译红色](./evidence/2026-10-20-plan-http-red.txt)，再实现。

- [最终回归](./evidence/2026-10-20-plan-http-tests.txt)：supply/API共120组顶层测试通过。
- [vet](./evidence/2026-10-20-plan-http-vet.txt)、[build](./evidence/2026-10-20-plan-http-build.txt)：ExitCode=0。

测试使用真实Gin、请求追踪和公共幂等中间件，认证/业务writer/幂等持久化是替身。覆盖创建同键两次只调用一次服务、StaffID与AccountID分离、修改/取消传递计划revision、拒绝自报created_by和非法日期/数字revision/不支持种类、缺依赖及403/409映射。Go容器执行，本轮未改前端。

## 尚未完成

没有计划执行写HTTP；其Conflict=true结果需公共提交机制配合，不能把BLOCKED回滚。现有公共幂等中间件响应先于提交的风险仍待公共方修复，当前维护接口替身测试不能证明真库commit失败响应一致。

没有真实仓储、新表/修订/日志/提醒outbox适配、生产主装配或Docker联调。计划列表、可办理权限投影和创建/办理页面仍需继续完成；只读PlanSummary未被改成可执行权限承诺。
