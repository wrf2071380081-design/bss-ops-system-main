# A 资源上下架计划存储契约（草案）

> 2026-10-22更新：计划读取/创建/修订/取消已有真实页面/API联调；计划执行器、原发起人权限、BLOCKED提交后409终态重放、到期提醒worker和工作台plan_id投影现已接入源码（Stage58）。Go编译和Docker实际执行尚未验证；B的待应用配置事实仍需提供。详见[交接清单](./2026-10-22-handoff.md)及[TDD汇总](../TDD/SUMMARY.md#stage-58-a-侧配置安全计划执行与提醒源码接线2026-10-22)。

## 范围与交互

遵循v0.22及REQ-19：已接入资源由当前采购人创建ONLINE/OFFLINE计划，原发起人修改、取消或登记实际完成。due_at只用于提醒；GET、修改日期和提醒投递不改变资源实际状态。转交资源不转交旧计划发起人，资源版本变化也不能通过改日期静默重设计划基准。

## 原生33/34映射

复用plan的id/resource_id/kind/reason/due_at/before/after/status/created_by/created_at，不改公共迁移。after采用supply.state_plan/v1完整安全封套，含字符串revision、expected_resource_revision、原始资源基准与预计目标快照；before保存同协议的原始创建快照，后续永不覆盖。控制字段不从日志数量、时间或当前buyer猜测。执行成功领域EXECUTED映射原生DONE，以兼容公共35工作项视图；BLOCKED保留原样。

每次变更新增operation_log，entity_type=plan、action=PLAN_REVISION；before/after为完整修订封套，reason/request_id/operator_id保留动作归因。当前行与最新保留修订、原生列及原始基准必须一致；旧计划缺协议或未知协议返回契约错误，不能补假revision或发起人。此适配只处理ONLINE/OFFLINE，不接管B的PRICE_CHANGE计划。

## 读取

详情、列表与当前resource控制头在同一一致性快照读取。列表先resource_id/region/kind/status筛选，再created_at DESC、数值id DESC分页。所有内部人员可读，不按buyer过滤；人员目录显示名由公共装配提供，空名称不替换真实created_by。读取只返回安全ResourcePlan及当前资源版本；不暴露存储快照。

## 维护事务

加入调用方事务，不内部提交。创建先锁资源，再检查该资源ONLINE/OFFLINE维度PENDING计划，并调用B的“已审批尚未应用配置变更”可信提供方；缺提供方或错误不能当false。修改/取消先非锁定定位资源，锁资源后锁计划，再复核引用。所有状态计划写入口共享此锁顺序，现有表没有全局唯一约束，绕过该协议的外部SQL不在保护范围。

计划写入、不可变日志及提醒outbox同事务；日志或outbox缺失、被过滤、失败时整笔回滚。每次revision发布新的提醒指令，包含plan_id/revision/status/due_at及schedule；取消发布失效指令。公共worker按当前计划revision/PENDING重新核对，不依靠删除旧事件，不修改资源状态。创建/修改/取消均保持resource不变。

## 执行接入门槛

执行适配借用同一外层事务，先定位资源并锁供应商→资源→计划，ONLINE再锁模型及读取当前正式配置/所选报告。供应商锁与现有配置/报告/资源写共享，模型锁与F1修订共享；旧计划基准不得重算。OFFLINE不因当前资料/报告不再可报价而阻止安全下架，只核对实际资源与原计划基准。

当前执行修订在after封套的execution保存实际完成说明、发生时间、失败原因或唯一source_event_id；资源控制日志亦保留plan_id/plan_revision/source_event_id。成功时创建RESOURCE_ONLINED/OFFLINED outbox，并用该真实主键关联RESOURCE_CHANGE公告，影响摘要初始PENDING，不猜客户数量、不自动恢复客户关系。BLOCKED不生成资源事件/公告；已有提醒因revision/status不再匹配而失效。日志和事件提供方须被核对实际写入，不能空回调成功。

执行适配复核原发起人、计划revision及原始资源revision；ONLINE须完整可报价前置，OFFLINE须实际已接入。成功原子保存资源、DONE计划、日志、公告与事件；前置冲突保存BLOCKED及原因，但不发布资源变更事件。构造NewPlanExecutionWork(plans, configs, reportRefLoader)，传给NewPlanExecutionService；ONLINE缺真实配置/报告提供方返回依赖不可用。Outbox写入须保留传入的OccurredAt、人员和request_id，不能任意替换为数据库now或空回调。

HTTP执行必须先在幂等外层事务中提交BLOCKED，再以409返回。该提交/重放及提醒worker已接入Stage58源码，但Go编译、Docker运行和真实HTTP/PG结果尚待验收。

当前运行组合已注入 `SupplyDeps.PlanExecutor` 与 `POST /plans/:id/execute`。handler只接收规范revision、实际操作确认、完成依据和原因，成功返回EXECUTED，Conflict结果返回携带BLOCKED计划数据的409。幂等仓储将该409保存为DONE终态快照并原样重放；普通校验/授权4xx仍为FAILED。前端执行能力契约已同步。该源码尚待Go编译与Docker真实HTTP/PG验收；计划创建仍依赖B提供待应用变更事实。A API契约见[TDD汇总](../TDD/SUMMARY.md#stage-32)及Stage58。

## 验证

专用库使用原生LIKE表及FK验证创建、修改、取消、唯一待办维度、原发起人限制、旧资源基准保留、条件分页、过期GET无副作用、日志/outbox失败回滚及旧协议拒绝；真实创建竞争、OFFLINE/ONLINE组合、失败报告阻断上架、实际事件与公告、重复执行及BLOCKED外层COMMIT也已验证。Stage58新增HTTP幂等重放/worker虽已接入源码，仍待Go编译与Docker/v36联合验收，见[TDD汇总](../TDD/SUMMARY.md#stage-58-a-侧配置安全计划执行与提醒源码接线2026-10-22)。
