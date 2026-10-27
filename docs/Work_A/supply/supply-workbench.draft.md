# F13 工作台公告与资源计划契约

<a id="workbench"></a>

更新：2026-10-27。A提供公告/个人阅读、资源计划/执行/提醒与具体对象导航；人员筛选和共享聚合由现有工作台组合，B审批与分派规则不重做。 当前迁移与部署见[交接](./handoff.md)，执行结果只维护[TDD SUMMARY](../TDD/SUMMARY.md)。下文保存业务/接口/存储约束，不再逐阶段重复测试结论。

## 人员与任务规则

默认看本人，切换查看人员不改变认证身份。待阅每人各自标记，GET无副作用，不能代别人已阅。普通执行待办只绑定一个人；供应商报价共享审批是有资格者完成一次并分派后续任务，展示在领导待办中，不恢复独立审批中心。

上下架严格走创建计划→原发起人登记实际完成；创建/修订/取消和到期均不改变资源状态。修改日期不重设原资源版本；转交资源不改变旧计划发起人。前置冲突保存BLOCKED/失败依据，事务提交后才返回并保存409幂等响应；成功同事务保存资源、计划、日志、公告和事件。

<a id="workbench-http"></a>

## 公告、阅读与计划HTTP

### 1. 读取契约

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

### 2. 标记已读

POST `/api/internal/notices/:id/read` 允许空body或`{}`，拒绝自报reader_id/staff_id/user_id等字段。Actor从认证StaffID读取，操作说明由服务端设为“阅读公告”，仍要求Idempotency-Key与请求追踪。

新请求同事务读取公告、核对内部人员、锁定/创建个人阅读行并追加操作记录。唯一键notice_id/staff_id由数据库强制。首次READ写阅读行，重复READ_NOOP保留首次read_at且不重复阅读行；新的请求意图仍留痕。公共幂等重放不再次执行业务。日志/提交失败必须整体回滚，不返回成功对象。

### 3. 计划只读摘要

计划发起人created_by与当前资源buyer_id分开。返回is_initiator仅说明发起人身份，不授权查看者代办。摘要始终read_only=true，表示读取本身无副作用；办理能力通过独立permissions字段声明，不能仅据read_only推断是否可打开维护表单。

permissions.can_revise仅原发起人且PENDING的上下架计划为true；can_cancel允许原发起人的PENDING/BLOCKED上下架计划；can_execute仅原发起人的PENDING上下架计划可用，API执行服务仍会再次校验身份。前端缺失或不匹配permissions时全部关闭，不根据资源当前buyer或所查看人员自行授权。修改仅提交expected_revision/due_at/reason；取消提交expected_revision/reason，取消原因写操作日志，不覆盖原计划原因。修改不更换原资源版本依据。

前端已在计划详情提供修改/取消/实际完成入口（按显式能力展示），沿用原型vPlanEditForm和实际完成确认字段。网络失败保留输入和同一提交意图的幂等键；403/409停止提交，返回详情重新读取后核对；成功重新加载详情及资源计划列表。

预计时间仅用于追踪，不因尚未到期拒绝读取；当前资源revision与计划预期不同则返回precondition_matches=false和阻断原因，GET不修改计划状态。F13执行服务必须验证发起人、实际外部操作确认、资源前提及共享事务。

### 4. 公共/真实适配要求

适配器不独立提交；缺事务或日志函数即失败。运行组合已注入NoticeWriter。

该接口不自动转换旧payload，运行库不因本项另加迁移。

创建计划另需阻止B已通过但尚未应用的配置变更（原型vCreatePlan第539行）。PlanMaintenanceTransaction.HasPendingConfigurationChange在同资源锁内查询，true返回409，依赖错误阻断；B审批/应用与A创建计划统一锁顺序。不能把缺实现或查询失败视为没有变更，见[修正记录](../TDD/SUMMARY.md#stage-15)。

计划列表由原生PlanStore读取，缺能力时503；created_at DESC、数值id DESC稳定排序，过滤先于分页，total为计划数。每条计划携带当前资源一致性事实；同详情共享校验与只读投影，不按buyer限制内部读取。

计划维护HTTP提供POST plans、PUT plans/:id、POST plans/:id/cancel，注入SupplyDeps.PlanWriter=PlanMaintenanceService。创建字段resource_id/expected_resource_revision/kind/due_at/reason，修改expected_revision/due_at/reason，取消expected_revision/reason；ID/revision字符串，日期RFC3339，拒绝自报操作者。执行HTTP由运行组合注入PlanExecutor，原发起人PENDING上下架计划可执行；BLOCKED回执以409终态快照保存/重放。

PlanMaintenanceService已提供Create/Revise/Cancel事务服务，创建按当前buyer，修改/取消按原发起人；同状态维度唯一、修订/日志与提醒意图同事务。SQL及创建/修改/取消页面已运行，提醒worker也已接入源码并默认每分钟检查。

PlanExecutionService已提供执行事务端口：成功保存计划/资源/日志/公告/outbox；前提冲突保存BLOCKED/失败依据/日志，并以Conflict=true结果返回，事务回调不抛业务冲突。A执行handler将该结果映射为携带计划数据的409；409终态快照先提交，再由公共幂等中间件保存和重放。见[事务服务记录](../TDD/SUMMARY.md#stage-17)与[HTTP记录](../TDD/SUMMARY.md#stage-32)。

当前A原生[PlanStore及PlanExecutionWork](./supply-workbench.draft.md#plan-storage)已实现上述维护/执行存储，并验证过隔离库实际上下架和BLOCKED外层COMMIT；BLOCKED详情投影持久化的RESOURCE_CHANGED或RESOURCE_NOT_READY原因。

计划写领域进度：StatePlan已实现创建/修改/取消/执行方案，原发起人办理，创建仅当前资源buyer，预计时间未到不单独阻断。

使用公共人员目录的StaffID，不把登录AccountID/OperatorID当作人员选择值。读取公告人员组合在一个一致性快照中完成；all_staff按真实目录分页组合，不先分页人员再伪造总数。

NoticeUnitOfWork须加入已有幂等事务，阅读唯一性与日志同事务；禁止独立提交。该端口不替代共享事务/日志工具。计划摘要从plan及当前resource一致性读取，不写旧表；到期worker只写公告，不执行资源状态变更。

<a id="notice-storage"></a>

## 公告载荷与个人阅读事务

### 表字段映射

| 表字段 | A 语义 |
|---|---|
| notice.id | 公告 ID |
| notice.type | Notice.Kind |
| notice.target_type | RESOURCE |
| notice.target_id | Notice.ResourceID |
| notice.created_at | Notice.PublishedAt，发布时业务时间 |
| notice.payload | 下述版本化资料；不含可覆盖表字段的 ID、kind、发布时间 |
| notice_read.notice_id/staff_id/read_at | 个人首次阅读记录，人员使用 internal_staff.id |

payload 结构：

```json
{
  "contract": "supply.notice",
  "version": 1,
  "resource_name": "资源发布时名称",
  "region": "CN",
  "title": "资源公告",
  "content": "公告正文",
  "reason": "变更原因",
  "source_event_id": "123",
  "affected_status": "PENDING"
}
```

source_event_id 可省略；如有必须为正 bigint 字符串。affected_status=PENDING 时不能带数量；READY 时必须带 affected_customer_count，使用无符号整数字符串，允许 "0"。区域只接受 CN/OVERSEAS。未知字段、未知版本、缺标记或无效资料拒绝，不从资源当前状态猜历史公告，不把默认空对象当完整公告。

### 读取行为

- 仓储只读取 contract=supply.notice 的 A 公告，其他业务公告交各自适配器；A 标记下的未知版本在读到时明确报错，不按 v1 猜测。
- 选择人员来自真实 internal_staff 目录，包含保留的历史人员，不用登录账户替代人员。显式选择不存在人员返回不存在错误；all_staff 使用完整目录，不能先分页人员。
- notice 与人员组合后左联 notice_read；先按 READ/UNREAD、region、resource_id、keyword 过滤，再计算总数和分页。
- keyword 为标题、正文和资源名称中的字面子串，百分号、下划线不作通配符。
- 排序 created_at DESC、notice.id 数值 DESC、staff.id 数值 ASC；total 是公告/人员组合数量。分页超出范围仍保留正确 total。
- 列表的人员检查、count 和分页查询使用同一个只读 REPEATABLE READ 事务；查询和提交错误返回错误，不降级为空列表或未读。
- 仓储不生成 can_mark_read；交既有 NoticeReader 根据认证 actor 与所查看人员生成权限，查看他人不代办。

读取切片不写 notice_read，不装配生产主路由。

### 标记已读的事务适配

A 的 NoticeUnitOfWork 接收“从当前请求 context 取得已有 *sql.Tx”的解析函数，以及公共日志追加函数。每次调用从请求解析事务，不把某一次请求的事务保存在全局服务中；缺任一依赖即失败，不自行开启或提交另一笔写事务。

处理顺序：锁定 A 公告行 → 校验并锁定人员引用 → 读取个人阅读行 → 首次插入或保持原 read_at → 使用同一 *sql.Tx 追加 READ/READ_NOOP 操作日志。公告行锁覆盖阅读行尚不存在时的竞争；重复意图可以留 READ_NOOP 日志，但不改首次时间。公共幂等重放应在进入业务前返回原结果。

适配器仅参与外层事务，业务返回的 receipt 在外层提交前是候选结果。外层必须在错误时回滚、成功提交后才发送成功响应；不能直接把候选 receipt 当已提交成功。日志函数由共享文件约定维护人提供，A 只定义注入端口，不替代 pkg/oplog。请求追踪号须适配现有 varchar(64)、原因 varchar(500)，超长明确失败，不截断。

首次阅读时间及同次日志时间统一为 UTC 微秒精度，避免纳秒回执与 PostgreSQL 微秒存储不一致。并发首次阅读验证使用外层 READ COMMITTED 事务；公共方如使用更高隔离级别，应评审并处理序列化冲突的整笔事务重试，不把异常当作已读成功。

<a id="plan-storage"></a>

## 计划存储与执行边界

### 范围与交互

遵循v0.22及REQ-19：已接入资源由当前采购人创建ONLINE/OFFLINE计划，原发起人修改、取消或登记实际完成。due_at只用于提醒；GET、修改日期和提醒投递不改变资源实际状态。转交资源不转交旧计划发起人，资源版本变化也不能通过改日期静默重设计划基准。

### 原生33/34映射

复用plan的id/resource_id/kind/reason/due_at/before/after/status/created_by/created_at，不改公共迁移。after采用supply.state_plan/v1完整安全封套，含字符串revision、expected_resource_revision、原始资源基准与预计目标快照；before保存同协议的原始创建快照，后续永不覆盖。控制字段不从日志数量、时间或当前buyer猜测。执行成功领域EXECUTED映射原生DONE，以兼容公共35工作项视图；BLOCKED保留原样。

每次变更新增operation_log，entity_type=plan、action=PLAN_REVISION；before/after为完整修订封套，reason/request_id/operator_id保留动作归因。当前行与最新保留修订、原生列及原始基准必须一致；旧计划缺协议或未知协议返回契约错误，不能补假revision或发起人。此适配只处理ONLINE/OFFLINE，不接管B的PRICE_CHANGE计划。

### 读取

详情、列表与当前resource控制头在同一一致性快照读取。列表先resource_id/region/kind/status筛选，再created_at DESC、数值id DESC分页。所有内部人员可读，不按buyer过滤；人员目录显示名由公共装配提供，空名称不替换真实created_by。读取只返回安全ResourcePlan及当前资源版本；不暴露存储快照。

### 维护事务

加入调用方事务，不内部提交。创建先锁资源，再检查该资源ONLINE/OFFLINE维度PENDING计划，并调用B的“已审批尚未应用配置变更”可信提供方；缺提供方或错误不能当false。修改/取消先非锁定定位资源，锁资源后锁计划，再复核引用。所有状态计划写入口共享此锁顺序，现有表没有全局唯一约束，绕过该协议的外部SQL不在保护范围。

计划写入、不可变日志及提醒outbox同事务；日志或outbox缺失、被过滤、失败时整笔回滚。每次revision发布新的提醒指令，包含plan_id/revision/status/due_at及schedule；取消发布失效指令。公共worker按当前计划revision/PENDING重新核对，不依靠删除旧事件，不修改资源状态。创建/修改/取消均保持resource不变。

### 执行接入门槛

执行适配借用同一外层事务，先定位资源并锁供应商→资源→计划，ONLINE再锁模型及读取当前正式配置/所选报告。供应商锁与现有配置/报告/资源写共享，模型锁与F1修订共享；旧计划基准不得重算。OFFLINE不因当前资料/报告不再可报价而阻止安全下架，只核对实际资源与原计划基准。

当前执行修订在after封套的execution保存实际完成说明、发生时间、失败原因或唯一source_event_id；资源控制日志亦保留plan_id/plan_revision/source_event_id。成功时创建RESOURCE_ONLINED/OFFLINED outbox，并用该真实主键关联RESOURCE_CHANGE公告，影响摘要初始PENDING，不猜客户数量、不自动恢复客户关系。BLOCKED不生成资源事件/公告；已有提醒因revision/status不再匹配而失效。日志和事件提供方须被核对实际写入，不能空回调成功。

执行适配复核原发起人、计划revision及原始资源revision；ONLINE须完整可报价前置，OFFLINE须实际已接入。成功原子保存资源、DONE计划、日志、公告与事件；前置冲突保存BLOCKED及原因，但不发布资源变更事件。构造NewPlanExecutionWork(plans, configs, reportRefLoader)，传给NewPlanExecutionService；ONLINE缺真实配置/报告提供方返回依赖不可用。Outbox写入须保留传入的OccurredAt、人员和request_id，不能任意替换为数据库now或空回调。

HTTP执行必须先在幂等外层事务中提交BLOCKED，再以409返回。

当前运行组合已注入 `SupplyDeps.PlanExecutor` 与 `POST /plans/:id/execute`。handler只接收规范revision、实际操作确认、完成依据和原因，成功返回EXECUTED，Conflict结果返回携带BLOCKED计划数据的409。幂等仓储将该409保存为DONE终态快照并原样重放；普通校验/授权4xx仍为FAILED。前端执行能力契约已同步。该源码尚待Go编译与Docker真实HTTP/PG验收；计划创建仍依赖B提供待应用变更事实。A API契约见[TDD汇总](../TDD/SUMMARY.md#stage-32)及Stage58。

<a id="price-plan"></a>

## 内部采购系数改价计划

### 生命周期

`PENDING → EXECUTED | CANCELED | BLOCKED`

- 新建只建立计划快照，不修改资源。计划保存资源ID、资源版本、修改前/后的供应商接口模型ID、修改前/后的采购系数、发起人、指定办理人、约定时间、原因和计划修订号。请求中的 `supplier_model_id` 表示计划目标值。
- 仅有上架历史且当前为 `ADMITTED` 或 `OFFLINE` 的资源可建立计划；同一资源同一时间只允许一个待执行的内部改价计划。
- 新系数是 `0 < coefficient ≤ 1` 的十进制字符串，最多四位小数。接口模型ID或系数至少一项必须改变；允许只改接口模型ID或只改系数。
- 计划发起人可以修改待执行计划、取消待执行或阻断计划；修改递增计划修订号并重排提醒，不重设资源版本基准。
- 指定办理人是唯一可登记实际完成的人。其他内部人员可以查看计划，但不能代办。
- 约定时间只触发提醒，不自动改变资源或调用供应商平台。
- 当前首版将资源归属采购人作为创建资格，并要求办理人是在职内部员工；如产品另有授权规则，后续按新决策调整。

### 实际完成事务

办理人提交外部调整完成确认、实际完成依据和登记原因后，服务端在一个事务内重新读取资源与计划：

1. 计划状态、计划修订号、资源修订号及当前接口模型ID/系数仍与计划基准一致时，更新计划目标接口模型ID和/或采购系数并递增资源revision；仅接口模型ID变化时递增目标版本。登记执行人/时间/完成依据和操作日志，写入与变更内容一致的事件及工作台公告。
2. 仅变更接口模型ID时，保留历史报告但递增目标版本；旧目标报告随即失效，必须有匹配新目标版本的测试结果后才能恢复报价资格。其他资源状态、正式配置、标准模型和申报来源事实保持不变。
3. 资源、接口模型ID或系数前提冲突时，保存 `BLOCKED` 和失败原因，不更新资源、不发成功事件；HTTP 返回携带计划回执的 `409`。
4. 计划完成事件标明 `source.kind=PLAN`、计划ID和完成后的计划修订号，包含接口模型ID及系数的前后值；兼容事件类型仍为 `RESOURCE_COEFFICIENT_CHANGED`，目标版本递增及报告失效必须可查询。到期提醒使用 `COST_CHANGE`。

### HTTP

- `POST /api/internal/plans/price-change`：创建内部改价计划。
- `PUT /api/internal/plans/:id/price-change`：发起人修改计划。
- `POST /api/internal/plans/:id/cancel`：发起人取消计划（与上下架计划共用取消端点）。
- `POST /api/internal/plans/:id/execute`：指定办理人登记实际完成（与上下架计划共用执行端点；服务端按计划类型授权）。
- `GET /api/internal/plans`、`GET /api/internal/plans/:id`：列表和详情，包括接口模型ID/系数前后快照、办理资格和当前资源版本。

创建、修改、取消和执行均沿用公共幂等中间件；UI 不传入创建者或执行者身份，操作者来自认证上下文。
