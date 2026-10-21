# 工作台 A 子能力：公告阅读与计划摘要

依据：v0.22 原型1263—1267行、A详细设计v1.1 §9.1/12.2、附01公共工作台归属。A提供公告/计划事实，公共负责人汇总A/B/C的待办/已办/待阅/已阅及人员选择界面。

## 1. 读取契约

| 接口 | 入参 | 结果 |
|---|---|---|
| GET `/api/internal/plans` | resource_id、region、kind、status、page/size | 当前资源前提与原发起人的计划只读分页 |
| GET `/api/internal/notices` | region、resource_id、read_status=ALL/UNREAD/READ、keyword、page/size | 当前认证StaffID的公告投影 |
| GET `/api/internal/notices/read-states` | 上述条件、staff_ids（逗号分隔）、all_staff=true | 指定人员或全部内部人员的阅读投影；省略人员条件默认为当前人员 |
| GET `/api/internal/notices/:id` | 可选staff_id | 公告及该人员阅读详情；省略staff_id看当前人 |
| GET `/api/internal/plans/:id` | 无人员替换参数 | 发起人、资源、预计/实际时间、修订号、状态及当前前提是否匹配的只读摘要 |

所有读取仅限认证内部人员，全员可读，不套旧角色域。staff_ids与all_staff不可同时使用；显式ID最多100个，重复/非法ID拒绝。全部人员通过真实内部人员目录查询，不以空数组猜测全集。分页对象为notice/person组合，total不是去重公告数；排序published_at desc、notice_id desc、staff_id asc，先过滤阅读状态再分页。

投影包含notice、staff_id/name、read_status、可空read_at、permissions.can_mark_read。仅查看自己且未读时可标记已读；查询他人不能切换认证身份。关联客户影响PENDING不能显示成0；READY时明确数量。

## 2. 标记已读

POST `/api/internal/notices/:id/read` 允许空body或`{}`，拒绝自报reader_id/staff_id/user_id等字段。Actor从认证StaffID读取，操作说明由服务端设为“阅读公告”，仍要求Idempotency-Key与请求追踪。

新请求同事务读取公告、核对内部人员、锁定/创建个人阅读行并追加操作记录。唯一键notice_id/staff_id由数据库强制。首次READ写阅读行，重复READ_NOOP保留首次read_at且不重复阅读行；新的请求意图仍留痕。公共幂等重放不再次执行业务。日志/提交失败必须整体回滚，不返回成功对象。

## 3. 计划只读摘要

计划发起人created_by与当前资源buyer_id分开。返回is_initiator仅说明发起人身份，不授权查看者代办。摘要始终read_only=true，表示读取本身无副作用；办理能力通过独立permissions字段声明，不能仅据read_only推断是否可打开维护表单。

permissions.can_revise仅原发起人且PENDING的上下架计划为true；can_cancel允许原发起人的PENDING/BLOCKED上下架计划；can_execute当前恒false。前端缺失permissions时全部关闭，不根据资源当前buyer或所查看人员自行授权。修改仅提交expected_revision/due_at/reason；取消提交expected_revision/reason，取消原因写操作日志，不覆盖原计划原因。修改不更换原资源版本依据。

前端已在计划详情提供修改/取消入口（按显式能力展示），沿用原型vPlanEditForm的原因和预计时间字段。取消前填写留痕原因并确认。网络失败保留输入和同一提交意图的幂等键；403/409停止提交，返回详情重新读取后核对；成功重新加载详情及资源计划列表。实际执行登记入口仍未开放。见[页面验证记录](../TDD/SUMMARY.md#stage-23)。

预计时间仅用于追踪，不因尚未到期拒绝读取；当前资源revision与计划预期不同则返回precondition_matches=false和阻断原因，GET不修改计划状态。未来F13执行服务仍须验证发起人、实际外部操作确认、资源前提及共享事务。

## 4. 公共/真实适配要求

阅读写入已新增supplypg.NoticeUnitOfWork：逐请求解析外层*sql.Tx，注入公共日志函数，公告/人员/阅读锁及首次写入参与同事务；真库已验证首次/NOOP、SQL日志失败回滚、外层回滚和READ COMMITTED并发，见[写入事务记录](../TDD/SUMMARY.md#stage-14)。首次回执与日志统一UTC微秒精度。适配器不独立提交；缺事务或日志函数即失败，生产NoticeWriter仍待公共装配和提交后响应机制，不以候选receipt宣称已提交。

公告读取已有真实SQL适配：supplypg.NewNoticeReadStore(db)注入NoticeReader，按[存储契约草案](./notice-storage.draft.md)读取版本化A公告，在只读REPEATABLE READ快照内检查人员、计数和分页；真实A HTTP/领域/SQL测试已通过，见[验证记录](../TDD/SUMMARY.md#stage-13)。该草案尚待公共和事件生产方接入评审；不自动转换旧payload。公共装配/运行库迁移及真实写接口联调仍未完成。

创建计划另需阻止B已通过但尚未应用的配置变更（原型vCreatePlan第539行）。PlanMaintenanceTransaction.HasPendingConfigurationChange在同资源锁内查询，true返回409，依赖错误阻断；B审批/应用与A创建计划统一锁顺序。不能把缺实现或查询失败视为没有变更，见[修正记录](../TDD/SUMMARY.md#stage-15)。

计划列表通过可选PlanListStore读取，缺能力503；created_at DESC、数值id DESC稳定排序，过滤先于分页，total为计划数。每条计划携带当前资源一致性事实；同详情共享校验与只读投影，不按buyer限制内部读取。前端面板已实现，实际SQL和生产装配仍待接入，见[列表记录](../TDD/SUMMARY.md#stage-20)及[TDD最新索引](../TDD/README.md)。

计划维护HTTP已在A注册器提供POST plans、PUT plans/:id、POST plans/:id/cancel，注入SupplyDeps.PlanWriter=PlanMaintenanceService。创建字段resource_id/expected_resource_revision/kind/due_at/reason，修改expected_revision/due_at/reason，取消expected_revision/reason；ID/revision字符串，日期RFC3339，拒绝自报操作者。执行HTTP仍未注册，当前只读摘要不承诺办理权限，见[HTTP记录](../TDD/SUMMARY.md#stage-18)。

PlanMaintenanceService已提供Create/Revise/Cancel事务服务，创建按当前buyer，修改/取消按原发起人；同状态维度唯一、修订/日志与提醒意图同事务。HTTP及创建/修改/取消页面已实现为独立切片，实际SQL、worker和生产装配仍待实现，见[维护服务记录](../TDD/SUMMARY.md#stage-22)及[TDD最新索引](../TDD/README.md)。

PlanExecutionService已提供执行事务端口：成功保存计划/资源/日志/公告/outbox；前提冲突保存BLOCKED/失败依据/日志，并以Conflict=true结果返回，事务回调不抛业务冲突。外层幂等事务提交后才可映射409，当前公共提交响应机制尚未对齐，执行HTTP尚未注册。见[事务服务记录](../TDD/SUMMARY.md#stage-17)。

计划写领域进度：StatePlan已实现创建/修改/取消/执行方案，原发起人办理，创建仅当前资源buyer，预计时间未到不单独阻断。当前已有维护应用服务/HTTP及页面；执行HTTP/页面未开放。事务层须先提交BLOCKED记录再映射409，不能直接抛业务错误回滚失败记录。见[领域记录](../TDD/SUMMARY.md#stage-19)。

使用公共人员目录的StaffID，不把登录AccountID/OperatorID当作人员选择值。读取公告人员组合在一个一致性快照中完成；all_staff按真实目录分页组合，不先分页人员再伪造总数。

NoticeUnitOfWork须加入已有幂等事务，阅读唯一性与日志同事务；禁止独立提交。该端口不替代公共事务/日志工具。计划摘要从plan及当前resource一致性读取，不写旧表，不执行到期计划。新表/日志/主装配缺失仍是接入依赖。
