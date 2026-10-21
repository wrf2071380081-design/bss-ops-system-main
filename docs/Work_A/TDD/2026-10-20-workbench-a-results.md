# 工作台 A 公告阅读与计划摘要执行记录

日期：2026-10-20。依据：[契约](../supply/supply-workbench.draft.md)、[计划](./2026-10-20-workbench-a-plan.md)。

## 交付

- 公告当前人员列表、跨人员阅读状态及详情读取。查看他人仅改变查询投影，认证身份保持不变；默认人员取认证 StaffID。
- 标记已读只写实际操作者的阅读记录，拒绝请求自报 reader/staff/user/reason。首次阅读记录与日志在同一事务端口内写入；重复操作保留首次时间并追加 READ_NOOP，幂等重放不再次调用业务写入。
- 公告影响范围区分待计算与已计算为零，防止未知影响显示为零客户。
- 计划摘要按原创建人返回 is_initiator，所有摘要 read_only=true；未来预计执行时间允许查看，资源 revision 不符只返回提示，读取不执行或修改计划。
- A 注册器新增 notices、notices/read-states、notices/:id、notices/:id/read、plans/:id；缺依赖503，匿名401，其他门户403。

代码位于 `internal/domain/supply/notice*.go`、`plan_read*.go` 和 `internal/api/supply_notice.go`、`supply_plan.go`、`supply_workbench_test.go`。未修改公共 main/router、DDL 或工作台主页面。

## TDD 与回归证据

| 检查 | 实际结果 | 原始输出 |
|---|---|---|
| 领域红色阶段 | 缺少新增领域类型导致编译失败，属于接口先行的红色记录 | [领域红色](./evidence/2026-10-20-workbench-a-red.txt) |
| HTTP 红色阶段 | 缺少 SupplyDeps 的 Notices/NoticeWriter/Plans 导致编译失败 | [HTTP 红色](./evidence/2026-10-20-workbench-a-http-red.txt) |
| supply 全包回归 | 55个顶层测试组通过，语句覆盖率84.9% | [领域回归](./evidence/2026-10-20-workbench-a-domain-regression.txt) |
| API 全包回归 | 31个顶层测试组通过 | [API 回归](./evidence/2026-10-20-workbench-a-api-regression.txt) |
| go vet ./internal/domain/supply ./internal/api | ExitCode=0 | [vet](./evidence/2026-10-20-workbench-a-vet.txt) |
| go build ./... | ExitCode=0 | [build](./evidence/2026-10-20-workbench-a-build.txt) |

本切片新增6个领域顶层测试组及4个HTTP顶层测试组。回归在挂载当前后端目录的 golang:1.26-alpine 容器中运行；没有主机Go环境依赖。本切片未修改前端，也未重新执行前端测试。

三次受控变异分别把“仅本人可标已读”改成所有人、把重复阅读首次时间改成当前时间、把计划发起人改成当前采购人；对应测试均失败，随后恢复源码并运行上述最终回归：

- [查看人员变异](./evidence/2026-10-20-workbench-a-mutation-viewer.txt)
- [首次时间变异](./evidence/2026-10-20-workbench-a-mutation-timestamp.txt)
- [计划发起人变异](./evidence/2026-10-20-workbench-a-mutation-initiator.txt)

## 证据边界与后续接入

事务、仓储和人员目录使用测试替身；并发测试验证服务调用边界，不能证明真实数据库锁和唯一约束。HTTP测试使用真实Gin、读取器和已有幂等中间件，但认证上下文与持久化为替身，不能证明登录令牌、SQL提交及运行环境装配。

需公共方提供目标新表与入口装配；A再实现真实仓储映射及联调。公告/人员组合过滤和总数必须在SQL分页前完成，阅读表需 notice_id/staff_id 唯一约束；公告影响字段映射既有 affected_summary，不自行添加第二套列。事务需加入外层幂等事务，日志失败和提交失败要用真库验证。公共幂等响应先于提交的问题仍见[公共接入清单](../supply/public-integration-request.md)。

计划执行、公告发布与影响范围计算、公共工作台四状态聚合及A前端子面板尚未交付。本记录不表示本机8080/5173已运行这些能力。

本轮重新获取远端 main，仍为 c453affe3f8c7c598f924f3abf433e82b448a2c6；远端分支列表仅main及两个财务分支，本地未定位000033和公共装配代码。用户选择提供公共基座分支或目录，随后询问该术语含义，已解释为共用新表、路由及日志/绑定等代码；当前尚未收到实际位置，不将选择选项视为代码已经存在。
