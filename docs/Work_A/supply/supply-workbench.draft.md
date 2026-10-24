# 工作台 A 子能力：公告阅读与计划摘要

2026-10-23需求补充：以[最新需求结论](./2026-10-21-requirements-and-plan.md)及DEC-016为当前依据。取消独立审批中心，供应商报价共享审批展示在有审批资格领导的工作台待办，详情进入原审批/分派流程。流程、授权和共享审批的一次性决策不变；普通执行待办仍单人绑定，一人审批及分派完成后才出现后续个人待办，由报价模块提供事实、工作台聚合。合并展示不合并两类任务的办理规则，人员筛选不产生审批或代办资格。

依据：v0.22 原型1263—1267行、A详细设计v1.1 §9.1/12.2、附01共享契约。本文只定义A提供的公告/计划事实；工作台聚合由共享工作台能力承接，项目没有独立第四位“公共负责人”。

## 1. 读取契约

| 接口 | 入参 | 结果 |
|---|---|---|
| GET `/api/internal/plans` | resource_id、region、kind、status、page/size | 当前资源前提与原发起人的计划只读分页 |
| GET `/api/internal/notices` | region、resource_id、read_status=ALL/UNREAD/READ、keyword、page/size | 当前认证StaffID的公告投影 |
| GET `/api/internal/notices/read-states` | 上述条件、staff_ids（逗号分隔）、all_staff=true | 指定人员或全部内部人员的阅读投影；省略人员条件默认为当前人员 |
| GET `/api/internal/notices/:id` | 可选staff_id | 公告及该人员阅读详情；省略staff_id看当前人 |
| GET `/api/internal/plans/:id` | 无人员替换参数 | 发起人、资源、预计/实际时间、修订号、状态及当前前提是否匹配的只读摘要 |

所有读取仅限认证内部人员，全员可读，不套旧角色域。staff_ids与all_staff不可同时使用；显式ID最多100个，重复/非法ID拒绝。全部人员通过真实内部人员目录查询，不以空数组猜测全集。分页对象为notice/person组合，total不是去重公告数；排序published_at desc、notice_id desc、staff_id asc，先过滤阅读状态再分页。

默认查询当前登录人员；切换人员不切换认证身份。进入待阅的业务消息覆盖全部在用内部人员，不按业务类型、岗位或审批资格缩小范围；每人独立标记自己的已阅，查看他人状态不能代替其写入。此规则由公共工作台聚合A/B/C的待阅事项；A的notice接口只提供A公告事实，不替B/C生成消息。普通执行待办仍各自只绑定一名办理人，其他员工可按姓名查看待办及详情、进入业务页，但不能办理或改绑定人。供应商报价共享审批候选只向有审批资格者显示。当前执行任务不重选办理人；审批分派或明确交接才选择后续办理人。

投影包含notice、staff_id/name、read_status、可空read_at、permissions.can_mark_read。仅查看自己且未读时可标记已读；查询他人不能切换认证身份。关联客户影响PENDING不能显示成0；READY时明确数量。

## 2. 标记已读

POST `/api/internal/notices/:id/read` 允许空body或`{}`，拒绝自报reader_id/staff_id/user_id等字段。Actor从认证StaffID读取，操作说明由服务端设为“阅读公告”，仍要求Idempotency-Key与请求追踪。

新请求同事务读取公告、核对内部人员、锁定/创建个人阅读行并追加操作记录。唯一键notice_id/staff_id由数据库强制。首次READ写阅读行，重复READ_NOOP保留首次read_at且不重复阅读行；新的请求意图仍留痕。公共幂等重放不再次执行业务。日志/提交失败必须整体回滚，不返回成功对象。

## 3. 计划只读摘要

计划发起人created_by与当前资源buyer_id分开。返回is_initiator仅说明发起人身份，不授权查看者代办。摘要始终read_only=true，表示读取本身无副作用；办理能力通过独立permissions字段声明，不能仅据read_only推断是否可打开维护表单。

permissions.can_revise仅原发起人且PENDING的上下架计划为true；can_cancel允许原发起人的PENDING/BLOCKED上下架计划；can_execute仅原发起人的PENDING上下架计划可用，API执行服务仍会再次校验身份。前端缺失或不匹配permissions时全部关闭，不根据资源当前buyer或所查看人员自行授权。修改仅提交expected_revision/due_at/reason；取消提交expected_revision/reason，取消原因写操作日志，不覆盖原计划原因。修改不更换原资源版本依据。

前端已在计划详情提供修改/取消/实际完成入口（按显式能力展示），沿用原型vPlanEditForm和实际完成确认字段。网络失败保留输入和同一提交意图的幂等键；403/409停止提交，返回详情重新读取后核对；成功重新加载详情及资源计划列表。执行器与409终态重放已接入源码，Go/Docker联合验收仍待完成。见[页面验证记录](../TDD/SUMMARY.md#stage-23)及[Stage58](../TDD/SUMMARY.md#stage-58-a-侧配置安全计划执行与提醒源码接线2026-10-22)。

预计时间仅用于追踪，不因尚未到期拒绝读取；当前资源revision与计划预期不同则返回precondition_matches=false和阻断原因，GET不修改计划状态。未来F13执行服务仍须验证发起人、实际外部操作确认、资源前提及共享事务。

## 4. 公共/真实适配要求

2026-10-22交接更新：公告个人阅读、计划读取/创建/修改/取消已经正式装配并在真实认证/页面/API/隔离v36库验证（Stage54）；供应来源指定plan_id导航已真实验证（Stage57）。计划执行器、BLOCKED提交后的409终态重放和提醒worker已接入源码。工作台计划事项由A仓储直接从原生`plan`表投影稳定`plan_id`，其他事项仍读公共`v_work_item`视图；不依赖时间戳反查，也不改共享迁移。该查询修正在Stage67补了定向回归并部署后，仍需用真实计划数据验证工作台跳转和提醒投递。详细责任见[交接清单](./2026-10-22-handoff.md)，验收见[TDD汇总](../TDD/SUMMARY.md)。

阅读写入由supplypg.NoticeUnitOfWork提供：逐请求解析外层*sql.Tx，注入A日志函数，公告/人员/阅读锁及首次写入参与同事务；Stage54已在真实隔离库验证首次/NOOP、日志失败回滚、外层回滚及真实页面/API阅读状态。适配器不独立提交；缺事务或日志函数即失败。运行组合已注入NoticeWriter。

公告读取通过supplypg.NewNoticeReadStore(db)注入NoticeReader，按[存储契约草案](./notice-storage.draft.md)读取版本化A公告，在只读REPEATABLE READ快照内检查人员、计数和分页；真实A HTTP/领域/SQL测试及Stage54真实页面/API写入已通过。该接口不自动转换旧payload，运行库不因本项另加迁移。

创建计划另需阻止B已通过但尚未应用的配置变更（原型vCreatePlan第539行）。PlanMaintenanceTransaction.HasPendingConfigurationChange在同资源锁内查询，true返回409，依赖错误阻断；B审批/应用与A创建计划统一锁顺序。不能把缺实现或查询失败视为没有变更，见[修正记录](../TDD/SUMMARY.md#stage-15)。

计划列表由原生PlanStore读取，缺能力时503；created_at DESC、数值id DESC稳定排序，过滤先于分页，total为计划数。每条计划携带当前资源一致性事实；同详情共享校验与只读投影，不按buyer限制内部读取。SQL/运行装配和Stage54真实页面/API计划维护已验证，新增执行路径见Stage58。

计划维护HTTP提供POST plans、PUT plans/:id、POST plans/:id/cancel，注入SupplyDeps.PlanWriter=PlanMaintenanceService。创建字段resource_id/expected_resource_revision/kind/due_at/reason，修改expected_revision/due_at/reason，取消expected_revision/reason；ID/revision字符串，日期RFC3339，拒绝自报操作者。执行HTTP由运行组合注入PlanExecutor，原发起人PENDING上下架计划可执行；BLOCKED回执以409终态快照保存/重放。新增后端尚待Go编译及Docker验收。

PlanMaintenanceService已提供Create/Revise/Cancel事务服务，创建按当前buyer，修改/取消按原发起人；同状态维度唯一、修订/日志与提醒意图同事务。SQL及创建/修改/取消页面已运行，提醒worker也已接入源码并默认每分钟检查；worker的Go/Docker运行结果仍待验收。

PlanExecutionService已提供执行事务端口：成功保存计划/资源/日志/公告/outbox；前提冲突保存BLOCKED/失败依据/日志，并以Conflict=true结果返回，事务回调不抛业务冲突。A执行handler将该结果映射为携带计划数据的409；完整的提交后幂等响应需公共中间件支持409终态快照和重放。见[事务服务记录](../TDD/SUMMARY.md#stage-17)与[HTTP记录](../TDD/SUMMARY.md#stage-32)。

当前A原生[PlanStore及PlanExecutionWork](./plan-storage.draft.md)已实现上述维护/执行存储，并验证过隔离库实际上下架和BLOCKED外层COMMIT；BLOCKED详情投影持久化的RESOURCE_CHANGED或RESOURCE_NOT_READY原因。Stage58已接入运行组合、HTTP幂等409快照/重放及提醒worker源码；新增Go/Docker链路尚待运行验收。

计划写领域进度：StatePlan已实现创建/修改/取消/执行方案，原发起人办理，创建仅当前资源buyer，预计时间未到不单独阻断。执行事务先保存BLOCKED再经外层幂等事务提交并返回409，具体部署验收见Stage58。

使用公共人员目录的StaffID，不把登录AccountID/OperatorID当作人员选择值。读取公告人员组合在一个一致性快照中完成；all_staff按真实目录分页组合，不先分页人员再伪造总数。

NoticeUnitOfWork须加入已有幂等事务，阅读唯一性与日志同事务；禁止独立提交。该端口不替代共享事务/日志工具。计划摘要从plan及当前resource一致性读取，不写旧表；到期worker只写公告，不执行资源状态变更。
