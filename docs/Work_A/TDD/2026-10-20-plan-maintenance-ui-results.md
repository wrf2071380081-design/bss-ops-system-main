# F13 修改/取消计划页面验证记录

日期：2026-10-20。范围：A 的计划办理子能力，不包含公共工作台整体、真实 SQL 仓储或实际执行登记。

## 依据与契约

原型 v0.22 的 `vPlanEditForm`（651行）编辑原因、预计时间；`vCancelPlan`（541行）由发起人取消计划。沿用已有 A 后端维护契约：

| 操作 | 接口 | 请求字段 |
|---|---|---|
| 修改 | PUT /api/internal/plans/:id | expected_revision、due_at、reason |
| 取消 | POST /api/internal/plans/:id/cancel | expected_revision、reason |

操作者来自认证，不提交 actor/buyer/created_by；请求携带 Idempotency-Key。取消原因用于操作日志，保留原计划原因、预计时间和资源版本依据。修改也不重置原资源版本依据，更不改变资源状态。

能力沿用既有领域/读取约定：原发起人且 PENDING 可修改；PENDING/BLOCKED 可取消；执行入口尚未开放。BLOCKED 可取消是既有后端规则扩展，不是原型中已有的状态。页面依据显式 permissions 展示按钮，缺失能力默认全部关闭。

## 红—绿过程

1. 先新增三个纯契约测试，空模块下全部失败，错误为缺少 buildPlanRevision 等实现，见[契约红阶段](./evidence/2026-10-20-plan-maintenance-ui-red.txt)。
2. 实现构造与回执校验：保持 bigint 字符串精度，检查计划身份、区域、方向、原发起人、原资源依据、下一修订号、时间及原因。
3. 新增真实 Vue 浏览器场景，旧页面找不到“修改计划”按钮而失败，见[页面红阶段](./evidence/2026-10-20-plan-maintenance-browser-red.txt)。
4. 新增 MaintainStatePlanDialog，在 PlanSummaryDialog 接入修改/取消，成功刷新详情并通知 ResourcePlansPanel 重新读取列表。

## 交互与验证

- 修改预填原原因、预计时间；空原因不发送请求；本地时间转换为 UTC。
- 网络503失败保留输入，原提交意图使用相同幂等键重试。
- 403/409进入停止提交状态，保留输入并提示返回详情核对；返回时重新读取计划。浏览器实际覆盖409，403共用错误分支，未单独执行403浏览器场景。
- 修改成功保留原资源依据，并刷新详情和列表。
- 取消先填写留痕原因并确认，成功后隐藏终态操作入口；原计划原因和实际完成时间不被改写。
- 非发起人看不到维护入口，客户端构造器也拒绝缺能力、非发起人、终态和非上下架计划。

最终验证：

| 验证 | 结果/证据 |
|---|---|
| 前端全量测试 | 98项通过，[输出](./evidence/2026-10-20-plan-maintenance-unit.txt) |
| vue-tsc --noEmit | 通过，[输出](./evidence/2026-10-20-plan-maintenance-typecheck.txt) |
| Vite build | 通过，仍有大包提示，[输出](./evidence/2026-10-20-plan-maintenance-build.txt) |
| 修改/取消真实Vue场景 | 通过，[输出](./evidence/2026-10-20-plan-maintenance-browser.txt) |
| 模型广场浏览器回归 | 通过，[输出](./evidence/2026-10-20-plan-maintenance-plaza-regression.txt) |
| 工作台公告/计划浏览器回归 | 通过，[输出](./evidence/2026-10-20-plan-maintenance-workbench-regression.txt) |

[409保留输入截图](./evidence/2026-10-20-plan-maintenance-conflict.png)。浏览器场景挂载真实 Vue 组件，HTTP 使用模拟响应，因此不能证明生产路由、权限认证、真实事务留痕或 Docker 联调完成。

## 待完成

公共增量结构、事务与主装配仍按现有交接需求推进；A 后续接真实仓储。上下架执行登记仍需解决成功/阻断结果先提交后响应的公共事务边界，再开放执行 HTTP/页面。此次未操作运行库、未提交或推送 Git。
