# A 模块 TDD 汇总

> **当前交付摘要（2026-10-27，Stage209）：**用户已授权将A增量、测试、正式v44/v45迁移和精简文档提交推送到work-A-fix。基线已合入main@1fe8907，日常库45/false。运行API尚未替换为合并构建；本轮只做交付静态核查，不重新宣称业务全量验收。旧阶段记录保留原执行时点。

> **阅读规则：**下面旧摘要及正文保留原执行时点。历史本地43/44不等于当前正式43/44；不从旧缺项推断现在仍未实现，也不将fixture、结构能力或构建结果等同完整真实业务联调。当前路径见[数据库说明](../supply/database-v45-upgrade.md)、[主交接](../supply/handoff.md#handoff)和[验收清单](./handoff-acceptance.md)。

> **历史摘要Stage204（2026-10-27）：**按用户新确认沿用B提交门槛（至少一行有效连通通过、压力非强制）及草稿物理测试资源→批准后正式接管。门户真实草稿/409恢复/离开保护/撤回、无压力的保存提交跳转，以及B批准后同ID资源接管与防重通过；修复草稿无冲突恢复入口、提交成功被路由守卫拦截、测试资源污染正式资源读取三处问题。日常44/clean，API已更新，QA已清理，未提交推送。报告为明确QA产物，未调用Inspector。

> **历史摘要Stage203（2026-10-27）：**供应商管理列表税率比例显示已修复：`0.06/0.0600→6%`、`0.0625→6.25%`，未填写显示`—`、真实零显示`0%`。浏览器先复现错误后修复通过，定向格式化测试及类型检查通过；本轮浏览器使用接口夹具，没有新增真库写入或迁移，未提交推送。Stage202业务链路结论保留。

> **历史摘要Stage202（2026-10-27）：**供应商F12归档、跨页冲突恢复、合同/授信并发增版、历史附件、业务日志分类分页与导航、附件清扫及失败保护已在日常44/clean定向验证。修复普通维护上传附件误要求流程字段的400错误，新增回归先失败后通过；当前API已更新。测试资料、文件及故障注入均已清理，无新增库或容器，未提交推送。Inspector与客户F12不在本轮验收内；以下旧阶段的“待验”以Stage202实际覆盖为准。

> **历史摘要Stage201（2026-10-27）：**用户指定的四项已实现并在日常44版定向联调：真实办理节点/人员及全状态原生申报历史，分阶段批量接入/取消，曾用名，资质元数据。新000044已备份后应用，44/clean；三模型配置3成功→确认开通1成功2阻断→取消2成功；全取消无多余商务待办。改名检索、资质上传/权限/日期/409恢复、重启文件及受影响页面回归通过，QA已清理，未提交推送。权威审核、D07、Inspector及客户F12未因此全部完成。下方为历史记录。

> **最新执行（Stage200，2026-10-27）：**用户明确授权A配置v43并继续实现，`000043_f12_records`已备份后应用，日常`model_bss=43/dirty=false`。真实供应商首次合同/授信资料保存、不可变版本、齐备查询、资源配置后商务完成及工作台已办通过；附件API重启后仍可授权下载。已装配真实B原生流程授权和A齐备服务，Stage199对应阻断已解除。客户F12实际身份/流程映射、F4其他增量和真实Inspector仍未完成。QA行及附件已精确清理，未提交推送；下方为历史结果。

> **最新执行（Stage196，2026-10-27）：**原生B审批来源、当前办理资格、来源版本、指定baseline/stress双报告和完成回写已默认装配；真实认证页面/API/42版PG通过绑定→接入→模型核验后可报价、批准改价及200幂等重放。B申报行/CONFIG/提交完成、接入及改价事件各一次。Inspector外部结果为明确QA产物，未调用平台；领导正向待办/flows HTTP仍待B接续。下方Stage195及以前为历史结论。

> **最新执行（Stage195，2026-10-26）：**A运行binder/admitter构造与批准改价PG/HTTP/表单已补；6个真实PG事务场景、身份/幂等/冲突接口、页面请求及失败保留草稿通过。日常API更新，42/clean且QA夹具已清理。B真实办理/来源版本/报告事实仍未注入，不宣称AB07/08成功链通过；下列Stage194及以前为历史。

> **最新执行（Stage194，2026-10-26）：**标准模型42版真实保存/核验/修改失效/历史日志回归通过；工作台多账号权限、真实人员筛选与计划点击导航、转交后原计划权限、提醒防重及真实HTTP事务失败回滚5组通过。内部补测已由指引改为实际按钮/类别表单、服务端平台目标与当前办理人授权，页面及403/400反例、授权原生SQL与接口定向测试通过。按用户要求不测真实Inspector配置/调用和任务回收。只使用日常两个容器，QA数据已清理，未提交推送。

> **最新执行（Stage193，2026-10-26）：**日常42版环境的非空报告历史/分页、详情、受控文件、0%与缺失指标、真实页面和浏览器下载已通过7组定向检查。使用A原生存储及B的CreateJob/CompleteJob生成明确标记的数据，未调用Inspector、未伪造审批或办理资格。修复任务指纹漏存、任务/报告/文件ID混用和仅比率时空Token展示；数据及文件已按精确ID清理。绑定/接入仅验证缺提供方503与无业务变更，成功路径仍待真实办理提供方。只保留日常两个BSS容器，无新环境、迁移、提交或推送。

> **最新代码同步（2026-10-26，Stage188）：**`work-A-fix`已快进到`origin/main@a7c5ef4`，包含B申报/测试平台PR #8；A未提交改动已恢复并解决冲突。源码迁移到000042，数据库实际升级见Stage190。Stage180–187是同步前的验收证据，不能直接证明合并后新接口已完成跨模块联调；B提供方缺项需基于最新代码重新核对。

> **最新环境（2026-10-26，Stage191）：**BSS只保留日常`model_bss_api_latest`与`model_bss_pg`两个容器，日常库`model_bss=42 / dirty=false`、8080健康且数据校验一致。五个临时/旧测试容器、四个历史测试库及专用PG卷/网络已在归档恢复校验后删除；18081/18082不再运行。测试数据库与附件备份保留于忽略目录`.qa/environment-cleanup-20261026/`。后续联调仅按需建立一个同版本临时环境，用完清理。新B功能仍按[联调计划](./ab-integration-plan.md#ab-plan)验收，日常API二进制本轮未更换。

### 2026-10-25：F4 资质元数据源码补充

- 增加资质类型、可空有效期、审核状态只读投影、revision CAS 维护和登记人投影；六个增量列缺失或类型/空值约束不兼容时回报 `metadata_supported=false`，保持旧版列表和基础文件上传兼容。新接口不接受审核结果，也不把上传视为审核通过。
- 文件仍通过既有 A 本地资质存储和 `file_ref` opaque 引用保存；D07 `file_id` 映射、权威审核状态写入和增量迁移仍待提供方/迁移维护人完成。没有修改 `migrations/**`。
- 本轮只完成源码静态审阅和文档记录，未运行 Go 编译、单测、前端类型检查、数据库或页面联调。Docker 恢复后应优先定向验证新建元数据、非法日期、revision 冲突/重载、附件引用保留、日志同事务和旧结构兼容。

### 2026-10-25：F12 双主体写入口与受控附件源码

- 源码新增合同新增/修订/归档、授信追加版本的 PostgreSQL 适配与 HTTP 幂等路由；共用主体面板增加合同/授信表单、版本操作和附件下载，响应字段兼容 v36 旧投影。
- 新增最小私有 F12 文件目录适配：仅 PDF/PNG/JPEG、最大 2 MiB、目录及文件限制权限、随机 opaque ID、主体/用途/上传人 sidecar 校验；下载要求文件已被同主体记录引用，并重新校验当前主体责任人或 B/C 指定办理人，缺少首次办理授权提供方时 fail-closed。运行配置为 `MODEL_BSS_F12_ATTACHMENT_STORE_DIR`，未配置时依赖关闭。
- 未改 `migrations/**`。F12 增量字段尚未由迁移维护人交付，B/C 首次办理事实提供方尚未装配；当前 v36 写操作应返回依赖未就绪，不可作为生产可写能力。
- 本条只记录源码状态，未运行测试。待 Docker恢复后优先验证字段缺失时 503、文件大小边界、主体/用途隔离、事务/日志回滚、同 ID 双主体版本、幂等重放及真实页面刷新闭环；详细请求见 `../supply/supplier-f12-schema-request.md` 和 `../supply/f12-records.draft.md`。

> 历史交接入口：[责任与接入参数](../supply/handoff.md#handoff)、[交接验收清单](./handoff-acceptance.md)。Stage83–84 曾记录当时主 Docker 快照；下方阶段条目保留各自执行时状态，当前容器和数据库以文档开头及最新 Stage 条目为准。

> 职责更正：总览/A任务书/共享契约只有A/B/C三条线，没有独立公共负责人；共享文件统一维护未点名全部由谁负责。A负责F4/F13等业务闭环，C明确提供F14日志/绑定helper。历史阶段的“待公共负责人”只保留当时表述，不能据此移交A责任；最新交接清单已校正。

## 2026-10-21：F1 接入适配与 36 版结构验证

- 运行库 `schema_migrations` 只读核对为 `36 / dirty=false`。新组装验收连接专用的 schema-only 测试库，未对运行库执行写入或迁移。
- 按 TDD 补齐 `GORMModelTxFromContext`，将公共幂等中间件注入的 GORM 事务解析为同一 `*sql.Tx`；新增 `AppendModelOperationLog`，在同一事务写公共 `operation_log`。
- 新增 `NewStandardModelSupplyDeps` 作为 A 的 F1 依赖构造入口，检查幂等仓储与 A SQL 仓储使用同一连接池，并构造标准模型读写服务；不改公共 `main.go`、主路由或权限装配。
- `TestStandardModelModuleCommitsBusinessLogAndIdempotencyTogether` 在 Go 1.26 容器中针对隔离库通过：创建投影、不可变首版本、操作日志和 `DONE` 幂等结果同时落库；同键重放不重复写版本或日志。
- 前端本轮 `npm test` 99/99 通过，`npm run build` 通过（Vite 报告现有大包体积提示）。标准模型专用浏览器脚本因项目环境缺少 `playwright` 包而未启动，不能据此算作交互验收。
- 当前未完成公共 `registerSupplyRoutes`/`api.Deps` 装配及真实认证、页面路由和运行容器联合验收。公共负责人完成入口接线后，再以 Docker 实际 API 做联合验收；本节结果只证明 A 注册器和适配器的真库组合。

## 后续本地阶段：计划实际完成与BLOCKED提交（2026-10-21）

契约：[计划存储与执行](../supply/supply-workbench.draft.md#plan-storage)。新增plan_execution.go、原生组合执行测试，并扩展计划/资源控制封套保存完成依据与来源事件；本节为未提交实现。

- 红阶段实际执行：执行服务接收NUL/非法UTF-8依据的反例失败，修复后统一拒绝，回执与动作时间统一UTC微秒。真实创建/接入/核验链上的计划测试在执行工作单元为桩时未拒绝他人、旧版本及缺失效果，且返回空执行结果；实现后同一反例通过。另加BLOCKED详情应显示RESOURCE_NOT_READY反例，先失败，再补PlanReadRecord.ExecutionFailureReason到安全BlockedReasons投影后通过。
- PlanExecutionWork借用PlanStore提供的外层事务/日志/outbox，锁供应商→资源→计划，ONLINE还锁模型并经真实ResourceReadStore/所选报告/正式配置组合读取准入事实。OFFLINE只复核资源和原始计划基准，不因当前不可报价而阻止下架。所有实际提案在写适配中再次按领域规则核对，不接受页面自报通过。
- 成功原子写资源状态/revision、原生DONE计划、不可变PLAN_REVISION与RESOURCE_REVISION、RESOURCE_ONLINED/OFFLINED事件及RESOURCE_CHANGE公告。计划和资源日志含plan ID/revision、完成依据和真实source_event_id；公告引用同一事件，影响摘要PENDING且数量未知，不修改客户关系。旧提醒由当前revision/status核对失效。
- 扩展TestResourceEvidenceAdmissionChainPostgres34的plan_execution子场景，复用真实F1/资源/报告/配置：未来计划实际下架→OFFLINE/revision6→新上架计划→ADMITTED/revision7。错误人员、旧计划版本、空日志、空事件回调、notice真实SQL CHECK失败均拒绝，外层回滚后仍为原ADMITTED/revision5，成功时只一条对应事件/公告；重复实际完成409。压力测试实际失败时ONLINE得到BLOCKED，资源仍OFFLINE，没有新资源公告；回滚该反例后恢复真实通过报告，上架成功。
- TestPlanConcurrentCreationPostgres34扩展真实外层提交：创建后的资源发生转交/revision变化，原发起人执行得到Conflict=true及nil error，外层COMMIT前另一连接仍见PENDING，提交后通过独立RR读取见BLOCKED/revision2并保留RESOURCE_CHANGED/完成依据；没有资源变更事件。此证明A事务结果可提交，不代替公共幂等HTTP提交后409响应验收。
- 新鲜验证：四包go test -count=1、同四包go vet及全后端go build ./...退出0；专用库显式运行14个顶层F1/F3/F4/计划原生SQL/并发测试通过。普通无专用库回归仍跳过原生SQL。测试独立schema清理，运行库未迁移。
- 未完成：执行HTTP的公共幂等提交/响应装配、生产B报告与公共配置提供方、实际页面办理入口和Docker联调；can_execute继续false，未对外注册执行路由。后续优先核对F1/F3主装配与原型页面接入，再联合验收工作台任务，不能把A受控组合称为全系统完成。

## 后续本地阶段：计划原生读取与维护（2026-10-21）

契约：[计划存储](../supply/supply-workbench.draft.md#plan-storage)。新增plan_store.go及依赖/原生维护/双事务竞争测试，复用现有plan.before/after与operation_log，不添加公共迁移；本节为未提交业务实现。

- 红阶段实际执行：schema反例暴露不安全schema被接纳；原生测试在桩实现下返回空计划，创建失败。实现后同一反例转绿。随后纳秒回执反例失败，将维护服务的创建时间、预计时间、修订及动作时间统一为UTC微秒，再验证通过。
- 当前after与不可变创建before使用supply.state_plan/v1安全封套，revision/基准版本为字符串，初始资源快照与预计目标仅允许状态不同。原生列和最新PLAN_REVISION日志须吻合，旧协议不填假版本。EXECUTED映射原生DONE仅建立协议，尚未写执行入口；列表限定A的ONLINE/OFFLINE，不接管B价格变更计划。
- 原生LIKE表测试补plan→resource FK，显式Asia/Shanghai时区。创建→详情→修改→取消→新建、分页与数值ID排序、过滤后total通过。预计时间在过去也保持资源ADMITTED不变。其他人员可读不能修改；转交后新采购人不能取得旧计划权限，原发起人修改也保留旧资源revision，GET显示前置已变化。
- 维护加入外层事务，固定锁资源→计划；创建检查待执行状态计划和B已审批未应用配置事实。B提供方必须注入，不以依赖缺失代替false。每次修订发布带plan ID/revision/status/due/schedule的提醒outbox，取消发布失效指令；本轮验证持久化，不证明公共提醒worker已实现。
- 日志失败、空日志、真实varchar SQL错误、提醒失败/空回调/真实SQL错误，在外层SAVEPOINT回滚后计划仍PENDING/revision2。“日志回调原地篡改旧修订字节”反例初次被错误接受；复制传给回调的before/after，并按未污染原件核对持久日志后转绿。
- TestPlanConcurrentCreationPostgres34在专用库独立测试schema建立原生表，两个READ COMMITTED事务竞争同一资源，第三连接pg_blocking_pids确认实际等待；提交前第三连接看不到计划，第一事务提交后第二事务409，最终仅一条计划、修订日志与提醒。测试结束删除仅该测试schema。
- 新鲜验证：四包go test -count=1、同四包go vet、全后端go build ./...通过；专用库14个顶层F1/F3/F4/计划SQL及并发用例通过，审计篡改和SQL回滚扩展后另复跑两个计划顶层用例通过。
- 未完成：计划实际完成执行事务（含BLOCKED提交）、资源变更公告/事件、公共提醒worker、真实B配置事实提供方及页面/主路由/Docker装配。本轮没有迁移运行库、提交或推送。

## 后续本地阶段：同步最新main与兼容性检查（2026-10-21）

- 通过用户指定的12000代理fetch，work-A-fix从2400df9快进至main的7f9886e；上游包含已合并的文档清理PR和统一登录/供应商门户改动。30个上游文件与31个本地已跟踪改动、71个未跟踪文件无路径重叠，merge-tree检查无文本冲突。更新前备份102个本地文件，更新后逐一SHA256比较无变化。没有新增业务提交或推送，远端work-A-fix未移动。
- 合并后的新鲜验证：A四包go test -count=1（domain/supply、api、infra/supplypg、domain/workbench）、domain/auth回归及全后端go build ./...退出0；前端tests/run.mjs共98项全部通过，vue-tsc --noEmit与vite生产构建退出0。构建有大于500kB的分块提示。本节普通Go回归未配置专用数据库，不能据此声称重新执行了原生SQL测试或Docker端到端联调。
- 上游新增000036审批资格布尔字段，A业务存储继续采用既有33/34表协议。本轮未迁移数据库、重建API镜像或切换运行服务。
- 源码复核发现：AppLayout隐藏无资格人员审批菜单，但guards未检查requiresApproval，permission store对internal.approvals无额外限制。该检查未包含在98项现有前端测试中，W14尚未满足，已记录到公共接入清单；不越过文件归属修改公共路由。

## 后续本地阶段：测试证据与登记接入真实事务（2026-10-21）

契约：[证据/登记接入与事件](../supply/supply-resources.draft.md#resource-transactions)。新增 `resource_test_evidence.go`、`resource_admission.go`、共同资源锁 helper、完整组合/并发测试及依赖单元测试；本节为未提交的本地业务实现。

- TDD红阶段实际执行：证据工作单元为桩，原生夹具已建但报告关联返回依赖不可用；实现后转绿。接入组合进一步暴露Go结构体直接比较时区的错误：SQL时区Asia/Shanghai与控制快照UTC指向同一时刻，诊断两时间Equal=true但Location不同。改为规范UTC/微秒后逐字段比较，未放宽身份、版本或准入条件；测试显式SET LOCAL TIME ZONE保持该反例覆盖。
- 完成说明含非法UTF-8/NUL的领域反例先失败后修复，不能到写阶段才变SQL错误。新增工作单元拒绝缺日志、B资格/报告/逐行完成或outbox提供方；缺依赖不进入事务解析，取消上下文原样传播。
- `TestResourceEvidenceAdmissionChainPostgres34`使用专用34原生临时表，补回资源/任务/报告/申报行FK。真实A创建服务建立两资源，真实报告选择/乱序保护、正式配置创建与绑定、逐资源登记接入、F1人工核验后可报价的组合通过。报告指标来自所选SQL行，真实0缓存没有被当成缺失，也不增加缓存阈值。
- B资格、序号、版本及完成仍是受控事务提供方；测试实际读取原生节点指派并CAS写逐行状态，实际写event_outbox。A不生成审批/分派事实。一行接入后另一行仍PENDING，节点/流程仍进行中，supplier.established仍false；未通过自身压力测试的同批资源不能借用另一模型结果。
- 证据关联验证SELECTED、重复NOOP、晚到旧序号、旧目标（即使序号更大）、未来目标拒绝和不可信B调用403。B报告插入＋A关联日志失败后整体回滚，报告行、当前指针和revision均恢复。已接入失败补测只更新证据/可报价性，保留ADMITTED与ever_admitted，不自动下架。
- 接入验证错误办理人403、过期revision、前置不满足、所选SQL报告实际失败、配置已变版本、日志缺失/完成依据被过滤、B行失败、outbox真实SQL失败、空回调及错误payload、外层回滚。每个失败后资源仍为原PENDING_ACCESS/revision，B行PENDING、outbox为空；成功后仅一条RESOURCE_ADMITTED事件，重复接入409不重复发布。
- A准备supply.resource_admitted/v1安全payload，所有版本为字符串，事件无凭据/URL；outbox writer返回实际主键，A核对类型/payload/请求号/人员及PENDING状态。控制快照保留节点proof、完成说明或本次报告处理依据，日志适配不得静默移除必要字段。
- `TestAdmissionFailedEvidenceContentionPostgres34`使用专用库public原生表、真实人员/组织/供应商/办理/资源/报告/outbox夹具，两个READ COMMITTED事务竞争，第三连接pg_blocking_pids确认等待。失败报告先提交→接入RevisionConflict、资源PENDING_TEST且无接入事件；接入先提交→后续失败复测成功但保留ADMITTED、只一条事件。外部连接在外层提交前看不到事件。最初夹具曾把绑定时间设在报告完成前，被检查正确拒绝，修正测试时钟后通过；此为夹具修正，不记作生产TDD红阶段。
- 新鲜验证：四包 `go test -count=1`、同四包go vet、全后端 `go build ./...`均退出0；PG容器Unix socket显式执行F1/F3/F4共14个顶层SQL/HTTP/并发用例，全部通过。公共表并发夹具按精确ID清理，复查供应商/日志/事件/人员/组织均0。普通无专用库的回归仍明确跳过真库用例。
- 仍未完成真实B后台/回调鉴权、公共幂等提交与投递、生产主路由/页面/Docker联合验收，以及计划的真实存储/执行接入。不能把A组合测试称为全系统端到端已完成。运行库复查32/dirty=false；未迁移、提交、推送或新增分散results/evidence报告。

## 后续本地阶段：正式配置原生存储与资源绑定（2026-10-21）

契约：[正式配置原生34存储](../supply/supply-suppliers.draft.md#formal-config)。新增 `formal_config_store.go`、`resource_config_binding.go` 及单元/真库/并发测试，复用现有34表。本节为未提交的本地实现，不属于文档清理PR。

- TDD红阶段实际执行：不安全schema被接纳，配置创建及绑定工作单元为桩时，真实SQL正向与授权反例失败；实现后同一测试转绿。后续回执携带纳秒、超长scope到达持久化、缺失配置导致资源详情404三个反例均先失败，再修复并重跑通过。
- 原生supplier_formal_config保留范围/URL/审计列和note用途；api_key_enc使用版本化密封信封编码，精确验证varchar(512)容量，不截断。状态/revision/确认信息与信封指纹保存为保留的安全控制快照，日志无明文、密文或密钥版本。测试用真实AES-GCM附加身份上下文验证存取与跨配置解密拒绝；该测试提供方不等于生产密钥已经配置。
- 供应商写资格读取真实显式初始分配/转交链，不将历史created_by当当前采购人。真库验证旧采购人转交后403、新采购人可维护、无归属链的历史主体明确拒绝。DEC-008初始创建者规则仍需创建路径和公共归属能力在生产原子记录，本切片不在配置命令中代建或猜测。
- `TestFormalConfigServicePostgres34Native`覆盖创建、确认、保留Key修改、轮换、NOOP、逻辑停用及重复停用，403/409/跨主体404，密文列漂移、坏控制头、日志真实SQL失败/空回调失败、外层回滚以及未知资源控制头不能被当成无引用。单元覆盖信封正好512字符及超限拒绝、协议/编码异常、中文scope字符边界、非法UTF-8/NUL和UTC微秒回执。
- `TestResourceConfigBindingPostgres34`使用真实配置、资源与模型存储，B proof仍为可信边界夹具。A按supplier→resource→config→B顺序加锁；实际办理人8与归属采购人7不同，7不能代办。验证目标/配置版本冲突、日志失败/外层回滚、真实引用阻止停用、BIND_NOOP不增revision以及流程/节点/办理人/字符串flow revision留痕。资源日志无配置URL或凭据。
- 配置更新不自动改变资源绑定版本，旧绑定读出FORMAL_CONFIG_CHANGED；当前办理人可显式关联新确认版本。缺失配置时保留资源详情并给FORMAL_CONFIG_UNAVAILABLE；损坏协议/指纹及SQL故障仍报错，不包装成空配置或可用。
- `TestResourceBindingDisableContentionPostgres34`在专用库public原生表短暂提交隔离夹具，两个真实READ COMMITTED事务、第三连接pg_blocking_pids确认共同supplier锁等待。绑定先提交→停用ErrConfigInUse；停用先提交→绑定ErrRevisionConflict。最终状态/版本符合先提交者，没有出现“已停用却新绑定”。人员/组织/供应商和日志夹具清理后数量均0。该结果不替代B真实节点、审批或其他业务锁序验收。
- 新鲜验证：四包 `go test -count=1`、同四包go vet、全后端 `go build ./...`退出0；PG容器Unix socket显式执行F1/F3/F4共12个顶层原生SQL/HTTP/并发用例，全部通过。未配置专用库的普通回归仍明确跳过真库测试。
- 还需A登记接入、测试证据和计划真实写适配、公共加密/URL与归属初始化、B真实证明提供方及主路由/页面/Docker联合验收。正式配置新增HTTP和生产入口本轮未启用，不能宣称整体上线。运行库复查32/dirty=false；没有迁移、提交、推送或新增results/evidence输出报告。

## 后续本地阶段：F3 控制快照、创建事务与真实并发（2026-10-21）

契约：[控制快照存储](../supply/supply-resources.draft.md#resource-control)。新增 `resource_state_store.go`、`resource_creation.go` 及对应单元/真库/并发测试；本节为未提交的本地业务实现，不属于文档清理PR。

- TDD红阶段实际执行：不安全schema被接纳；原生夹具准备后ID分配仍返回依赖不可用；创建工作单元仍为桩时，真实创建/外层回滚正向场景失败。实现后相同测试转绿。后续“把其他资源报告登记为自身报告”反例先失败，再补原生报告/任务归属校验转绿。
- A控制元数据已经真实持久化：复用34的resource列与operation_log.after中带保留动作RESOURCE_REVISION的supply.resource/v1快照，保存字符串revision、来源行/目标版本、配置绑定及所选报告任务序号。ID仅用于确定日志顺序，不从ID/时间/数量构造业务revision。该类控制记录必须保留；公共展示需翻译具体业务operation，不能直接展示存储标记。
- `TestResourceStateStorePostgres34Native`覆盖原生插入、读回、更新与旧revision拒绝、身份唯一冲突、最新损坏快照不回退、原生投影漂移、日志varchar真实SQL失败、空日志回调不被当成功、外层回滚及无配置提供方时依赖关闭。协议单元覆盖字符串大版本、中文长度、UTC微秒规范化、缺字段/未知字段/疑似凭据字段拒绝，不修改调用方对象。
- `TestResourceCreationServicePostgres34`组合已有真实领域服务与A原生存储，B提供方仍为受控事务回调。临时表恢复原生资源、任务、报告互相引用的FK：先建真实资源，再转存已选草稿报告，核对自身归属/连通和压力结论，再保存选定引用、控制头与申报行绑定。测试未分派409、错误办理人403、B行回写失败、转存后报告结论改变、缺转存提供方和外层回滚；失败后资源/报告/日志/行绑定均恢复为空。
- 同一申报创建重读返回created=false、保留原状态/时间/revision和报告，新增CREATE_NOOP日志而不替换控制头。成功资源为PENDING_ACCESS，尚未ADMITTED且无正式配置，读取仍不能报价。创建→真实控制头→真实所选报告读取的组合通过。
- `TestResourceConcurrentWritersPostgres34`在专用库public原生表短暂提交独立人员、组织、供应商、模型和资源夹具；两个READ COMMITTED事务竞争，第三连接用pg_blocking_pids确认真实等待。先写未提交时外部快照仍读revision1/原系数；先写提交后，后写返回RevisionConflict；最终revision2/先写系数，日志共2条，无过期写额外留痕。夹具按精确ID清理，后查resources/logs/staff/orgs均为0。该测试不替代真实B创建/接入混合锁序验收。
- `TestSupplyResourceHTTPPostgres34Native`已将A控制事实map替身换成真实StateStore写入＋SQL读取；GET仍验证列表/来源/详情/选项、权限及错误，无新增写日志。认证与故障注入边界保持受控；没有借此启用生产主路由。
- 新鲜验证：四包 `go test -count=1`、同四包go vet、`go build ./...`退出0；在PG容器Unix socket显式执行F1/F3共9个顶层原生SQL/HTTP/并发用例全部通过。普通回归未配置真库时仍明确跳过真库用例，不把跳过当真库通过。
- A剩余工作包括正式配置真实存储、绑定/接入/测试关联和计划的真实事务适配；B真实鉴权、不可变版本、报告转存与统一锁序、公共日志/幂等及生产主装配尚未联合验收。所有页面、API和Docker整体完成仍未成立。运行库复查32/dirty=false，本轮未迁移、提交、推送或新增散落results/evidence报告。

## 后续本地阶段：F3 原生资源仓储与真实 HTTP 读取（2026-10-21）

契约：[F3 PostgreSQL 读取](../supply/supply-resources.draft.md#resource-read)。新增 `resource_read.go`、`resource_repository.go` 及对应测试；本节为本地未提交实现，不属于文档清理 PR #5。

- 红阶段实际执行：非法 schema 被接纳的单元测试失败；原生 34 夹具已就绪时仓储仍返回依赖不可用，真实 PG 测试失败。实现三个读取方法与选项组合后转绿。
- 后续反例先失败再修复：仅检查 updated_at 未能识别原生系数被修改而控制快照未变；现改为逐列核对随 revision 保存的非敏感投影。待接入别名在筛选下漏行，现统一 PENDING_ADMIT/PENDING_ACCESS 的查询与展示。
- 输入边界反例先失败再修复：长搜索及NUL原先能到达仓储，现在关键词200/公司128字符限制及UTF-8/NUL校验在领域入口执行，包含中文字符边界；HTTP非法搜索返回400，不由SQL适配错误变成500。
- 专用库 `model_bss_a_schema34_20261020` 中复制原生 34 列到会话临时表，整体回滚；两模型、四资源、三区分来源和两个区域，验证同范围完整聚合后分页、资源与供应商/可报价数量、系数范围、全部 9 个分组排序字段和 10 个平铺排序字段、空页保留总数、字面 `%_` 搜索、来源展开过滤及筛选选项。
- 资源原生列、F1 当前版本与所选报告来自真实 SQL；B 来源/目标版本、A 控制快照及正式配置元数据由受控事实提供方注入，并断言使用同一 sql.Tx。测试不证明真实事实提供方已交付，不从 updated_at、报告数或页面参数生成 revision。
- 报告自身失败不计可用；过期报告显示 STALE 且不复用指标。NULL 与真实零分开，升降序均 NULLS LAST；数值主键补序、缓存大整数原始分数比较，无 float 或先舍入排序。原生读取没有选择测试 URL/密钥或供应商密码列，HTTP 输出检查未泄露这些字段/值。
- 真实 HTTP 测试 `TestSupplyResourceHTTPPostgres34Native` 使用真实注册器、处理器、领域 reader 和 SQL 仓储，覆盖 grouped/flat/详情/选项、他人全局读取但 can_edit=false、401/403/404/400、事实依赖失败503及损坏事实500；GET 不追加写日志。认证上下文为夹具，不是 main/JWT/生产联调。
- 新鲜验证：四包 `go test -count=1`、同四包 `go vet`、全后端 `go build ./...` 均退出0；另在 PG 容器 Unix socket 执行 F1 原生写/真实并发、选定报告、F3 原生读及 F1/F3 HTTP 六个顶层真库用例，全部通过。普通回归未配置专用库，会明确跳过真库用例。
- 当前读取在同快照物化完整匹配范围后排序/分页，保证外部事实参与可用数；尚未完成大规模查询优化。生产前仍需 A 控制快照持久化和正式配置真实适配、B 可信事实提供方、公共认证/主装配及页面/API/Docker 联验。不能以这组测试宣称三个页面整体完成。
- 运行库复查 `32 / dirty=false`。没有迁移、提交、推送、更新 PR 或新增终端输出报告。

## 后续本地阶段：F1 可配置官方来源策略（2026-10-21）

契约：[官方来源策略](../supply/supply-standard-models.draft.md#official-source)。新增真实 `OfficialSourcePolicy` 实现，替换 F1 原生写测试中的来源校验替身。

- 红阶段桩策略错误拒绝允许地址、接受非法配置，测试失败；实现后两个策略测试组通过。精确匹配公司允许的 HTTPS origin，默认443等价、显式端口隔离、无隐式子域放行；配置深拷贝。未知公司、任一来源不匹配、嵌入凭据/常见密钥查询参数等拒绝。
- F1 真库流程先注入不匹配来源配置，人工核验返回 ErrSourceRejected；再注入正确配置，完成核验、后续改价失效及历史读取。没有将域名匹配当作自动核验或网页内容真实性证明。
- 不进行事务内网络抓取，未硬编码厂商允许名单。部署仍需可信配置；没有配置时不能默认核验成功。四包回归、vet、全后端构建及上述 F1 真库流程本轮重新执行通过。

## 工作区与文档对齐（2026-10-21）

原开发目录已从91a322d快进到远端work-A-fix的2400df9，未改写历史；仍有本地业务修改，不代表这些修改已提交。同步前保存文件备份和Git stash，恢复后43个代码文件SHA-256全部与备份相同。

本地采用清理后的README与SUMMARY布局：evidence跟踪数0、忽略规则生效，当前目录不再保留分散的results报告。9份后续未提交的results原文保存在同步备份中，供追溯；其阶段结论按当前34基线理解，不重新当作必须补列的依据。150个相对文档链接及汇总锚点检查通过；此次未重跑业务测试或更新远端PR。

同步备份目录：`H:/Desktop/Dongxin/sync-backups/align-work-a-20261021-1792560576314/`。备份包含文件清单/hash与未提交diff；Git stash继续保留作为恢复保障。

---

## 后续本地阶段：F1真实并发写验收（2026-10-21）

新增 `internal/infra/supplypg/model_concurrency_integration_test.go` 验证既有34快照revision方案，没有修改生产实现。此为已有行为的集成覆盖，不把夹具错误记录为业务TDD红阶段。

- 在显式专用库 `model_bss_a_schema34_20261020` 使用原生public表创建唯一模型、真实内部人员及组织夹具，日志回调实际写operation_log。此前夹具误用固定人员7触发外键拒绝，已改为真实创建人员；手机号遵守varchar(20)。未修改表结构或绕开约束。
- 两个独立READ COMMITTED事务竞争同一模型，第三连接以pg_blocking_pids确认第二事务真实等待第一事务行锁，不靠固定sleep假定已阻塞。
- 第一事务尚未提交时，第三连接仍读取原资料，证明仓储未自行提交；第一事务提交后，第二事务读取新快照并返回ErrRevisionConflict，原更新保留。最终版本数/日志数各2，过期写没有部分效果。
- 单次通过后连续5次重复通过；清理后models/staff/orgs/logs对应夹具数量均为0。测试会短暂提交专用库夹具，结束后删除；不触碰运行库。
- 四包 `go test -count=1` 及同四包go vet退出码0。普通回归会跳过未显式配置真库的集成测试；上述5次为测试二进制通过PG容器Unix socket单独执行结果。
- 本测试仅证明READ COMMITTED下此锁顺序的冲突保护；其他隔离级别、真实公共日志工具、公共幂等及提交失败后的HTTP响应仍需联合验收。未提交或推送，未更新文档清理PR，未新增终端输出文件。

---

## 后续本地阶段：34选定报告读取（2026-10-21）

本节为尚未提交的新实现，不属于文档清理PR #5。新增 `internal/infra/supplypg/selected_report.go`，契约见[选定报告读取](../supply/supply-resources.draft.md#selected-report)。复用34原有报告、任务和资源字段，不修改表结构，不新增生产路由。

- TDD红阶段：在原生34报告临时表写入真实数据后，桩函数返回依赖不可用，`TestSelectedReportPostgres34`失败；实现后同一测试通过。
- 真实PG：专用库 `model_bss_a_schema34_20261020`、会话临时表、整体回滚。按指定报告读取，核对任务/资源/供应商/模型/区域绑定；不一致返回ReadContract，不存在返回NotFound。B的来源行/目标版本/序号由可信接口输入，测试ref是夹具，不证明真实B接口已装配。
- 已覆盖真实零耗时、NULL RPS、精确TPS、大于JS安全整数的缓存计数、非法缓存显示NOT_PROVIDED、未知压力结论与失败结论区分、B否定有效性不被重新放行。普通单元测试验证非法ID/版本/区域/schema在SQL前拒绝。
- `go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench`四包通过；同四包go vet与`go build ./...`退出码0。普通回归未显式开启真库测试，真库结果为单独编译测试二进制后在PG容器Unix socket执行该用例所得。
- 尚缺完整资源读取仓储、聚合分页、B真实事实提供方及生产装配。此函数只覆盖已有resource_id的报告；草稿测试仍由B管理，不虚构资源。
- 运行库复查仍32/dirty=false；没有执行迁移、提交、推送或更新清理PR。按要求不新增结果报告和Git终端输出文件。

---

整理日期：2026-10-21。基线：已合入 main 的 PR #3（合并提交 cffb655）。本文件合并该基线中的 31 份阶段结果报告；未包含其他工作区尚未提交的后续实现。

## 如何使用

- 本文是历史验证结论与交付边界的汇总，不代表今天重新执行了全部测试。
- 测试源码随业务代码维护；设计与关键计划仍保留，入口见 [README](./README.md) 和 [业务契约](../supply/README.md)。
- evidence/ 中的终端输出和截图属于本地运行产物，不再纳入 Git。旧提交仍可追溯原始报告及证据，本次没有改写历史。
- 下列测试数量属于各阶段当时的累计范围，不能相加为唯一测试总数；内存事务、受控 HTTP 与真库测试分别说明。

## 1. 标准模型库（F1）

已实现价格项精度、零价与缺失区分、计费单位和条件、八类资料完整性、人工核验、不可变版本、维护人和 revision 检查。允许保存待补齐资料；补齐及人工核验后才供报价。修改价格或资料会使核验失效。

读取和 HTTP 切片覆盖查询白名单、严格请求、内部身份、幂等依赖与错误映射。前端包含标准模型列表、单一维护入口、资料编辑与历史展示，以及公司/类型/供应商/区域多选。关联筛选要求供应商和区域匹配同一资源。

历史验证：独立领域最初19个顶层测试组通过、覆盖率89.2%；后续读取切片领域25组、API25组通过。前端从79项推进至多选筛选阶段90项；Vue浏览器场景使用真实组件及受控HTTP。价格精度、来源策略及响应身份等反例/变异被测试捕获。

数据库阶段已交付现有资料列编解码、事务内资料读取和元数据检查。专用测试库验证 JSONB/text[]/date、特殊字符、精确价格字符串、NULL拒绝及夹具回滚。该基线未交付完整标准模型业务仓储及生产主装配；旧结构差异属于当时方案的核对结论，不代表公共表不存在。

## 2. 模型广场与资源（F3）

已实现安全资源快照、按标准模型聚合、来源展开、详情、筛选选项、报告指标和缓存摘要。资源自己的当前连通性与压力测试均须通过；不能用批次或其他资源的结果放行。缓存只使用选定报告，真实零、未提供、不适用、未测试分别表示，不增加缓存命中率准入门槛。

列表先按相同范围筛选/聚合再分页，供应商数去重，系数范围保留十进制字符串。来源和详情继承筛选；凭据不进入快照。前端83项阶段回归、真实Vue受控HTTP场景通过；资源读取阶段领域48组、API26组通过。删除压力门槛及错误区域校验的变异均被捕获。

统一系数命令校验可信申报/计划来源，并提供CAS、留痕和outbox事务端口；该阶段96组后端累计回归通过。真实B证据、完整资源SQL仓储、事件投递及生产路由/页面仍待接入。

## 3. 正式配置（F4）

已实现正式配置新增、修改、停用的独立领域服务：绑定采购人、正式凭据与测试凭据隔离、版本和确认状态、引用保护、安全DTO及注入加密/URL策略。定向8组、当时领域33组及API25组通过；加密与持久化依赖为受控替身，不证明生产密钥配置或真实数据库写入已完成。

## 4. 工作台 A 子能力

已实现公告按人员读取、本人标记已读、他人只读投影、首次阅读时间保留和计划只读摘要。打开详情不自动已读；影响待计算与已计算0客户区分。工作台整体人员选择、四状态聚合和主导航属于公共能力。

前端阶段88项回归及真实Vue/受控HTTP场景通过。后端阶段领域55组、API31组通过。之后公告真实读取仓储以版本化payload验证人员组合、READ/UNREAD过滤后分页、HTTP身份及协议隔离；读取不会写状态。

公告写适配加入外层sql.Tx，真实PG验证首次/NOOP、日志SQL失败回滚、外层回滚、未知人员及并发锁竞争；不独立提交。生产公共日志回调、认证、幂等与主工作台装配仍需联合验收。

## 5. 上下架计划（F13）

当前规则以 DEC-010 为准：创建计划 → 原发起人登记实际完成；预计时间仅提醒，不自动变更资源。早期直接上下架领域/HTTP/页面结果是历史过程，直接HTTP路由和页面入口已撤下，不能作为当前可用入口说明。

已实现计划领域生命周期、执行事务服务，以及创建/修改/取消事务服务和HTTP入口。执行检查原发起人、实际外部操作确认、资源版本和当前准入；成功时状态/日志/事件共同保存，失败保持原子性，BLOCKED需由外层正确提交后响应。创建还拒绝已有同维度计划和待应用配置变更。

前端已实现计划列表/只读摘要及创建、修改、取消表单；403/409保留输入并停止继续提交，失败重试复用幂等键。浏览器实际覆盖409，403共用分支但未单独跑浏览器场景。最终相关前端98项回归、类型检查、Vite构建及广场/工作台浏览器回归通过，保留既有大包提示。

执行HTTP、真实SQL/公共幂等提交、提醒/事件投递及生产页面装配仍待完成。多个早期记录内的“待完成”应按本节最终阶段理解，不把计划维护完成误报为计划执行已上线。

## 6. 33/34 数据库适配与验证

在独立库复制运行库32结构后，直接执行000033/34的up SQL进行验证；该库不是由迁移运行器标记为34的运行库。运行库当时复查为32、dirty=false，未执行迁移。

已验证报告缓存bigint到字符串映射；非法/缺失计数不伪造0。资料空JSON对象只映射待补齐，非空旧对象拒绝静默丢字段。筛选选项真实SQL验证无资源公司保留、区域关联、供应商去重、55项limit+1截断、字面关键词及测试夹具清理。

该基线的结构检查记录了当时候选方案与33/34实际DDL差异、空身份唯一冲突和版本类型；不能据此断言已完成完整业务仓储、历史回填或生产联调。

## 7. 复现入口与验收边界

在对应提交及依赖环境中可运行以下命令；本文未因文档清理重新运行业务测试：

```text
# model-pricing-backend（需项目指定Go版本）
go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench
go vet ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench
go build ./...
# model-pricing-web
npm test
npm run typecheck
node node_modules/vite/bin/vite.js build
node tests/standard-model.browser.mjs
node tests/resource-plaza.browser.mjs
```

真实PG用例需要显式专用测试库环境，不设置时会跳过；普通go test通过不等于真库测试执行。真实登录、生产主路由、公共事务/日志/幂等提交、B/C事实、运行镜像与数据库版本必须另行联合验收。

## 8. 原阶段报告索引

以下保留每份原报告的主题与阶段范围，统一指向本文。原始详细红/绿输出可从合并前Git历史追溯，当前目录不保存终端转储。

| 原报告主题 | 阶段范围 |
|---|---|
| <a id="stage-01"></a>F1 独立领域切片执行记录 | 实际红/绿过程、19个测试组、变异验证、检查结果及未完成接入 |
| <a id="stage-02"></a>F1 多选筛选前端记录 | 四维多选、远程选项、90项回归与浏览器证据 |
| <a id="stage-03"></a>F1 多值筛选后端阶段记录 | 91组后端回归，前端和SQL仍待完成 |
| <a id="stage-04"></a>F1 标准模型库前端切片执行记录（v0.22） | 单维护入口、资料编辑、79项回归与浏览器验证，含截图与接入边界 |
| <a id="stage-05"></a>F1 PostgreSQL 资料读取适配记录 | 独立真库数组/JSONB/日期/精确价格往返、错误与回滚验证 |
| <a id="stage-06"></a>F1 读取与 HTTP 切片执行记录 | 接口交付、无变化写入留痕补正、回归结果和待接入边界 |
| <a id="stage-07"></a>F1 现有资料字段编解码适配 | 八类资料映射、旧结构拒绝、133组后端回归及交接需求 |
| <a id="stage-08"></a>F1 数据库结构检查及本地实查 | 缺列/类型报告、137组回归、本地仍32无目标表 |
| <a id="stage-09"></a>F3 模型广场前端执行记录（v0.22） | 聚合读取页面、83项前端回归、截图与真实接入边界 |
| <a id="stage-10"></a>F3 资源安全读取与模型广场聚合执行记录 | 逐资源可用性、聚合与精度验证、48组领域回归及待接入项 |
| <a id="stage-11"></a>F4 正式配置独立切片执行记录 | 新增/修改/停用领域交付、变异测试及公共接入边界 |
| <a id="stage-12"></a>F1/F3 筛选选项真实 SQL 查询 | 全局公司/资源区域关系、去重、55项截断与真库验证 |
| <a id="stage-13"></a>工作台 A 公告真实读取仓储验证 | 版本化payload、人员组合筛选分页、真实HTTP/SQL验证及清理 |
| <a id="stage-14"></a>A 公告阅读写入的真实事务适配 | 外层事务/日志注入、首次与NOOP、SQL失败回滚、真实并发与时间精度 |
| <a id="stage-15"></a>F13 创建计划的待应用配置变更门槛 | 创建计划拒绝待应用变更，依赖失败关闭，125组后端回归 |
| <a id="stage-16"></a>F13 创建上下架计划页面 | 原型表单、已有计划门槛、94项前端回归 |
| <a id="stage-17"></a>F13 计划执行事务服务 | 成功与BLOCKED事务、失败回滚、112组回归及外层提交边界 |
| <a id="stage-18"></a>F13 计划维护 HTTP 切片 | 创建/修改/取消、严格请求、认证/幂等及120组回归 |
| <a id="stage-19"></a>F13 上下架计划领域生命周期 | 创建/修改/取消/执行方案、原发起人、BLOCKED及108组回归 |
| <a id="stage-20"></a>F13 资源计划列表读取 | 资源范围、分页/只读前提、124组回归 |
| <a id="stage-21"></a>F13 资源详情计划列表前端 | 状态筛选、只读详情、失败重试和92项前端回归 |
| <a id="stage-22"></a>F13 计划创建、修改、取消事务服务 | 创建/修改/取消、修订与提醒端口、116组回归 |
| <a id="stage-23"></a>F13 修改/取消计划页面验证记录 | 原发起人、版本/幂等、冲突保留、98项前端回归及真实Vue场景 |
| <a id="stage-24"></a>F3 统一采购系数命令阶段记录 | 可信申报/计划来源、事务留痕/outbox端口、96组回归 |
| <a id="stage-25"></a>F3 直接上下架 HTTP 接入记录 | 严格登记请求、幂等重放及104组回归，页面尚待接入 |
| <a id="stage-26"></a>F3 直接上下架领域切片 | 实际操作确认、绑定采购人、上架前提及101组回归 |
| <a id="stage-27"></a>F3 上下架登记页面切片 | 实际平台确认、同键重试、冲突保留及92项前端回归 |
| <a id="stage-28"></a>最新main字段核对与首批适配 | 逐表差异清单、000034缓存转换、129组后端回归 |
| <a id="stage-29"></a>000033/000034 独立测试库验证记录 | 真实元数据检查、草稿唯一冲突、缓存字段；运行库仍32 |
| <a id="stage-30"></a>工作台 A 前端子组件执行记录 | 88项回归、真实Vue浏览器流程与截图 |
| <a id="stage-31"></a>工作台 A 公告阅读与计划摘要执行记录 | 55组领域/31组API回归、3次变异及真实接入边界 |
| <a id="stage-32"></a>F13 计划实际完成 HTTP 切片 | 原发起人请求契约、EXECUTED/409 BLOCKED响应、错误输入及依赖校验；公共主装配和409幂等重放待接入 |
| <a id="stage-33"></a>F1 标准模型操作记录查询适配 | 公共查询路由复用、bigint筛选和DTO映射；99项测试及类型检查通过，浏览器环境依赖缺失 |
| <a id="stage-34"></a>F3 资源接入与正式配置绑定 HTTP 契约 | A注册器提供严格的逐资源接入/配置绑定端点；定向契约及相关Go包回归通过，公共装配、B真实提供方和前端表单仍待完成 |
| <a id="stage-35"></a>F4 正式配置新增/更新/停用 HTTP 契约 | A注册器写接口、凭据安全投影、版本及引用冲突测试通过；列表读取、主装配、供应商页面仍待完成 |
| <a id="stage-36"></a>本机 PostgreSQL 迁移对齐 | 备份并在副本演练 000033–000036；本机运行库升级到 36、dirty=false；API 容器尚未替换 |
| <a id="stage-37"></a>v36字段对照与资源配置/接入页面 | 实际库36只读核验、F1结构兼容及A原生PG/API包回归、前端100项与5个浏览器场景通过；公共入口仍待装配 |

## Stage 32：F13 计划实际完成 HTTP 切片

按计划执行契约先写 `POST /api/internal/plans/:id/execute` 测试。首轮红测因 `SupplyDeps` 缺少执行服务依赖而无法编译；实现 A 注册器的 `PlanExecutor` 端口和 handler 后，测试发现HTTP层未拒绝 `confirmed:false`，补齐该边界后切片通过。

请求只接收规范十进制字符串 `expected_revision`、`confirmed:true`、非空 `completion_note` 和 `reason`。操作者从认证StaffID与RequestID构造。handler核验执行器返回的计划ID、发起人、revision、状态和完成时间；成功返回EXECUTED，领域服务以无error的Conflict结果交给外层事务后，handler返回含BLOCKED计划信息的409。

本轮 Docker Go 环境执行：

```text
go test ./internal/api -run '^TestPlanExecutionHTTP' -count=1
go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench
```

两组命令通过。该 API 测试使用执行器和幂等存储替身，不证明真实 HTTP/PG提交顺序。工作区未设置 `SUPPLY_SCHEMA_TEST_DB`，本轮四包回归未执行专用PostgreSQL集成用例；计划存储阶段此前的真实BLOCKED外层提交证据仍见计划并发测试记录。

公共幂等中间件当前把4xx标记为FAILED，幂等重放对DONE固定包装为HTTP 200。故本轮仅验证首次BLOCKED响应为409且携带计划，不宣称409响应已被共享幂等仓储保存或可重放。公共 `registerSupplyRoutes` 为空，主路由尚未注入 `PlanExecutor`；在公共支持409终态响应快照并完成主装配前，`can_execute` 和前端实际完成表单保持关闭。交接要求见[公共接入请求第15节](../supply/handoff.md#handoff)。

## Stage 33：F1 标准模型操作记录查询适配

检查发现 `StandardModelList` 的维护记录页请求 `/internal/operation-logs`，该路径在当前公共代码中不存在；现有真实查询为内部受保护路由 `GET /api/internal/workbench/operations`，查询参数使用 `operator_id/entity_type/entity_id/page/size`，分页行提供 `action/entity_type/entity_id/operator_name/reason/created_at`。它在主路由装配 `WorkbenchInboxSvc` 后开放，并继承内部 AuthN。

先新增严格 MSW 场景后运行 `node tests/run.mjs`，首轮按预期因旧地址没有匹配处理器而失败。随后 A API 改为复用现有路由，以 `entity_type=standard_model` 和十进制字符串 `entity_id` 精确筛选，并将返回字段映射为操作、操作人姓名、说明和创建时间；界面移除该接口不提供的 `operator_id/request_id` 列。浏览器场景夹具也切换到真实路径、snake_case参数和公共DTO。

验证结果：

```text
node tests/run.mjs                                      PASS，99项
node node_modules/vue-tsc/bin/vue-tsc.js --noEmit       PASS
node node_modules/vite/bin/vite.js build               PASS
```

Vite构建有既有主bundle超过500 kB的提示。本地环境未安装 Playwright，直接运行 `node tests/standard-model.browser.mjs` 在加载阶段以 `Cannot find module 'playwright'` 退出；因此本轮不报告浏览器表格场景通过。MSW场景证明请求路径、过滤参数、bigint字符串保留和DTO映射，不证明真实登录或生产主路由已接入。公共路由、数据库和容器未修改。

## Stage 34：F3 资源接入与正式配置绑定 HTTP 契约

按契约先为 `POST /api/internal/resources/:id/admit` 与 `PUT /api/internal/resources/:id/formal-config` 增加 HTTP 测试。首轮红测在编译阶段因 `SupplyDeps` 尚无接入服务端口而失败；随后在 A 的 `RegisterSupplyRoutes` 添加两个服务接口、依赖字段和幂等路由，并实现严格请求解码、认证操作者/request ID 注入、bigint 字符串解析、服务返回契约校验及不含凭据的回执。

接入只接受外部完成确认和说明，不接受客户端自报办理人、审批或测试事实；配置绑定只接受资源与配置 ID/预期版本，不接受状态、URL、密钥或“已确认”字段。两个 handler 都从认证上下文获取操作者，依赖缺失返回503，版本/业务冲突复用供应侧错误映射。幂等重放由当前 A 注册器中已有公共中间件处理；该切片不改变公共 `registerSupplyRoutes` 主装配。

验证结果：

```text
go test ./internal/api -run 'TestResourceAdmissionHTTP|TestResourceConfigBindingHTTP' -count=1  PASS
go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench  PASS
go vet ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench  PASS
go build ./...  PASS
git diff --check  PASS（仅报告既有的LF/CRLF提示）
```

Go命令运行于 Go 1.26 Docker 容器；没有传入专用真实PG集成测试库配置，因此不把本轮 package pass 作为数据库集成测试执行证据。服务使用领域服务替身的HTTP场景证明请求校验、操作者传递、bigint字符串、响应安全字段、幂等回放及常见错误映射，不证明真实B proofLoader、公共主路由、事务提交/幂等数据库或前端页面已接入。接入及绑定页面表单仍未完成。

## Stage 35：F4 正式配置新增、更新与逻辑停用 HTTP 契约

对照详细设计 `/api/internal/suppliers/:id/formal-configs` 与现有 `FormalConfigService`、原生34存储适配，先新增 A 注册器 HTTP 场景。首轮 RED 因 `SupplyFormalConfigWriter` / `SupplyDeps.FormalConfigs` 尚不存在而编译失败；实现三条严格写路由及依赖端口后转绿。

- `POST /api/internal/suppliers/:id/formal-configs` 创建正式配置；需要scope、URL、API Key、`external_confirmed:true`及原因。
- `PUT /api/internal/suppliers/:id/formal-configs/:configId` 更新配置；expected_revision为字符串，可省略Key保留原密文，传入空Key拒绝。
- `DELETE /api/internal/suppliers/:id/formal-configs/:configId` 逻辑停用；共享资源引用冲突映射409，不物理删除。
- 三条路由均要求内部Staff、RequestID和幂等键；supplier采购人授权由既有领域服务与事务事实判定，HTTP不接受客户端自报buyer_id/区域/状态。
- 回执只返回 `FormalConfigView` 安全字段，固定掩码保留，不返回明文Key、密封信封或密钥版本。错误映射覆盖绑定拒绝403、版本/引用冲突409及缺依赖503。
- 更新允许在最大bigint revision执行不变更no-op；是否能递增仍由领域服务原子检查，HTTP不提前禁止有效no-op。

验证结果：

```text
go test ./internal/api -run '^TestFormalConfigHTTP' -count=1  PASS
go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench  PASS
go vet ./internal/domain/supply ./internal/api ./internal/infra/supplypg ./internal/domain/workbench  PASS
go build ./...  PASS
```

覆盖新建幂等回放与Key不回显、操作者/RequestID来源、更新省略Key、bigint revision、最大revision no-op、逻辑停用、引用冲突、未知字段/数值revision拒绝、匿名/外部门户拒绝及服务端403/409映射。Go命令在Go 1.26 Docker容器运行；四个包回归、vet和全后端build通过。本轮未传专用真实PG集成测试库配置，因此不把本轮 package pass 作为数据库集成测试执行证据。当前 A `RegisterSupplyRoutes` 已注册写端点，但 `GET` 正式配置列表和公共 `registerSupplyRoutes` 主装配尚未实现；也没有接入公共密钥实现或供应商详情浏览器表单。本测试使用FormalConfigService接口替身，不证明真实采购归属/加密或数据库事务；这些能力分别由先前领域与原生存储证据覆盖，生产接线仍待联合验收。

## Stage 36：本机 PostgreSQL 迁移对齐

迁移前确认本机 `model_bss_pg` 为 PostgreSQL 15，版本 `32 / dirty=false`；000033 要创建的 30 张表与现有 54 张 public 表无名称冲突。相关旧表 `internal_staff` 有 11 行，`todo_task` 有 0 行。先生成并验证自定义格式 pg_dump 备份，再恢复到隔离数据库，通过项目现有 `golang-migrate` 工具完整演练 000033–000036。

演练结果：副本从 32 升至 36，`dirty=false`；11 条员工记录保留；`v_work_item` 可查询；新增 `can_approve_supplier` 默认为 false。随后对本机运行库执行相同迁移并只读复核：当前版本 `36 / dirty=false`，员工数仍为 11，`test_report` 两个缓存字段存在，工作台视图可查询。没有给任何员工授予审批权。

本轮 `go build ./cmd/server` 在 Go 1.26 Docker 环境通过，产物落在 Git 忽略的 `model-pricing-backend/bin/server-v36`。随后用该产物重建 `model_bss_api_latest`，保留原配置与数据库连接参数；`GET /healthz` 返回 HTTP 200，payload 为 `{"db":"up","status":"ok"}`。旧容器保留并停止为 `model_bss_api_pre_v36`，可作为应用容器回退副本。迁移前 pg_dump 备份保存在本机临时目录，供必要时恢复；迁移后的新业务写入会使回滚 000033 变为有数据损失风险。本阶段确认数据库、当前源码构建的 API 容器及健康检查已对齐；由于公共 SupplyDeps/main 主装配、业务路由/页面和 B 提供方仍有待接入项，不代表 F1/F3/F4 业务闭环的 Docker 联合验收完成。

## Stage 37：v36 字段对照与资源配置/接入页面

对照仓库迁移确认当前基线是000033–000036：000033提供标准模型、资源、供应商正式配置、任务报告、计划及操作日志等原生表；000034增加报告缓存计数；000035创建工作台投影视图；000036只增加`internal_staff.can_approve_supplier`。没有为本切片新增迁移。

逐字段检查时，`resource.revision`及来源/目标/报告控制元数据由保留的`operation_log.after`中`supply.resource/v1`快照承载；正式配置状态、版本、核验依据由`FORMAL_CONFIG_REVISION`安全快照承载，区域从同一供应商行推导。它们是应用投影字段，不要求物理列。此前只看DDL列名便判断为“缺字段”是错误结论，已在[结构基线](../supply/requirements.md#superseded-proposals)更正。本轮只读查询实际运行库再次确认`36|false`，实际列与仓库迁移一致。

按TDD新增`resourceOperations`请求契约与浏览器场景，并在`ResourceDetailDrawer`增加“接入与配置”面板：只显示同供应商、同区域、已人工确认的正式配置，不暴露凭据；仅当本资源自己的连通性和压力测试均通过且配置已绑定时允许登记接入；登记要求外部实际完成确认及完成依据，操作者/办理节点由服务端校验。页面不允许调用者选择办理人。

验证：

```text
npm test                                              PASS，100/100
npm run build                                         PASS（Vite有既有chunk >500 kB提示）
tests/standard-model.browser.mjs                      PASS
tests/resource-plaza.browser.mjs                      PASS
tests/plan-maintenance.browser.mjs                    PASS
tests/workbench-a.browser.mjs                         PASS
tests/resource-operations.browser.mjs                 PASS
```

此外，在Go 1.26临时容器中，四个A相关Go包单元回归通过；连接独立测试库`model_bss_a_schema34_334c39`的F1元数据测试通过（`columns_compatible=true`，无缺失/不兼容列），`internal/infra/supplypg`与`internal/api`完整包回归通过。PG集成测试只写专用测试库；运行库只读核对，没有迁移或业务写入。

浏览器场景使用测试专用Vite入口和模拟HTTP，不证明公共真实路由/认证/数据库联调。资源操作页面本轮通过组件级交互验证。当前A依赖组合还未由公共`registerSupplyRoutes`装配，前端正式菜单仍未接此页面；批准改价页面和真实B流程/报告提供方也未完成。运行API容器没有重建成本轮A工作区代码，因此这些结果仍不是生产路由端到端验收。未提交或推送。

## Stage 38：v36结构下的A模块组合入口

此前把公共`main/Deps`装配尚未交付当作继续实现A侧依赖组合的前置条件，导致A自身可以独立完成的构造入口仍未补上。现已新增`NewSupplyRouteModule`，把已有A服务组合为一套`SupplyDeps`：标准模型、资源读取/筛选、接入与配置绑定、正式配置读写、计划读取/维护/执行、公告读取/已读写入；同时提供B流程可调用的资源创建和测试证据关联服务。A不接管公共路由注册或B办理事实。

构造器要求模型SQL写入和公共幂等仓储使用同一SQL连接池，拒绝把幂等与业务事务分到两个池；对身份、资源/配置/公告留痕、密钥封装、正式URL策略、接入证明/报告/申报行/outbox、资源创建来源锁/回写/报告转存、测试报告权限及待应用配置检查逐项校验。缺提供方时统一失败关闭，不填no-op成功实现。Notice读取当前固定公共schema，因此构造器显式要求`public`。返回给B的`ResourceCreation`和`TestEvidence`仍需由公共/B真实流程接入。

TDD先新增构造器缺依赖、全服务组合、连接池不一致三类场景。首轮Docker编译发现构造器缺少context导入，测试选项也需显式初始化嵌入的`StandardModelSupplyOptions`；修正后转绿。Go 1.26 Docker中执行：

```text
go test ./internal/api -run '^TestNewSupplyRouteModule' -count=1  PASS
go test -count=1 ./internal/api                              PASS
```

只读查询运行PostgreSQL仍为`36 / dirty=false`；本阶段没有执行迁移、运行库业务写入或重建API容器。当前结果证明A组合入口可编译、API包回归通过，不证明公共main已经传入真实适配器或Docker业务接口已开放。公共负责人仍需在`main/Deps`调用此构造器并接入认证、日志、加密与幂等事务；B需提供报告、申报行、节点/办理权限及outbox真实实现。未提交或推送。

## Stage 39：A 模块组合器接入 36 版隔离库

为把 Stage 38 的构造测试推进到真实SQL读写，新增`TestSupplyRouteModuleReadsV36SchemaThroughRealHandlers`，并把F1创建/日志/幂等原子性测试改为经完整`NewSupplyRouteModule`装配。读测试先检查隔离数据库的缓存列、`v_work_item`和审批资格列，确认33–36结构；随后创建仅用于测试的内部人员，把`module.Deps`交给A的`RegisterSupplyRoutes`，实际请求标准模型、资源、本人公告和计划列表。首轮测试因夹具使用的员工ID在隔离库不存在而暴露公告读取依赖；改为创建/清理测试员工后，四组GET均返回200。写测试通过同一完整构造器创建标准模型，并检查`created_by`、版本行、operation_log及`idempotency_key`均已提交；同键重放返回相同模型且不重复留痕。

所有数据库操作明确连接`model_bss_a_schema34_334c39`，不连接运行库`model_bss`。使用Go 1.26 Docker执行：

```text
go test ./internal/api -run '^TestSupplyRouteModuleReadsV36SchemaThroughRealHandlers$' -count=1  PASS
go test ./internal/api -run '^TestSupplyRouteModuleCommitsBusinessLogAndIdempotencyTogether$' -count=1  PASS
go test -p 1 -count=1 ./internal/api ./internal/infra/supplypg                              PASS
```

该结果证明完整A构造器在迁移字段齐备的真实PostgreSQL库支持F1/F3/F13列出的GET，以及F1创建操作与公共幂等中间件同事务写入、提交和重放；API与仓储完整包回归通过。它没有调用公共的`registerSupplyRoutes`：当前该函数仍为空；除F1创建外，没有通过真实JWT/认证、B流程与报告提供方，也没有覆盖其他写路径和所有业务的外层幂等提交。运行API容器仍指向`bin/server-v36`，本轮未覆盖该二进制、重建容器或写入运行库；Docker健康只证明服务连到数据库。公共主装配与B真实提供方接通后仍须做完整业务端到端验收。未提交或推送。

## Stage 40：本机 A 页面与 API 接线

为尽快让标准模型库、模型广场和工作台相关接口在当前项目入口可用，新增`Deps.SupplyRoutes`并由公共内部受保护路由调用`RegisterSupplyRoutes`；`cmd/server`装配 v36 的标准模型读写、资源与正式配置读取、公告读取/本人已读、上下架计划读写/执行。计划和公告操作复用当前幂等事务与`operation_log`/`event_outbox`。资源来源的申报审批、测试报告与正式配置密钥仍来自可信数据库事实/密钥配置，不由请求体或默认值伪造。

前端内部菜单已将“标准模型库”接到`StandardModelList`，“模型广场”接到`ModelPlaza`。构建并重启当前挂载二进制的`model_bss_api_latest`；健康检查HTTP 200。未认证访问标准模型、资源、公告、计划路由均返回401，说明路由已挂载并受内部认证保护。当前浏览器停在员工登录页，未代填账号密码。

验证：Go 1.26 Docker环境下`go test -p 1 -count=1 ./internal/api ./internal/infra/supplypg ./cmd/server`通过，其中API与`supplypg`集成测试连接专用v36隔离库；前端`npm test` 100项通过，`npm run build`通过（保留既有bundle体积提示）。正式运行库只读确认仍为`36 / dirty=false`，未运行迁移；未提交或推送。

## Stage 41：工作台 A 公告详情接入

当前工作台对待阅行只有公共通用标记已读操作；点击没有 deeplink 的公告时提示“详情页建设中”。A 已有公告详情和个人阅读组件/API。本阶段在工作台为已知供应侧公告类型接入 A 的 NoticeDetailDialog：本人查看使用认证人员默认上下文；切换查看他人时传入被查看人员并设为只读；打开详情不标记已读，详情内写入仍走 A 的个人阅读接口；待阅操作按钮显示“查看公告”。没有改公共收件箱聚合或状态规则。

验证：

- model-pricing-web 执行 npm.cmd run build：通过，包含 Vue TypeScript 检查和生产构建；仅保留既有 bundle 超过 500 kB 提示。
- 本机 API 健康检查返回 {"db":"up","status":"ok"}；前端根页 HTTP 200。
- 新开的本机标签因未继承已有登录态而进入登录页；本轮未生成公告/模型业务数据，未执行浏览器内公告读写，不声称真实公告闭环通过。
- Docker CLI 仍因 Docker Desktop pipe Access is denied 无法查看容器；没有查库、写库、执行迁移或重建容器。

本阶段只验证前端编译和构建。现有工作台投影仍将所有 notice 表记录呈现为通用 NOTICE；A 详情入口按当前已知供应侧 kind 映射。PLAN 项目的公共投影仍只传 resource_id，不能打开具体计划；该导航信息需公共负责人补齐。

## Stage 42：工作台计划待办资源深链

公共工作台为 PLAN 待办生成的路径是 /internal/model-plaza/{resource_id}，原前端只有不带 ID 的模型广场路由，点击会落到未匹配页面。新增 A 资源深链路由，复用 ModelPlaza 并在详情抽屉中直接打开关联资源的“计划”页。由于公共 v_work_item 当前只传 resource_id 而非 plan_id，页面展示该资源的计划列表，用户再选择具体计划；没有把 resource_id 冒充 plan_id，也没有开放尚未满足幂等条件的登记完成操作。

验证：model-pricing-web 执行 npm.cmd run build，通过 Vue TypeScript 检查和生产构建；仅有既有 bundle 体积提示。未执行浏览器真实待办跳转（当前没有可用的已登录新标签和计划数据），未写数据库；Docker 仍无法访问。

## Stage 43：资源报告有效性 fail-closed

核对运行装配时发现报告读取回调曾把数据库中存在的 `test_report` 行直接设为 `Valid=true`，没有核对 B 已验证并选中的报告证据。现改为从资源操作留痕中查找与当前报告 ID、序号、来源版本和目标版本一致的 `SELECTED/NOOP` 证据；只有留痕保存的有效性为真才返回有效。首次由申报创建资源并通过可信报告校验时，也在初始资源版本快照中保存这组证据；后续报告关联留存 B 验证的有效性。旧记录缺少匹配证据时按无效处理，不影响报告查询展示，但不允许准入或上下架前置检查据此放行。

验证状态：新增了覆盖缺失、无效及字段不匹配情形的定向单测，但本轮未运行 Go 测试；当前 PowerShell 会话找不到 `go.exe`/`gofmt.exe`。尝试运行 `gofmt` 得到命令不存在。`git diff --check` 对 Git 跟踪文件通过，但新增的 Go 文件属于未跟踪状态，不在该命令检查范围内。Docker Desktop pipe 此前返回 Access is denied，本轮未重复尝试，也未做数据库或容器写入。以上代码尚未通过编译或运行时验证。

## Stage 44：本机 A 页面只读冒烟与计划执行入口核查

使用本机内部测试账号登录前端，实际打开标准模型库、模型广场和工作台及操作记录页，均显示空状态且无可见加载错误；标准模型新增表单可打开，未提交业务数据。三页当前库内无模型、资源、计划或公告，因此本轮不声称新增、报告、准入、计划或公告写入闭环已跑通。

计划执行按钮继续保持关闭：A HTTP handler 可以在事务中返回带 BLOCKED 计划数据的 409，但共享幂等仓储仍把 4xx 标记为 FAILED，重放也固定按 200 成功信封返回。公共侧需先支持持久化和重放原 HTTP 状态及业务快照；不能只打开前端按钮或绕过该协议。

## Stage 45：审批页前端直接路由资格守卫

`AppLayout` 已按 `can_approve_supplier` 隐藏审批菜单，但路由守卫此前没有读取 `requiresApproval`，直接输入 `/internal/approvals` 仍能进入占位页面。现由路由守卫按登录响应中的 `canApproveSupplier` 拦截；未授权账号去 `/403`，有资格账号可进入。未添加角色推断，也未更改 000036 或后端审批 API。

验证：仅编译并运行 `auth.scenario.ts` 定向场景，2/2通过（无资格直接路由拒绝、有资格允许，以及原有认证/跨门户重定向场景）；`npm.cmd run build` 通过，只有既有大 chunk 提示。本机已登录但无审批资格的员工直接访问 `/internal/approvals` 后实际落到 `/403`。服务端 `/quotes/:id/approve` 尚未检查 `can_approve_supplier`，具体交接已写入 `public-integration-request.md`；前端守卫不代表后端鉴权完成。

## Stage 46：当前工作区 Docker API 冒烟

只读确认 PostgreSQL 迁移为 `36 / dirty=false`。原 API 容器挂载的二进制早于 `supply_runtime.go` 等当前源码；使用现有 Go 1.26 Docker 环境将当前工作区编译到忽略路径，构建通过。保留旧二进制备份后，更新原 `model_bss_api_latest` 容器所挂载的 `server-v36` 并重启；未迁移数据库。

当前 8080 API 的 `/healthz` 返回 200，未认证访问标准模型路由返回 401；用本地测试账号登录后，标准模型和模型广场列表均返回成功，当前记录数均为 0。前端开发服务器根页返回 200。没有创建业务数据，因此这里只证明当前工作区 API 已在 v36 容器环境启动、认证及列表读取可用，不代表新增、核验、资源准入或配置写入闭环已通过。旧容器二进制备份和新构建均在忽略的 `model-pricing-backend/bin/` 下；未提交、推送或执行迁移。

## Stage 47：供应商申报页面最小闭环

补齐 A 任务范围内的供应商门户模型新增页面：支持一份多行申报、标准模型安全目录选择、显式保存草稿、保存后的逐行连通性/压力测试启动与刷新、批次测试覆盖提示和申报提交；草稿/申报记录页签读取列表并开放可编辑草稿。测试状态由接口返回，页面区分“做过测试”和“测试通过”；每行连通性已测且至少一行压力测试已测才开放批次提交。价格变更仍从模型管理页按资源及原 revision 发起，不直接改资源。

验证：`npm.cmd run build` 首次暴露两个模板事件的隐式 `any`，修正事件处理后再次构建通过。Vite保留既有 500 kB chunk 提示。没有运行完整测试套件，没有创建或修改业务数据。`127.0.0.1:8080/healthz` 返回健康、`127.0.0.1:5173/` 返回 HTTP 200；本轮 Docker CLI 仍因 Docker Desktop pipe `Access is denied` 不可检查，所以未确认当前 Docker 容器是否包含本轮前端源码。

真实供应商申报 API 尚未可联调：申报/办理/测试相关业务模块的接口仍未开放；`/supplier/models`、安全标准模型选项、草稿/逐行测试/提交与价格变更的接口尚未由真实提供方提供。详细字段和隔离要求已补入[供应商门户跨模块接口契约](../supply/handoff.md#handoff)。旧单模型 `/supplier/model-applications` 未复用。源码与页面构建通过，不等于供应商门户页面/API真实可用。未提交或推送。

## Stage 48：F4 正式配置维护入口

模型广场资源详情的“接入与配置”页新增正式配置新建、修改、停用及资源绑定入口；Key 使用密码输入框，更新留空表示保留，页面只显示“已设置”状态，不回显已保存密钥。新增/修改要求明确确认外部操作，停用要求填写原因。修正列表状态契约，使前端接受后端 `ACTIVE/INACTIVE`，避免有效配置列表因 `DISABLED/INACTIVE` 不一致而报契约错误。

验证：`npm.cmd run build` 通过 Vue 类型检查与 Vite 生产构建，仍有既有大 chunk 提示；没有运行全量测试、没有写入数据库。当前运行装配仍未设置 `FormalConfigs` writer、生产 `SecretSealer` 与 `FormalURLPolicy`，所以新增/修改/停用请求仍应返回503。具体公共装配交接已写入[集成请求](../supply/handoff.md#handoff)。本阶段证明界面与请求层可构建，不证明正式配置真库写入可用。未提交或推送。

## Stage 49：供应商申报撤回入口

申报记录页按服务端 `permissions.can_withdraw` 展示撤回操作；操作要求填写原因，提交当前申报 revision 并使用独立幂等意图。成功后刷新记录列表，失败保留在当前页显示服务端错误；未提供撤回资格时不显示按钮。

验证：`npm.cmd run build` 通过 Vue 类型检查和 Vite 生产构建，保留既有大 chunk 提示。未运行全量测试、未创建业务数据。撤回 API 属于 B 的 `router_submission.go`，目前仍未实现；本阶段验证前端可构建，不代表撤回已真实联调。未提交或推送。

## Stage 50：三模块导航修复与本地运行恢复（2026-10-22）

标准模型新增保存后，后续冲突重载改用服务端返回的已创建模型 ID，避免仍使用初始空 ID 而无法重载。工作台收件箱和操作记录请求增加代次检查，切换人员时清空旧收件箱、关闭旧公告；迟到响应不覆盖新筛选结果。事项提供独立详情按钮。公告与计划详情增加关联资源导航，保留区域，并进入对应的公告或资源资料页；模型广场深链支持指定页签且正常加载背景目录。

验证：仅执行一次 `npm.cmd run build`，Vue 类型检查和生产构建通过，保留既有大 chunk 提示；受影响文件 `git diff --check` 通过。未运行全量测试，未新增测试终端证据。未在浏览器验证这次新增的写入后重载、请求竞争和详情跳转。

恢复运行环境时，默认权限 Docker CLI 仍报 pipe Access is denied；提升工具权限后可读取现有容器，确认数据库容器停止。启动原 `model_bss_pg` 并重启原 `model_bss_api_latest`，未修改配置、迁移或数据库业务数据。只读查询 `schema_migrations` 返回 `36|f`，即版本36、dirty=false。启动当前工作区前端开发服务于 `127.0.0.1:5173`。

使用本地测试账号真实登录后，标准模型、分组资源、公告、计划、工作台待办和操作记录六个列表均返回 code=0，total=0；完成后退出本次测试会话。本次证据证明运行库版本、认证列表读取和前端构建，不证明模型新增/核验、资源配置/接入、计划执行或个人已阅写入的完整业务闭环。正式配置运行 writer/公共密钥能力、B 的真实办理/报告提供方及计划执行公共幂等409提交重放仍待接齐。未修改 main.go/router.go，未提交或推送。

## Stage 51：计划完成表单及资源资料追溯（2026-10-22）

补齐 A 的实际完成登记组件 `ExecuteStatePlanDialog`、POST execute 请求和执行回执契约。表单要求原发起人资格、服务端执行能力、外部操作确认、完成依据及登记原因；使用计划 bigint 字符串 revision，回执核对计划/资源/发起人/资源基准及准确下一版本，区分 EXECUTED 与 BLOCKED。403/409 保留输入并停止继续提交，只有匹配的 BLOCKED 回执才显示已阻断结果。当前运行服务仍不发放 can_execute，现有摘要校验仍关闭此能力，执行按钮未开放；公共提交/重放能力验证前不改变该边界。

资源详情新增分页操作记录，复用公共 workbench/operations 的 resource 实体查询；只消费操作、姓名、说明和时间，不读取操作快照或凭据。资源详情可打开关联标准模型资料，维护人权限仍来自模型 API，保存后刷新资源及上层目录的报价可用状态。计划详情显示 BLOCKED 原因，操作后重新读取当前计划。

验证：仅运行新增的 plan-execution.test.ts 两条定向用例，2/2通过；npm.cmd run build 通过（既有大 chunk 提示）；最后一处资源资料入口修改后补跑 npm.cmd run typecheck，通过。未跑全量测试，未生成终端输出证据，未写运行库业务数据，未修改公共装配或迁移，未提交或推送。本阶段是前端源码及契约检查通过，未宣称执行/资源准入或真实页面写入闭环通过。配置加密/运行 writer、B真实报告/办理提供方和公共计划幂等协议仍为接入依赖。

## Stage 52：v0.22 原型的页面与视觉对齐（2026-10-22）

实际在浏览器打开原型，核对最终生效的样式及标准库、维护弹窗、模型广场和供应来源页面。原型最终使用浅色蓝色（主色 #315ee8），供应来源是独立详情页，标准模型库提供标准模型/操作记录页签；不能只按文件前段的旧绿色 CSS 判断最终样式。

本轮为标准模型库增加页级操作记录页签，复用公共操作记录的 standard_model 实体分页查询；基础筛选按公司、类型、节点、搜索排列，额外筛选保留在更多筛选中，表头允许换行、列宽适配容器。模型广场供应来源从弹窗改为独立内容页，保留返回入口和原筛选范围；表格补报告查看、上下架计划及登记接入入口，复用现有详情和计划组件。A 模型/资源组件局部配色改为原型蓝色，未修改公共主题、认证或菜单。

验证：前端构建通过，只有既有大 chunk 提示。真实员工登录后打开标准模型库、页级操作记录及模型广场，无浏览器 pageerror，运行列表为空。有数据的供应来源独立页、查看报告进入当前报告摘要、返回广场使用既有资源浏览器夹具验证，pageerror为空；夹具仅拦截浏览器请求，未写运行库，不代表B报告历史/资源接入真实联调。最后仅调整来源表列宽和间距，未重复构建。未提交或推送。

剩余原型差异：公共工作台仍为人员单选及四状态单选，尚未实现原型的人员/状态复选及默认待办+待阅；公共查询当前一次只支持一个人员和状态，需公共聚合契约配合，A未另造聚合服务。来源筛选尚缺原型的性能指标区间筛选；报告当前只展示已选报告摘要，历史/文件/补测仍缺B提供方；官方抓取区域不展示模拟成功状态，真实F2能力仍在后续阶段。模型广场与标准库仍需继续核对原型细节，不能据本轮记录宣称全部UI已一致。
## Stage 53：供应来源筛选、报告页面及计划到期提示（2026-10-22）

按 v0.22 继续补齐前端。供应来源新增供应商/状态多选，以及采购系数、连通耗时、RPS、TPS区间筛选；通过已有flat API完整读取当前模型及外层筛选范围，核对分页和资源归属后筛选再分页。无指标/旧目标不作为有效数值参筛，零值可筛；上下限非法显示原因。来源页增加操作记录页签，明确选择当前模型中的具体资源查询公共resource日志，不把模型ID冒充资源ID。

资源详情新增报告历史页签、分页历史表、详情指标卡、旧目标警示和文件下载入口，文件通过认证资源/报告/文件嵌套路径访问，按can_download显示能力；404/501/503展示暂不可用和重试，不伪造历史。报告详情/文件路径及DTO仍是A/B接入提案，见公共集成请求第20节，当前B路由仍未提供。补测入口按详细设计为办理指引及刷新，不直接启动测试，不自动改资源状态。

标准模型维护弹窗对齐厂商/类型、官方标准名称/ID和资料顺序；保存按钮改为“保存并留痕”，核验按钮提示阻断原因；保持资料可先保存、核验独立操作的已确认规则。弹窗append-to-body，支持从资源详情嵌套打开。计划列表和详情按服务器返回due_at显示“预计时间已到”，30秒更新显示；只作页面提示，不代表提醒worker或事件已接入，执行资格仍关闭。

验证：新增一个定向场景文件，两条场景检查区间零/缺失/旧指标与报告安全投影/归属，2/2通过；前端build通过（既有大chunk提示）。后续到期文案/报告状态展示调整后补跑typecheck通过。真实本地测试账号登录，在浏览器中用夹具检查101条来源跨两页加载后筛到第101条、报告503后重试、历史详情、旧目标警示、受限/允许文件下载、补测指引和来源操作记录页签，无pageerror；另一轮检查新增模型弹窗字段/取消及到期计划详情/执行按钮关闭，无业务保存。认证与空列表来自运行服务，资源/报告/计划数据来自浏览器夹具，不代表这些业务后端已联调。前端开发服务本轮发现停止后重新启动，API健康。未跑全量测试、未迁移、未写业务库、未提交或推送；公共main/router现有diff保持原样。

剩余：B报告历史/详情/文件真实提供方；正式配置密钥能力与writer；资源办理/报告事实和接入联调；公共计划执行提交/409幂等重放；公共工作台人员/状态多选聚合、具体计划ID投影及提醒worker。A前端页面已推进，不宣称三个模块完整交付。
## Stage 54：真实认证写入联调与 A 运行断点修复（2026-10-22）

从当前运行库复制临时v36联调库 `model_bss_a_smoke_20261022_54`，以现有API二进制和真实登录账号在18081启动临时API。浏览器仅把API请求转发到该真实服务，不使用HTTP响应夹具。源运行库只读；用于计划维护的已接入资源和公告只在隔离库准备，不用其证明B办理/测试/资源准入已经完成，不伪造报告或测试通过。

真实写入证据：页面新增待补齐标准模型，API整体补齐资料，再从页面人工核验、修改资料使核验失效；当前可报价状态随核验变化，4个不可变历史版本与4条日志一致，历史核验版保留。补齐阶段调用真实API，尚不等同所有补齐字段均逐个通过页面输入验收。公告打开详情仍UNREAD、本人标已阅写入成功，重复请求不改变首次时间；查看另一真实人员的状态仍UNREAD且不可代标。页面创建、修改、取消下架计划均200；修改不重设资源版本基准，整个维护流程资源状态/revision不变。另一真实账号取消原发起人计划返回403。此结果使用隔离库真实认证/服务/SQL，不代表运行库已有业务数据。

修复三个真实断点：

- A模型/资源/计划/阅读日志未填写 `operator_name_snapshot`，公共操作记录姓名为空。现在从同事务中的真实internal_staff取姓名快照；新模型操作及计划三次维护日志均显示真实姓名，旧记录不回填、不改写。补充既有日志集成用例的姓名断言。
- 真实资源价格项省略可选dimensions时，前端Object.entries抛异常，导致整个资源详情及其公告/计划无法进入。读取层将缺失/null维度正规化为空对象，非对象仍拒绝，保留价格合法性校验；新增一条定向回归。
- 当前装配的计划前置检查查询不存在的resource.formal_config_id/formal_config_revision列，创建计划500。改为从现有资源控制快照取绑定和版本，不加列、不执行迁移。该回调目前检查绑定正式配置是否过期，不替代B已批准但未应用变更的可信事实；B事实提供方仍待接入。

计划执行运行装配暂不注入PlanExecutor，直接HTTP请求同样返回503；不能只隐藏按钮却允许直接请求在未满足公共提交/409重放协议时修改状态。领域服务和HTTP实现保留，公共能力接齐后再装配。

验证从简：受影响resource-contract.test.ts 5/5通过，前端build通过（既有大chunk提示）；Go格式化和cmd/server编译通过。单独调用的现有日志Go用例在未设置专用PG环境时跳过，不能算作该集成用例通过，姓名修复的证据来自本轮真实API/SQL。浏览器完成上述主流程，无pageerror，遇到真实失败后仅重跑受影响链路。原main.go/router.go既有diff保持8/15行未变。

部署：保留原忽略路径 `bin/server-v36.before-stage54`，将修复后二进制替换当前容器挂载的server-v36并重启现有model_bss_api_latest。8080健康、认证模型/资源/公告/计划列表通过，执行端点缺依赖503；只读确认运行库 `36|f`，模型/资源/公告/计划均0条。临时API自动移除、临时联调库已删除；无新增常驻环境、无运行库业务数据、无迁移、无提交或推送。当前仍未完成B报告/文件/补测、公共配置加密writer、真实资源准入、B待应用变更事实、计划执行幂等协议和提醒worker接入。
## Stage 55：模型供应资源日志汇总与指定计划定位（2026-10-22）

按 v0.22 供应来源页的操作记录交互，新增 A 自有只读 GET `/internal/standard-models/:id/resource-operations`。在同一只读快照中，先按 standard_model_id 限定资源，再合并资源日志和通过 plan.resource_id 关联的计划日志、计数分页；按 created_at DESC/id DESC 稳定排序。模型不存在或指定资源不属于模型返回404；仅投影业务对象、操作、人员姓名快照、原因、时间和导航标识，不返回 before/after 或凭据。来源页默认汇总本模型全部资源，明确不受来源列表筛选影响；不再强制只看一条资源。模型资料、供应商级配置日志不混入此范围。契约已先写入 supply-resources.draft.md 第13节。

日志中计划记录可直接“查看计划”。A 资源深链新增 plan_id，并支持 reports/records 页签；指定计划不依赖列表当前页，详情必须核对 resource_id/region，不匹配显示错误且没有维护按钮。计划详情 append-to-body，避免嵌套抽屉遮挡。未猜测公共待办的计划ID：公共投影未携带 plan_id 时仍打开资源计划列表；实际执行能力继续关闭。

最小验证：新增 Go 领域校验一项通过；前端两项定向检查通过（安全投影/大整数计划ID、跨资源/区域上下文），前端 build 通过，仅保留既有大chunk提示。新增 PostgreSQL 定向检查使用 pg_temp 临时表并回滚，验证两个供应商来源的资源/计划日志汇总、同时间ID倒序分页、单资源过滤、跨模型拒绝，实际执行通过；没有向 public 业务表插入夹具。API及仓储包在定向命令中仅编译、没有匹配测试，不算全量回归。

本地更新现有 model_bss_api_latest 挂载二进制，保留忽略目录 bin/server-v36.before-stage55。真实认证下新端点未认证401、缺模型404、非法模型ID400。浏览器以真实本地登录、页面夹具检查“模型广场→供应来源操作记录→指定计划详情”，计划不在列表当前页仍可打开；跨资源计划拒绝展示操作，无pageerror。这些有数据的页面响应来自夹具，不代表B流程、真实资源办理或计划执行已联合验收。未新增常驻环境、未迁移、未提交或推送。

仍未完成：公共正式配置加密writer、B报告/文件及真实办理事实、资源绑定/逐资源准入的运行闭环、计划执行的公共BLOCKED提交与409重放、B待应用变更事实和公共到期提醒worker。A此次新增的日志查询与计划导航不替代这些依赖。

## Stage 56：标准模型完整页面写入闭环（2026-10-22）

在从主运行库复制的临时 v36 验收库 `model_bss_a_acceptance` 中，以真实 smoke_admin 登录和当前 API/页面运行标准模型全字段流程。页面逐项填写新增待补齐模型，保存后关闭并重新打开维护，核对名称、官方ID、资料披露项和价格项回显；页面人工核验后服务端标记可用于报价；再修改资料，核验状态失效、报价资格关闭且版本前进。真实 GET、历史版本和操作记录核对一致：3个版本，CREATE → VERIFY → UPDATE，操作人姓名完整；浏览器无 pageerror。按 v0.22 核对资料字段分组与新增/维护、核验、历史及留痕交互。

## Stage 57：模型级供应操作记录与指定计划真实联调（2026-10-22）

同一临时验收库中，使用隔离 QA 标记创建两条 A 导航资源记录及资源版本操作日志，无测试报告、无报告引用，API 明确返回 NOT_TESTED 且不可报价；这只用于验证 A 的导航与记录聚合，不代表 B 已办理准入。通过真实供应来源页面创建下架计划，确认真实 API/数据库落库及计划 CREATE 日志；模型级操作记录 GET 将计划记录与两条资源记录合并，页面显示正确业务对象、人员和原因。点击“查看计划”后准确打开资源1/CN的指定计划详情；将链接改为资源2/海外后，页面拒绝显示不属于该资源的计划，浏览器无 pageerror。

两项均使用真实本地认证、当前前端与临时 API 容器、PostgreSQL v36 隔离库，不使用页面/API响应夹具。清理前确认主运行库仍为 `36 / dirty=false`，标准模型、资源和计划均为0条；本轮未迁移、未提交或推送。上述结论只覆盖这两个 A 页面/API/数据库切片，不代表配置绑定、资源准入、B 报告、计划实际完成和公共到期提醒已联调。

## 2026-10-22 交接文档补充

新增[supply三模块协作交接清单](../supply/handoff.md#handoff)与[TDD交接验收清单](./handoff-acceptance.md)。按公共/B/C明确真实提供方、A接续实现、代码入口、参数和最小验收条件；将Stage54/56/57已通过范围与配置/准入/报告/执行/提醒等待项分开。同步修正入口、规划和核查清单中的运行库32、主路由未挂载、前端审批守卫未实现、运行PlanExecutor已注入等过时状态。

本次仅核对源码及现有阶段记录、补交接说明，不执行新业务测试、不增加“通过”结论，不修改功能代码/数据库/容器，也不提交或推送。交接后的真实结果继续追加本文件，不新增分散results或上传evidence。

### 同日职责核对更正

重新核读总览§〇.1/〇.2及F1–F17、A任务书、附01§2.2/3.2/6.1/10：项目是A/B/C三人；“公共统一维护”是共享文件协作，未指定独立第四负责人；C明确提供F14日志/绑定工具。已修正交接清单和当前规划：正式配置安全实现/接线、资源状态、F13执行/提醒由A继续推进，B交付F5–F7流程/测试事实，C完成客户/报价及F14。热点文件修改仍按统一维护规则。供应商门户按业务模块及A任务书范围说明；此前按前端/后端描述F8责任的表述已更正，不据文件目录或页面入口重新切分模块。仅更正文档，不改变业务规则、实现和验证结果。

<a id="stage-58-a-侧配置安全计划执行与提醒源码接线2026-10-22"></a>

## Stage 58：A 侧配置安全、计划执行与提醒源码接线（2026-10-22）

- 正式配置新增生产用 AES-256-GCM 密封器和 HTTPS 公网 URL 策略。运行装配读取 `MODEL_BSS_FORMAL_CONFIG_KEY_VERSION` 与 Base64 编码的 32 字节 `MODEL_BSS_FORMAL_CONFIG_KEY`；两项缺失时不装配写服务，配置写入保持503；格式错误时启动失败。没有默认密钥或明文回退。
- `cmd/server/supply_runtime.go` 已装配计划执行服务；计划读取只向原发起人开放待处理上下架计划的 `can_execute`，前端响应契约同步校验。BLOCKED 的409响应会在外层事务提交后保存状态码、业务码、消息和计划数据，同一幂等键重放原409，不重复执行；普通校验/授权4xx仍不保存为终态。
- 新增默认每分钟轮询的计划提醒worker。它按计划revision和状态核对到期的有效提醒事件，以 `source_event_id` 去重后写A公告；不改变资源状态。工作台收件箱仓储补充具体 `plan_id`，由已有资源/区域深链打开计划。
- 验证：前端全量 `npm test` 107/107 通过，`npm run build` 通过（Vite有既有大chunk提示），`git diff --check` 通过。当前执行环境没有Go命令，故本次新增/修改的Go测试、编译和格式化均未执行；Docker Engine管道返回Access denied，未运行或更新API容器，也未对运行库执行真实联调。以上是源码实现状态，不作为后端编译、v36联合验收或已部署的证据。
- 后续仍需：在部署环境注入正式配置密钥并重建/启动API；运行Go测试和v36隔离库联合验收；B提供真实报告/附件、办理节点资格及待应用配置事实后完成绑定、准入和报价链路验收。此阶段未提交或推送。

## Stage 59：Go 1.26 验证及本地 API 更新（2026-10-22）

使用临时 `golang:1.26-alpine` 容器挂载当前工作区，完成 `go test -count=1 ./cmd/server ./internal/worker ./internal/api/middleware ./internal/api -run "Test.*(Plan|Idempotency|Supply|FormalConfig|Route)"`；涉及执行器、提醒、幂等和路由的测试通过，`cmd/server` 编译通过。随后 `go build ./cmd/server` 成功，先将原 `bin/server-v36` 备份到忽略路径 `bin/server-v36.before-stage58`，再以新候选二进制更新原 `model_bss_api_latest` 容器挂载文件并重启。临时Go容器退出后自动删除。

部署后 `/healthz` 返回 `{"db":"up","status":"ok"}`；标准模型、资源、工作台收件箱未认证请求均为401。只读复查运行库为 `36 / dirty=false`，标准模型、资源、计划、公告均为0条；未迁移、未写业务数据。现有 API 容器没有配置 `MODEL_BSS_FORMAL_CONFIG_KEY_VERSION` / `MODEL_BSS_FORMAL_CONFIG_KEY`，因此正式配置写服务按设计保持不可用；本轮没有生成或注入密钥，也没有验证配置真实写入、计划执行成功/BLOCKED重放或到期提醒投递。执行和提醒虽已部署源码版本，仍需在可安全配置密钥、提供实际计划/B事实的隔离环境完成业务闭环。未提交或推送。

## Stage 60：本地正式配置密钥启用（2026-10-22）

按用户要求为本地 API 生成独立的32字节加密随机密钥，版本标识为 `local-v1`；不记录密钥值。将其写入 Git 忽略的 `model-pricing-backend/bin/formal-config-runtime.env`，ACL 限为当前 Windows 用户和 SYSTEM。重建 `model_bss_api_latest` 时复用原二进制、配置挂载、端口和数据库连接；原无密钥容器保留为停止状态的 `model_bss_api_before_formal_key_20261022`，可用于回退。密钥环境文件没有加入 Git，也没有复制到文档。

重建后 API 健康检查 `db=up/status=ok`，容器存在两个正式配置密钥环境变量。数据库仍为 `36 / dirty=false`，供应商和正式配置行均为0；没有迁移或业务写入。由于没有供应商主体，未能从页面/API执行正式配置创建，因此本阶段只证明密钥已安全配置并随服务启动，不证明密文落库或供应商调用流程已经验收。生产密钥管理和密钥轮换流程仍需另行配置/设计；本机密钥不得复制到其他开发者或生产环境。未提交或推送。

## Stage 61：工作台、模型库和模型广场原型交互对齐（2026-10-22）

- 工作台改为默认筛选本人“待办 + 待阅”，人员与四种状态按 v0.22 使用复选筛选，支持搜索和重置。公共收件箱仍保留单人员、单状态 API 契约；前端按选中组合并行读取、按时间合并分页，并在每行保留被查看人员 ID，使公告详情按本人权限读写。认证操作者不随筛选变化。操作记录调整为原型的“查看详情”入口，详情只展示公共 API 提供的安全字段。
- 模型广场将模型名称改为可点击入口，移除重复的“详情”列；继续进入已有的供应来源独立页，保留报告、资源详情、操作记录、计划和接入入口。标准模型库经复核符合原型的标准模型/操作记录页签、资料展示及单独维护入口。供应来源当前代码已包含性能指标区间筛选；Stage52 记录的这一项属于当时状态，不代表当前仍缺失。
- 本轮仅按代码和原型做定向调整，没有运行构建、测试或数据库操作；本地前端预览地址 `127.0.0.1:5173` 当前拒绝连接，未做浏览器截图核对。未提交或推送。

## Stage 62：最新前端构建及真实认证入口复查（2026-10-22）

`npm.cmd run build` 通过 Vue 类型检查和生产构建，仍有既有的大 chunk 提示。启动本地前端，使用真实员工账号登录当前8080 API；健康检查返回db=up/status=ok。工作台默认本人待办/待阅，实际展开真实人员列表、选择采购员A并取消本人、勾选已办、重置，再打开操作记录；标准模型列表及新增资料表单正常打开，取消未保存；模型广场真实列表正常读取。浏览器错误日志为空，没有使用响应夹具或写业务数据。当前列表均为空，故这只证明真实认证、入口、筛选操作及空列表读取，不证明多人员非空数据的汇总/分页/逐人权限，也未验证新模型名称链接的有数据导航。

当前尚未完成真实页面/API/数据库联合验收：正式配置新建/更新/停用及密文落库；配置绑定与逐资源登记接入、最终可报价状态；B报告历史/详情/文件及真实补测；计划执行成功、BLOCKED提交后的409重放；到期提醒投递到工作台及具体计划跳转；批准改价与后续报价消费。标准模型全字段写入闭环已由Stage56证明，公告已阅和计划维护已由Stage54证明，模型级日志及指定计划导航已由Stage57证明，无需据旧清单重新判为未联调。

复查当前运行装配，FormalConfigs和PlanExecutor已有接线（密钥启用见Stage60），ResourceAdmitter及ResourceConfigBinder尚未装配；后两者需要B的真实办理资格/报告事实接入。编译或单元测试通过不替代上述联合验收。未提交或推送。

## Stage 63：正式配置页面/API/v36隔离库真实写入闭环（2026-10-22）

从主运行库复制临时 v36 验收库 `model_bss_a_formal_config_qa_20261022`，启动独立 API `8081` 和 Vite 前端 `5174`，未改主运行库。使用真实员工认证进入模型广场，通过模型来源 → 资源详情 → 接入与配置完成页面操作。标准模型通过真实页面创建；供应商与一条 `PENDING_TEST`、无报告、无办理资格的资源仅作为隔离 QA 夹具，不代表供应商注册、测试通过或B侧办理事实。

正式配置页面/API/数据库完成 CREATE → UPDATE → DISABLE：创建后显示“有效/修订1/已设置”，修改资料且不重填 Key 后显示修订2，证明原凭据保留；停用后显示“已停用/修订3”。数据库核验记录为 `CREATE → UPDATE → DISABLE`，密文使用 `se1.` 封套格式，合成 QA Key 在密文列及操作日志中均不存在，API读回由前端安全契约接受且只显示“已设置”，浏览器错误日志为空。配置 URL 只被校验和存储，没有对外调用；未使用真实供应商凭据。

验证过程中，未给 QA 供应商绑定采购人时写服务拒绝；补入与当前真实登录员工一致的隔离 QA 采购归属后成功。保留 HTTPS 公网地址校验：不可解析的示例域名被拒绝，之后用公网 IP 格式的 QA 地址验证存储链路。以上不证明真实供应商登记与采购人初始化页面已联调。主库供应商及正式配置行仍为0，运行 API `8080` 与临时 API `8081` 健康。已清理临时 API、验收数据库及忽略目录临时配置；未迁移、未提交或推送。

本切片只通过正式配置保存/修改/停用及安全回显。配置绑定和逐资源接入仍因运行 `ResourceConfigBinder`/`ResourceAdmitter` 未装配、缺B真实测试报告与办理资格而未联调；资源保持不可报价。供应商注册/创建与初始采购归属也不在本切片验收范围。

## Stage 64：供应商主体创建与维护页面/API/v36隔离库联调（2026-10-22）

修复供应商主体无变化保存后的读取校验：`UPDATE_NOOP` 留痕时间可以晚于主体 `updated_at`，但不得改变主体资料修订号或更新时间。供应商主体新建、编辑及初始采购归属的契约、页面、API和 v36 原生存储适配已接通，使用真实登录员工 ID 作为初始归属，并在同一事务写 assignment 与操作记录。

在从运行库复制的临时 v36 验收库 `model_bss_a_supplier_qa_20261022` 中，通过页面真实认证完成新建、修改和相同资料保存；API 列表回读成功。数据库确认归属员工 ID 3，税率页面 6.25% 保存为比例 `0.06250000`；留痕为 `CREATE → UPDATE → UPDATE_NOOP`，末次操作后资料仍为修订 2，主体 `updated_at` 未变化。Go 定向领域/存储/API/服务装配用例和 `go build ./cmd/server` 通过，浏览器保存后无报错。该验证只覆盖 A 供应商主体资料和初始采购归属，不代表供应商首次接入、B办理事实或资源报价链路通过。

验证后移除临时 API、临时 v36 数据库、Vite QA 服务及忽略目录下的临时构建产物；运行数据库仍为 `36 / dirty=false` 且供应商业务数据未被本切片写入。未执行迁移，未修改主 API 容器，未提交或推送。

## Stage 65：当前工作区 API 部署及三模块入口复查（2026-10-22）

用 Docker Go 1.26 环境构建当前工作区 `cmd/server`，将原挂载二进制备份到忽略目录 `bin/server-v36.before-stage65` 后更新现有 `model_bss_api_latest` 并重启。API 健康检查返回 `db=up/status=ok`。使用真实内部账号从本地 5173 前端读取供应商主体列表、标准模型列表、模型广场列表和工作台；均进入新页面并通过真实认证/API返回空列表，工作台默认勾选本人待办与待阅。登录后已退出，不留浏览器会话。

只读核实主数据库为 `36 / dirty=false`，供应商行数为0；本阶段未执行迁移或业务写入，未改配置密钥，未提交或推送。该结果证明当前工作区API已部署且页面/认证/空列表读可用；主库没有可用于验证的资源、计划或公告，因此不证明资源绑定/准入、计划执行及409数据库重放、提醒投递已经联合验收。

## Stage 66：F12 v36 兼容只读投影（2026-10-22）

先新增 [F12 v36 只读契约](../supply/f12-records.draft.md#f12-legacy-read)，再实现供应商合同记录与授信历史查询：`GET /api/internal/contracts`、`GET /api/internal/credits`。读模型只投影 v36 原生列；合同不返回 `file_ref`/`note`，只显示附件引用是否存在。当前授信按主体、方向、币种和最高原生版本确定；同一最高版本有多条时标记 `AMBIGUOUS_VERSION`，金额非正时标记 `INVALID_LEGACY_AMOUNT`，都不猜测当前值。供应商主体页新增只读合同/授信面板，并明确显示旧表无法提供资料名、编号、附件下载或不可变版本历史。

Go F12 定向领域、存储、API 用例通过；前端 F12 契约用例 2 项通过，`npm run build` 通过（保留既有 chunk 体积提示）。`cmd/server` 使用与运行环境匹配的 Alpine Go 1.26 且 `CGO_ENABLED=0` 构建后部署到本地 `model_bss_api_latest`；运行库健康为 `db=up/status=ok`，两个新路由未认证请求均返回401。首次以 Debian Go 镜像构建的动态二进制与 Alpine 运行镜像不兼容，已回滚并改为静态 Alpine 构建；旧 API 二进制保存在忽略目录 `bin/server-v36.before-stage66`。

数据库仍为 v36，本阶段没有迁移或业务写入。主库供应商记录为0，因此没有真实非空合同/授信行可供页面/API数据联合验证；本阶段只确认了代码用例、构建、容器健康及路由鉴权。F12 完整写入、不可变版本、附件 D07 上传/下载、客户主体、首次接入流程上下文和 readiness 仍未完成；不得将此兼容投影用于办理完成判断。未提交或推送。

## Stage 67：工作台指定计划ID稳定投影（2026-10-22）

核对000035的`v_work_item`后发现，计划行只暴露资源ID、发起人和创建时间；旧仓储再按这三项反查plan ID。多条计划投影字段相同（如同资源、同发起人、同一事务时间）时，反查会让多条待办指向同一计划。

遵守共享迁移统一维护约定，不修改`migrations/**`。A仓储改为非计划事项继续读`v_work_item`，计划事项直接从原生`plan`及资源/模型/供应商表投影，携带原生`plan.id`；排序增加plan_id作为并列项稳定键。新增定向回归确保计划ID来自原生行且不按时间戳反查。Docker Go 1.26环境中的`internal/repo`定向用例和`cmd/server`编译通过；随后更新现有API二进制，健康检查通过。数据库仍为36，未迁移、未写业务数据，未提交或推送。

Stage67当时没有非空工作台计划数据，因此该阶段只修复了投影歧义并验证编译/回归。后续隔离非空数据验收见Stage70。办理节点flow/node定位仍需B提供稳定上下文。

## Stage 68：计划详情执行资格提示修正（2026-10-22）

计划执行按钮已按服务端`can_execute`开放给符合条件的原发起人，但详情的禁用提示仍称“登记入口尚未开放”。改为说明仅原发起人对可执行的上下架计划可登记，预计时间只作提醒。`npm.cmd run build`通过，保留既有Vite大chunk提示；未新增测试或业务数据。

## Stage 69：模型广场报告读取 API 适配（2026-10-22）

为已有的报告历史/详情/文件下载页面补上正式 A 路由及`ResourceReports`注入契约：列表`GET /api/internal/resources/:id/test-reports`、详情`GET .../:reportId`、文件`GET .../:reportId/files/:fileId`。请求把认证员工ID及资源/报告/文件ID交给可信提供方；列表和详情验证资源归属、版本ID、状态、分页、十进制指标及缓存计数；文件请求还要先核对报告内的可下载文件，随后由附件提供方再次执行授权。文件仅流式返回，文件名拒绝路径分隔符，内容类型白名单化，并设置attachment、`nosniff`和`private, no-store`。缺少提供方明确返回503，不回退到旧`report_file`路径，不伪造空历史或测试成功。

Go 1.26环境下`go test ./internal/api -run 'TestResourceReportHTTP' -count=1`通过，覆盖认证员工ID传递、跨资源响应拒绝、缺提供方503、下载元数据保护及不可下载文件不调用附件提供方；`CGO_ENABLED=0 go build ./cmd/server`通过。已保留原二进制到忽略路径`bin/server-v36.before-stage69`，更新并重启现有`model_bss_api_latest`；`/healthz`为`db=up/status=ok`，报告列表路径未认证请求返回401。当前运行组合尚未注入B报告/附件提供方，数据库也没有资源或报告数据，本阶段未验证非空历史/目标匹配/真实文件下载。数据库仍是36，未迁移、未写业务数据，未提交或推送。

## Stage 70：工作台计划跳转、到期提醒与实际完成隔离库闭环（2026-10-22）

基于主运行库创建临时 v36 数据库 `model_bss_a_plan_qa_20261022`，使用明显标记的 QA 供应商、标准模型和已接入资源；资源没有测试报告、正式配置或标准模型核验，因此页面明确显示“暂不可用于报价”。未伪造 B 测试/办理事实。本切片只验证离线下架计划，不走依赖 B 的在线配置计划。

通过真实本地认证和当前前端/API，模型广场创建下架计划并写入隔离数据库。计划到期 worker 写入 `ONLINE_OFFLINE_PLAN` 公告；工作台同时显示“下架计划到点提醒”和资源变更公告为当前用户待阅。点击工作台下架计划待办“详情”后，浏览器 URL 携带原生 `plan_id=1`、资源ID和区域准确打开计划详情。页面填写仅带 `QA ONLY` 标识的模拟依据并提交后，计划状态成为 DONE、资源状态成为 OFFLINE 且版本递增，资源变更公告落库；工作台将该计划显示为“已办”。UI页面、HTTP API和 PostgreSQL v36 隔离库结果相互吻合。

验收仅针对隔离库的 A 侧下架计划、worker提醒、个人待阅投影、具体计划深链和成功执行路径；模拟依据不代表真实外部平台操作。未覆盖并发前提冲突后的 BLOCKED/409幂等重放、跨人员权限、依赖 B 的在线计划、真实资源准入/报告/配置或报价可用性。本阶段不改变主运行库、不迁移、不提交或推送。结束后已删除临时API容器、隔离数据库和专用SQL/Vite文件，5174端口不再监听；主库复核为 `36 / dirty=false`，supplier/resource/plan/notice均0条，临时库不存在；主API仍在8080健康运行。

## Stage 71：BLOCKED 409 幂等重放与计划提醒修订/取消定向验证（2026-10-22）

补充 `TestPlanReminderPostgres36IgnoresRevisedAndCanceledSchedules`，在从主运行库克隆的专用 v36 PostgreSQL 验收库内创建仅带 QA 标记的两条供应商/资源计划。第一条计划到期后修订，worker只为当前 revision 的调度事件生成一条提醒，旧 revision 事件未被用于提醒；第二条计划取消后不产生提醒。重复运行 worker 不重复投递，资源状态仍为 `ADMITTED`。新增的是存储层真实数据库定向用例，没有改动提醒生产逻辑。

定向 Go 验证通过：`TestPlanExecutionHTTPReturnsBlockedOutcomeAs409WithPlanData`确认 BLOCKED 以409返回计划版本，并以相同幂等键重放原409响应且执行器只调用一次；`TestIdempotency_ReplaysCommittedTerminal409WithBusinessData`确认终态409连同业务数据被同键重放；新增的 PostgreSQL v36 提醒用例通过。前两项为 HTTP/幂等中间件测试夹具，不是运行 API 与真实数据库事务的端到端 HTTP 验收。

以上仅运行三项定向用例，未跑全套测试。主运行库未写入，未执行迁移，未提交或推送。隔离 Go 容器及验收数据库在验证后清理。

## Stage 72：模型广场详情列与供应商模型详情页（2026-10-23）

模型广场汇总列表增加独立“详情”操作，打开该模型的供应来源；供应来源行的“详情”现在进入该供应商对应的模型资料页，页头明确供应商、标准模型及接口模型ID，保留返回供应来源的路径。详情页复用现有资源详情、报告、正式配置/接入、计划和操作记录组件。资源详情读取增加供应商ID、标准模型ID交叉核对，避免显示错配资源。

前端生产构建 `npm.cmd run build` 通过（包含 `vue-tsc --noEmit`）；Vite输出保留既有大于500kB的chunk提示。未运行页面/API/数据库联合验收。运行装配仍未注入 `ResourceReports` 可信报告及附件提供方，真实报告历史和文件下载因此仍不算接通；本次只完成入口与页面复用，不将构建结果当作报告联调证据。

## Stage 73：跟进 main 模型详情页并接入报告历史入口（2026-10-23）

拉取并快进合入 `origin/main` 至 `f6d6cf0`。与本地 `ModelPlaza.vue` 有内容冲突，按要求保留 main 版的模型广场入口和新增 `ModelPlazaDetail.vue`，未用本地旧详情覆盖；其他 A 侧未提交改动已恢复。

在 main 新详情页中，将每条来源的“详情”接到资源详情抽屉并校验供应商/标准模型归属；“查看报告”打开报告历史页签，“资源操作”进入该资源可用的接入/配置或上下架计划页签；模型级操作记录页签接到 main 新增的 `ModelSourceOperationRecords`。报告读取器从 `test_report`/`test_job` 提供真实列表和详情投影，按已选中的资源证据快照判断目标版本；不向前端返回文件路径或 URL。附件目前只显示为存在但暂不可下载，等待受控文件存储/授权提供方。

`npm.cmd run build` 通过（`vue-tsc --noEmit` 与 Vite 构建；保留既有大 chunk 提示）。Go 编译及非空报告数据库/API联调未执行：Go CLI 不在 PATH，Docker Desktop Linux Engine 当时未启动。数据库迁移、API容器更新、提交和推送均未执行。自动暂存保留为 `codex-temp-before-main-sync-20261023`，用于冲突恢复；工作区原有其他 A 侧未提交更改仍在。

## Stage 74：F13 内部采购系数改价计划（2026-10-23）

新增[内部采购系数改价计划契约](../supply/supply-workbench.draft.md#price-plan)并落地首版。该计划独立于供应商改价审批：模型广场资源详情可创建计划，选择在职办理人、系数、预计时间和原因；计划支持查看、修改、取消、指定办理人登记实际完成。工作台将待办投影给指定办理人并沿用带具体 plan_id 的资源计划跳转。预计时间只提醒，不调用供应商生产接口。

授权基线：资源归属采购人创建，发起人修改/取消，指定办理人执行。执行时校验资源修订号和原系数；成功在一个事务内更新系数与资源修订号、写完成依据/操作日志、`RESOURCE_COEFFICIENT_CHANGED` 成本事件和 `COST_CHANGE` 公告，同时保留测试、配置和资源状态事实。冲突写入 `BLOCKED`，不更新资源或发布成本事件，并由 HTTP 返回携带计划数据的409。无需新增数据库迁移，复用原生计划快照、资源修订快照、日志及事件/outbox结构。

验证：前端 `npm.cmd test` 114项通过，`npm.cmd run build`（含 `vue-tsc --noEmit`）通过；Go 1.26 Docker 环境中 `internal/domain/supply`、`internal/infra/supplypg`、`internal/domain/workbench` 测试通过，`internal/api` 的 `TestPricePlanHTTP*` 定向用例通过。一次全 `internal/api` 包测试中，现存 `TestRouterSupplierPortalProfileNotGuardedByRequirePerm` 失败（源码保护测试的范围断言命中供应商区块之后的内部客户路由）；该项不属于改价计划。

当前未设置专用 PostgreSQL 测试库，因此未验证数据库中的改价成功事务、BLOCKED/409真实提交及重放，也未更新或重启 `model_bss_api_latest`。运行库没有因本切片执行迁移或业务写入。工作区未提交或推送。

## Stage 75：改价计划 v36 HTTP 事务验收与报价审批待办接入（2026-10-23）

工作台收件箱在共享 `v_work_item` 投影之外增加待审批报价投影：每条 `APPROVING` 报价分配给所有在职且 `can_approve_supplier=true` 的员工；同一报价原先分配给采购人的通用 `QUOTE` 待办在审批中隐藏，避免把审批误交给供应商归属采购人。工作台详情深链进入既有 `/internal/supplier-quotes?quote_id=...`；报价页读取该 ID 后打开原有差异抽屉并沿用现有审批资格、行级范围和 approve/reject API。没有恢复独立审批中心，也没有修改报价审批事务。

为内部采购系数改价计划新增真实 HTTP/PostgreSQL v36 集成验收：通过 A 路由和真实幂等中间件创建计划并登记完成；成功事务核对资源系数/修订、计划 DONE、操作记录、`RESOURCE_COEFFICIENT_CHANGED` 事件和 COST_CHANGE 公告。资源版本并发变化时核对 BLOCKED 与409均已提交，同一幂等键重放同一业务响应且不改写资源。无需数据库迁移。

验证：Docker Go 1.26 下收件箱仓储/深链/改价路由定向单测通过；隔离 v36 PostgreSQL 上 `TestWorkbenchInboxHTTPProjectsQuoteApprovalsOnlyToEligibleStaffPostgres36` 与 `TestPriceChangePlanHTTPPostgres36CommitsExecutionAndBlockedReplay` 通过。`npm.cmd run build`（含 `vue-tsc --noEmit`）通过，保留既有大 chunk 提示；当前 API 二进制构建成功并更新现有 `model_bss_api_latest`，健康检查为 `db=up/status=ok`，未认证的工作台 API 返回401。

验收库从主库只读克隆，测试夹具已清空后删除；临时低权限数据库角色也已删除。主运行库只读复核仍为 v36，供应商/资源/计划/报价记录均为0；未执行迁移或业务写入。保留旧 API 二进制回滚副本 `model-pricing-backend/bin/server-v36.before-stage75`（Git 忽略目录）。未提交或推送，未运行完整测试套件。

## Stage 76：标准模型单一保存与原子人工核验（2026-10-23）

按最新“手动补全”原型将维护弹窗收敛为单一“保存并留痕”按钮。新增/更新统一走 `manual-completion` API：不完整资料可保存为待补齐且不可报价；完整资料需要当前维护人、官方来源策略、核对日期、完整性及 revision 均通过，后端在同一事务内保存当前状态、一个新版本快照和操作记录，并写入核验人。资料变化只递增一个 revision/version；保存不再先写更新再单独调用 verify。旧创建/更新/核验路由仍保留兼容。

定向验证通过：Go 1.26 Docker 下 `go test ./internal/domain/supply -run 'TestModelServiceManualSave'`、`go test ./internal/api -run 'TestSupplyHTTPManualCompletionUsesOneAtomicSaveContract'`；前端标准模型 API scenario 2项通过；`npm.cmd run typecheck` 与 `npm.cmd run build` 通过，构建保留既有大 chunk 提示。浏览器交互脚本未执行，因为当前项目环境未安装 `playwright` 模块。

未进行 PostgreSQL 运行库写入、迁移或 API 容器更新；本阶段未提交或推送。原型差异记录已同步到需求规划、决策记录和标准模型 API 契约。

## Stage 77：供应商申报测试 Key 一次录入与复用（2026-10-23）

按本轮决策不为供应商测试 Key 新增 BSS 加解密。供应商申报页面保存成功后清空 Key 输入框，只显示是否已有配置；编辑已保存草稿时，Key 留空表示沿用，填入新 Key 表示替换。发起测试的浏览器请求只提交测试类别，不重复发送 URL/Key；供应商自测的 URL、Key、额度与内部平台测试所用 URL、Key、额度分开说明。API DTO 增加 `test_api_key_configured` 状态字段，前端错误响应脱敏覆盖 `test_api_key` 等键名。幂等缓存仍只保留 payload 指纹和幂等键，不保存请求正文。

TDD 红灯证据：旧页面在保存成功后仍将明文留在密码框，Playwright 用例按此行为失败；脱敏单测因 `test_api_key` 未匹配敏感字段而失败。修正后定向验证通过：`node tests/supplier-test-key.browser.mjs` 覆盖已有草稿首次录入 Key、成功后清空输入、空值更新不带 Key、测试触发只传类别以及 sessionStorage 不留明文；`node --experimental-strip-types --test tests/supplier-test-key.test.ts` 通过；`npm.cmd run typecheck` 通过。没有运行完整前端测试套件或构建。

此验证使用浏览器和模拟 API 响应，证明 A 页面行为及请求契约，不证明后端持久化、数据库明文策略、Inspector 关联或真实测试调度。当前供应商申报/测试后端仍由 B 接入，测试路由为空；后续需由 B 的真实接口返回 Key 配置状态、空 Key 更新保留已保存值，并使用供应商自己的凭据和额度调用 Inspector。内部测试继续使用平台凭据。正式运行配置现有 AES-GCM 加密本切片未改，数据库未迁移、未写入，API 容器未更新，未提交或推送。

## Stage 78：正式配置 Key 移除 AES-GCM 加密（2026-10-23）

按用户明确的后续决定，移除 F4 `supplier_formal_config` 正式配置 API Key 的 AES-GCM 实现。领域写服务不再依赖 `SecretSealer`；运行装配不再读取 `MODEL_BSS_FORMAL_CONFIG_KEY_VERSION` / `MODEL_BSS_FORMAL_CONFIG_KEY`，创建、更新正式配置不再因为缺密钥而不可用。持久层直接保存输入 Key 到共享 000033 物理列 `api_key_enc`，字段改名留待共享迁移流程。HTTP 回执、页面和操作日志仍使用安全投影，Key 原文不返回、不写日志；留空更新保留原 Key，旧 `se1` 封套不自动解密，要求重新录入后使用。供应商申报自测 Key 与正式配置 Key 仍属于不同链路。

TDD 定向验证通过，命令：`go test ./internal/domain/supply ./internal/infra/supplypg ./internal/infra/supplysecurity ./internal/api ./cmd/server ./cmd/seedchain -run "TestFormal|TestNewSupplyRouteModule|TestSupplyFormalConfig" -count=1`，在 Docker Desktop 的 Go 1.26 容器执行。覆盖无加密器创建、明文保存/安全投影、留空保留/Key轮换、旧封套重录、存储字符上限、日志指纹字段兼容、HTTPS URL策略、正式配置HTTP契约和运行入口编译。专用 PostgreSQL 集成用例因本轮未提供隔离测试数据库而跳过；没有访问或写入主运行库。

本轮未修改共享迁移，未部署或重启 API 镜像，未改写历史提交，也未提交/推送。Docker 中 `model_bss_api_latest` 反复重启的原因经容器日志确认是 `model_bss_pg` 已停止，API 连接 `host.docker.internal:5433` 被拒并以退出码1结束；与 AES Key 配置无关。Stage 77 中“本轮正式配置 AES-GCM 未改”的说法描述的是当时切片，现由本阶段决策和代码状态覆盖。

## Stage 79：正式配置 Key 前后端与 v36 数据库联调（2026-10-23）

使用 Docker Go 1.26 编译当前工作区 API，在临时 v36 数据库副本和临时 API 容器上验证；主库、现有 `model_bss_api_latest` 与其二进制未改。Playwright 经真实员工登录页以 `buyer_a` 登录，随后挂载正式配置真实 Vue 组件（临时联调资源夹具）执行新建和 Key 轮换。新建 POST 返回200、版本1；轮换 PUT 返回200、版本3；随后 GET 返回单条、版本3。写入和列表响应均不含 `api_key`/`api_key_enc` 字段及 Key 明文，只返回掩码和已配置状态。数据库确认共享 `api_key_enc` 物理列等于本次虚构 Key 原文且不是旧 `se1` 封套格式；三个版本的 `operation_log` 均未含原文。临时 API 未设置 AES 密钥环境变量，仍启动并通过 `db=up` 健康检查。

跨端口直连测试另发现本机浏览器的 PUT 被 CORS 拦截：OPTIONS 返回204，但公共 CORS 中间件未返回 `Access-Control-Allow-Methods`。通过 Vite `/api` 同源代理后，Playwright 页面新建、轮换和掩码回读全部通过；本机 `.env.local` 目前配置为 `http://127.0.0.1:8080/api` 直连，若继续使用该方式，更新/停用请求仍需补 CORS 方法配置或改走同源代理。该 CORS 问题不属于 Key 存储逻辑，本阶段未改公共中间件。

联调页使用真实配置组件但以临时资源夹具承载；没有覆盖模型广场完整资源抽屉导航，也没有调用真实供应商端点。验证后移除临时数据库、API 容器、Vite 进程、临时页面/配置和编译二进制。运行库仍为36；未迁移、未写主库、未部署 `model_bss_api_latest`、未提交或推送。

## Stage 80：供应商模型详情导航与准入时间展示（2026-10-23）

模型广场供应来源行的“详情”进入供应商模型页，带上供应商、资源、标准模型和区域上下文；该页列出同一供应商的模型，并打开所选资源详情。上下文不匹配时拒绝展示。列表准入时间改为读取真实准入操作记录：从 `operation_log` 中最早的资源 `RESOURCE_REVISION` 且 `after.operation=ADMIT` 记录取值；找不到记录时显示“未记录”，不再用资源创建时间冒充准入时间。

定向浏览器场景 `node tests/model-plaza-supplier-detail.browser.mjs` 通过，使用模拟 API 响应验证详情导航、供应商模型列表、所选资源展示、跨供应商隔离及缺失准入时间的展示。Docker Go 1.26 下 `TestResourceProjectionEqualityIgnoresReadOnlyAdmissionTimestamp` 和 `TestResourceReadPostgres34Native` 通过；数据库检查使用从当前 v36 主库结构生成的临时 schema-only 数据库和测试事务夹具，验证准入时间取自操作记录、缺记录保持空值及资源读取投影。`npm.cmd run build` 通过（保留既有大 chunk 提示）。这些结果不是完整的页面/API/真实业务数据库联合验收。

本阶段未迁移或写入主运行库，未更新 `model_bss_api_latest`，未运行全套测试，也未提交或推送。真实报告历史目前仍未做非空专项 API/数据库验收；报告列表元数据适配仍在开发，v36 没有可信报告来源字段，来源未知时必须显示“未记录”，不能按推断填充。

## Stage 81：报告历史元数据读取与展示（2026-10-23）

报告列表和详情补齐原型展示字段：测试日期、测试项、操作人、来源、场景。读取器从 `test_report.tested_at/scene/tested_by`、`test_job.category` 及内部员工姓名投影这些字段；v36没有可信报告来源字段，因此 `source` 保持空值，页面展示“未记录”，不猜测供应商或内部来源。列表以测试日期排序，详情展示同一组元数据；API校验时间、测试项和来源枚举，并继续做凭据/原始载荷安全投影。

定向验证通过：前端 `supply-front-completion.scenario.ts` 2项通过（报告元数据安全投影、非法来源和时间拒绝）；`npm.cmd run build` 通过，保留既有大 chunk 提示。Docker Go 1.26 下 `TestResourceProjectionEqualityIgnoresReadOnlyAdmissionTimestamp`、`TestResourceReadPostgres34Native`、`go test ./internal/domain/supply -count=1` 和 `go test ./internal/api -run "TestResourceReportHTTP" -count=1` 通过；`CGO_ENABLED=0 go build ./cmd/server` 成功。仓储用例在临时 schema-only 数据库插入合成报告与操作记录，验证报告日期/测试项/操作人/场景读取及来源不被虚构；API测试用隔离的 HTTP reader fixture。

本阶段结束后已删除临时数据库 `model_bss_a_schema34_admitted_20261023` 和临时 Go 缓存卷。主库仍为 `36|dirty=false`，未执行迁移或业务写入；现有 `model_bss_api_latest` 未更新，`/healthz` 返回 `db=up/status=ok`。未做真实登录浏览器→API→含业务报告数据数据库的全链路验收，附件下载仍缺受控存储/授权提供方；未运行全套测试，未提交或推送。

## 运行状态与 Key 联调范围复核（2026-10-23，非新增测试）

Stage81 结束时的 `/healthz` 是当时快照，不能代表当前容器状态。用户随后报告 `model_bss_api_latest` 反复重启；本次复核运行 `docker ps` 时 Docker Engine 返回 `Access is denied`，因此没有重新确认容器状态、日志或重启原因。Stage78 曾在当时观察到 API 因 PostgreSQL 容器停止而连接失败；不能把那次原因直接套用到当前重启。

DEC-018 正式配置 Key 改动已做前后端联调：Stage79 使用真实员工登录、正式配置 Vue 组件、临时 API 和隔离 v36 PostgreSQL 副本，验证创建、轮换、掩码回读、明文落库及日志脱敏。它没有更新 `model_bss_api_latest`，也没有经过主运行库；因此“Key改动在隔离环境联调通过”和“当前主Docker容器正在运行该改动”是两件事。

## Stage 82：内部改价计划支持修改接口模型ID（2026-10-24）

按新版原型，将供应商接口模型ID作为改价计划的目标值。计划保存修改前/后的接口模型ID和采购系数；允许只改ID、只改系数或两者都改，只有两项均未变化时拒绝。创建和修改不会直接变更资源，指定办理人登记完成时才在执行提案中更新资源接口模型ID/系数并递增资源revision；接口模型ID变化时递增target_version，保留旧报告记录但将旧目标报告标为过期。执行事件与公告记录对应前后ID/系数和目标版本。旧v36计划快照可按历史资源状态恢复目标快照，不新增迁移。

TDD定向验证通过：前端 `node --experimental-strip-types --test tests/price-change-plan.test.ts tests/plan-permissions.test.ts tests/plan-maintenance.test.ts` 共9项通过，覆盖ID-only创建/修改、无变化拒绝、权限、请求回执及计划摘要；`npm.cmd run typecheck` 通过。Go 1.26容器下 `go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg -run 'Test(InternalPriceChangePlan|PriceChangePlan|PricePlanHTTP|PlanMaintenance|PlanExecutionPostgres|PlanMaintenancePostgres|CanonicalStatePlan)'` 通过，覆盖领域执行与旧报告目标失效、HTTP契约、计划存储兼容和相关包编译。

尚未完成隔离数据库联合验收。`TestPriceChangePlanHTTPPostgres36CommitsExecutionAndBlockedReplay` 在本轮以 `-v` 显式执行，因未配置 `SUPPLY_SCHEMA_TEST_DB` 而跳过；它不能算作新ID语义的PostgreSQL通过证据。四页签真实数据浏览器验收、API容器构建/更新也未执行；本阶段未连接或写入主运行库、未迁移、未提交或推送。下一步使用专用v36隔离库运行成功执行（核对资源ID/target_version/旧报告失效）、冲突BLOCKED/409和同键重放，再做页面数据冒烟。

## Stage 83：三模块快速交付收尾与改价目标ID v36 联调（2026-10-24）

按快速交付计划完成新增改价语义和 A 三入口的轻量联合冒烟。基于主库 v36 schema-only 创建隔离数据库，运行 `TestPriceChangePlanHTTPPostgres36CommitsExecutionAndBlockedReplay`，通过计划创建、执行时更新资源接口模型ID/采购系数、资源 revision 与 target_version 递增、旧目标报告失效、冲突保存 BLOCKED，以及相同幂等键重放 409。另运行 `TestWorkbenchInboxHTTPProjectsQuoteApprovalsOnlyToEligibleStaffPostgres36`，通过有审批资格员工与普通员工的工作台审批事项投影差异。验收数据及临时数据库均已清理；主库未写业务数据、未执行迁移。

定向前端验证通过：`node tests/standard-model.browser.mjs`、`node tests/model-plaza-supplier-detail.browser.mjs`（供应商模型详情四页签及资源范围隔离）、`node tests/workbench-a.browser.mjs`；改价计划及计划权限/维护测试 9 项通过；`npm.cmd run typecheck` 和 `npm.cmd run build` 通过。另用 Playwright Edge 通道真实登录本机 `smoke_admin` 测试账号，工作台、标准模型库、模型广场页面均完成加载，API 调用成功，退出登录成功且无页面 JavaScript 错误。主库相关业务表为空，因此这次真实 UI/API 冒烟验证的是空数据呈现；四页签的非空业务内容由前端 API fixture 场景覆盖，不宣称真实供应商/报告数据全链路已通过。

用 Go 1.26 Docker 环境构建工作区 API 并更新本机 `model_bss_api_latest`；当前容器 `running`、RestartCount=0，`/healthz` 返回 `db=up/status=ok`。`model_bss_pg` 正常运行；只读核对迁移为 `36|false`，标准模型、供应商和资源主表记录数均为 0。Vite 本机 `127.0.0.1:5173` 返回 200。此前观察到的 27 次 API 重启发生在 PostgreSQL 不可连接时；本次 Docker 手动重启更新后计数为 0，不能据此保证未来数据库不可用时不会再次重启。

本阶段未运行全套测试，未迁移或写主运行库，未提交、推送或改写 Git 历史。剩余跨模块验收包括 B 提供的真实报告/办理授权/附件下载事实、资源配置绑定与逐资源准入、供应商批准改价后续办理；报告来源在 v36 无可信字段时继续显示“未记录”。

## Stage 84：模型广场详情往返保留筛选上下文（2026-10-24）

模型广场打开模型详情、进入供应商详情后再返回时，保留原模型列表的筛选、分页和排序状态；模型详情至供应商详情的往返也保留来源筛选，并恢复原选中供应商上下文。状态通过路由查询参数传递，仅接受页面使用的筛选字段。供应商详情返回模型详情后，资源范围仍按供应商/模型/区域校验。

定向浏览器脚本 `node tests/model-plaza-supplier-detail.browser.mjs` 通过。脚本挂载实际 `ModelPlaza.vue`、`ModelPlazaDetail.vue` 和供应商详情组件，以 fixture API 验证详情往返、列表及来源筛选恢复、四个供应商页签和资源范围隔离；缺失准入时间仍展示“未记录”。`npm.cmd run build`（含 `vue-tsc --noEmit`）通过，Vite保留既有大chunk提示。

随后重跑 `node tests/standard-model.browser.mjs` 与 `node tests/workbench-a.browser.mjs` 均通过：标准模型场景覆盖单一保存、冲突恢复、历史/日志、非维护人只读及不完整草稿；工作台场景覆盖本人已阅、他人只读、计划版本提示和资源范围。两脚本均挂载实际 Vue 组件并使用 API fixture，不作为真实数据库写入证据。只读探测 `127.0.0.1:5173` 返回200，`/healthz` 返回 `db=up/status=ok`；Docker CLI 在本会话返回 `Access is denied`，因此没有重新核对容器 RestartCount，运行容器状态仍以Stage83记录的快照为准。

本阶段只改前端导航状态与浏览器脚本，没有新增真实业务数据、调用非空后端数据或更新API容器；因此不替代真实供应商/报告页面联调。未运行全套测试、未迁移或写主库、未提交或推送。

## Stage 85：供应商正式详情入口与空主体联调（2026-10-24）

供应商管理列表的“详情”统一进入四页签主体页面；空资源供应商仍可查看档案、操作记录与报价空态，返回时恢复原列表筛选。模型广场来源详情携带白名单上下文，回退后恢复筛选/分页，并继续校验供应商、模型与区域范围。待审批报价接口对无记录的 HTTP 404 显示空态。F12 当前额度读取修正重复 SQL 参数绑定，避免 `/credits` 在列表页签返回 500。

`node tests/supplier-management.browser.mjs`、`node tests/model-plaza-supplier-detail.browser.mjs` 及 `npm.cmd run typecheck` 通过。前者经 fixture 验证列表到四页签、空资源和筛选恢复；另以真实员工登录、临时 API 端口 8081 和 v36 数据库验证列表、主体档案、合同/授信空态、资源空态、操作记录/报价空态及返回导航。临时供应商和两条日志已在保护条件下删除，临时 API 已移除。模型广场关联详情的非空资源实际 API 链路仍待非空业务数据验收。

未改动或更新原 `model_bss_api_latest`，未执行迁移；真实 API 测试运行库版本为 `36|dirty=false`。未提交或推送。

## Stage 86：供应商税率允许未填写并保留零值区别（2026-10-24）

F4 税率契约改为 `null | 比例字符串`：空值表示未填写，字符串 `"0.00000000"` 表示零税率。领域、API DTO、前端请求/响应和 PostgreSQL 映射已适配 nullable `supplier.tax_rate`，无需迁移；新建页面税率默认留空，供应商列表及详情不再将 null 显示为 0%。已有采购归属、revision、操作留痕规则保持。

RED 已观察：前端空税率 builder 和 null 回读断言因必填验证失败；领域空值用例因 decimal 解析失败；Playwright 发现 null 曾显示为 `0.00%`。GREEN 后，前端 `node --experimental-strip-types --test tests/supplier-entities.test.ts` 5 项通过；Go 1.26 Docker 定向 `go test ./internal/domain/supply ./internal/infra/supplypg ./internal/api -run 'Test(Supplier|SupplierTaxRate|F12LatestCreditQuery)' -count=1` 通过；`npm.cmd run typecheck` 与 `node tests/supplier-management.browser.mjs` 通过。浏览器 fixture 覆盖税率未填写、零值分离和保存请求的 null。

另使用真实员工登录和临时 API 容器，在本机 v36 数据库经页面新建 `tax_rate=null` 供应商，刷新列表并打开主体详情后均显示“未填写”。QA 供应商、归属记录及操作日志已精确清理；临时 API 容器和二进制已删除，原 `model_bss_api_latest` 与 `model_bss_pg` 未修改。未验证真实库把已有税率清空、已保存零税率回读；别名/曾用名、共用主体表单与失败后输入保留尚未实现。未运行全套测试、未迁移、未提交或推送。

## Stage 87：供应商档案共用表单与真实编辑闭环（2026-10-24）

新增 `SupplierProfileForm.vue` 并用于主体列表新建/维护及四页签详情的档案维护。详情仅在 `can_edit` 为真时显示维护入口；保存调用既有 PUT、携带 expected_revision 和操作说明，成功后重新读取档案、合同和授信。税率允许在详情页从未填写设为0%，也可清空回 NULL。

TDD 首先添加 Playwright 断言，确认详情没有“维护档案”入口时失败；补入入口和表单后 `node tests/supplier-management.browser.mjs` fixture 通过，并覆盖税率 null→0→null 和请求体。首次真实 v36 联调暴露后端快照比较把 `*string` 按地址比较，导致保存后 GET 和清空操作返回500；新增 `TestSupplierSnapshotComparisonUsesTaxValueNotPointerIdentity` 后先确认 RED，再改为深值比较。Go 1.26 Docker 定向 `go test ./internal/domain/supply ./internal/infra/supplypg ./internal/api -run 'Test(Supplier|SupplierTaxRate|SupplierSnapshotComparison|F12LatestCreditQuery)' -count=1`、前端 `supplier-entities.test.ts` 5项、`npm.cmd run typecheck` 通过。

以真实员工登录、临时 API 8083 和 v36 数据库重新跑 Playwright：页面新建 `tax_rate=null` 供应商，更新为0%，再清空；HTTP 两次更新均返回200，清空后的列表/详情仍显示“未填写”。数据库核对主行 NULL 与 CREATE/UPDATE/UPDATE 快照 `NULL → 0.00000000 → NULL` 一致。QA 主体及归属/操作记录在无资源、申报、合同、正式配置和资质引用的检查后已事务清理；临时 API 与二进制已移除。

真实运行库仍为 `36|dirty=false`；未更新原 `model_bss_api_latest`，所以当前8080容器尚未包含此修改。曾用名/别名、revision冲突后保留输入、非归属用户只读场景尚未验收；非空模型广场来源详情也未做真实数据联调。未运行全套测试、未迁移、未提交或推送。

## Stage 88：供应商改名保留曾用名并支持检索（2026-10-24）

主体改名时，旧名称写入新增的 `supplier_alias` 独立表；曾用名随主体更新、revision 与 `operation_log` 快照同事务保存。别名只在单一供应商内去重，不设置跨供应商唯一约束；两种供应商列表都按别名检索，并以 `EXISTS` 匹配避免同一主体重复。当前名称不返回为自己的别名，改回旧名时从当前别名集合移除。新增增量迁移 `000038_supplier_alias`，未修改任何历史迁移。

TDD 红灯已观察：领域改名用例因缺少 `Aliases` 行为无法编译；Playwright fixture 搜索曾用名返回 0 行。实现后，Docker Go 1.26 下 `go test ./internal/domain/supply ./internal/infra/supplypg ./internal/api -run 'TestSupplier|TestF12LatestCreditQuery' -count=1` 通过；前端 `node --experimental-strip-types --test tests/supplier-entities.test.ts` 6项通过，`npm.cmd run typecheck`、`npm.cmd run build` 和 `node tests/supplier-management.browser.mjs` fixture 通过。生产构建保留既有大 chunk 提示。

真实 Playwright 使用登录页、临时 API `8083` 及 PostgreSQL 隔离副本：先以页面新建 QA 供应商，再维护改名；供应商主体列表与正式供应商工作台分别用旧名检索，均仅返回一行；随后进入供应商详情并完成已有档案读写。数据库只读核对确认当前主体名、独立旧名行及 UPDATE 操作快照中的 `aliases` 一致。隔离库从主库 `36|dirty=false` 克隆后仅应用 000037/000038；测试结束已删除隔离库、临时 API、备份、配置和二进制。主库仍为 `36|dirty=false`、供应商行数0；原 `model_bss_api_latest` 仍运行且健康，本轮没有升级它或迁移主库。

S02 尚未验收旧 revision 冲突后保留输入及非归属采购人的只读入口。未运行全套测试、未部署到 8080、未提交或推送。下一步继续 S02 这两项快速验收，再进入 S03 供应商列表可信摘要；不根据资源数推断“已有”状态。

## Stage 88 后续：恢复 schema 交付归属（2026-10-24）

按用户要求撤回 A 在本地新建且尚未跟踪的 `000038_supplier_alias.up.sql/.down.sql`，恢复“A 提 schema 变更请求，统一维护人编写迁移”的原约定。结构需求、兼容要求、回退注意和验收场景移交至 `supply/supplier-alias-schema-request.md`；计划、F4 契约与交接入口同步更新，不预占正式迁移编号。Stage88 中 000038 的执行描述保留为此前候选结构隔离验证历史，不能作为当前正式迁移已交付的证据。

业务代码和已有测试保留；供应商主体 CRUD、曾用名检索和改名写入仍直接查询 supplier_alias，正式结构交付并对齐映射前不能开放这些入口。本阶段另为供应链工作台列表增加缺表降级：只在结构存在时投影别名，缺表时返回空别名数组，不影响主体摘要列表。本次只调整未跟踪迁移文件和文档，未执行数据库迁移或回退、未改动运行容器、未提交或推送；无需重跑业务测试。

前一轮补充的 Playwright fixture 已覆盖非归属主体维护按钮禁用、HTTP 409 后保留名称输入并通过；未覆盖真实跨人员请求、冲突刷新重试，不提升为完整权限/冲突联合验收。

## Stage 89：供应商档案冲突后显式刷新并重试（2026-10-24）

供应商主体列表和详情共用的档案表单增加 revision 冲突恢复：写入返回 409 时保留当前草稿，并提示先显式载入服务器最新资料。载入成功后以新版本和最新资料重置表单，避免把旧表单直接套用到新 revision 而覆盖并发修改；采购归属已变化时关闭维护表单。列表与详情均调用真实档案 GET 后再开放重试。

按 TDD 先扩展 Playwright 场景；首次运行因缺少“载入最新资料”入口失败。补齐表单、列表页与详情页事件后，`node tests/supplier-management.browser.mjs` fixture 场景通过，`npm.cmd run typecheck` 通过。真实联调用两个 Playwright 登录会话：第二会话先更新联系人，第一会话用旧 revision 保存得到 HTTP 409，输入保留；显式刷新显示并发联系人，再以 `expected_revision=2` 保存成功，随后改名、曾用名检索也通过。当前业务 API 由 Docker Go 1.26 从工作区源码构建，接到从主库复制的 v36 隔离数据库；隔离库仅额外应用 schema 申请中的候选 `supplier_alias` 结构，以运行当前别名代码。只读核对确认最终主体名称、并发联系人和别名均已持久化。

测试结束删除隔离 API 容器、隔离数据库、临时 dump、配置和二进制。主库仍为 `36|dirty=false`、供应商记录数0；`model_bss_api_latest` 未替换，未执行正式迁移。真实跨人员 API 403 仍未验收（只有 fixture 只读入口和既有领域归属用例），曾用名正式结构仍待统一维护人交付；未运行全套测试、未提交或推送。

## Stage 90：供应商主体列表状态与授信摘要纠偏（2026-10-24）

供应商列表不再用正式 API 配置存在与否冒充合同状态、不再按资源数量推断合作阶段，也不把 `supplier.credit_amount` 当成 F12 当前授信。合同状态依据启用的 CONTRACT 办理节点和原生 `contract.status`；合作阶段直接展示 `supplier.established`；未完成事项聚合进行中申报、onboarding flow 与待执行或阻塞的内部改价计划。资源数继续按供应商关联资源计数。当前授信仅从 `credit_line` 的供应商方向、主体币种和最高 version 读取；最高 version 多条标为 `AMBIGUOUS_VERSION`，非正金额标为 `INVALID_LEGACY_AMOUNT`，未记录时不回退旧字段。工作台列表还兼容缺少尚待统一维护人交付的 `supplier_alias` 结构，返回空别名数组；主体 CRUD 的别名能力仍受结构依赖限制。

前端新增授信状态说明和“潜在/已有”筛选，并以独立筛选验证“已有且有未完成事项”可同时出现。新增 Go 查询契约测试 `TestSupplierWorkspaceListQueryUsesV36BusinessFacts`、`TestSupplierWorkspaceListQueryIncludesFormerNamesOnlyWhenStructureExists`、可选只读测试 `TestSupplierWorkspaceListExecutesReadOnlyAgainstV36`，以及非空隔离库测试 `TestSupplierWorkspaceProjectionWithIsolatedV36Data`。Go 1.26 Docker 定向测试通过；非空测试在 clean v36 克隆库中调用真实 `ListSuppliers` SQL，覆盖潜在空主体、已有主体资源/归档合同/当前授信/进行中办理/BLOCKED改价、歧义授信版本及待处理申报。该测试有数据库名前缀和版本保护，QA记录已清理，临时克隆库及转储已删除；正式 v36 库只读复核为 `36|false`，主体和标准模型计数为0。

`node tests/supplier-management.browser.mjs` 通过，覆盖曾用名命中、合作阶段与办理状态独立筛选及 F12 金额显示；`npm.cmd run typecheck` 通过。浏览器仍使用 fixture 响应，最新 Lovable 状态口径未在本切片重新确认；因此这证明了真实数据库聚合查询，不代表浏览器、HTTP API 与数据库的完整联合验收。服务端筛选/排序分页及多币种列表额度口径也仍待收敛。

本阶段未添加迁移、未向主库写入测试数据、未更新 `model_bss_api_latest`，未提交或推送。S03 的非空 v36 SQL 聚合验收已完成；仍待最新原型复核、浏览器/API/数据库联合验收、服务端筛选/排序/分页口径确认，以及多币种授信列表显示规则确认。

## Stage 91：供应商列表真实页面/API/v36读取联调（2026-10-24）

新增 `model-pricing-web/tests/supplier-workspace.live.browser.mjs`，供真实认证浏览器验收使用。测试启动本地 Vite 前端并代理至临时 API；API 由当前工作分支源码启动，连接本轮从运行库克隆的 clean v36 隔离数据库。隔离库仅插入两条带不同状态的 QA 供应商及其资源、授信、合同、流程、申报和计划数据，正式 `model_bss` 未写入。

Playwright 使用内部测试账号实际登录，页面调用真实 `/api/internal/supply/suppliers`。验证潜在主体、资源数、最新授信金额与状态、归档合同状态、未完成事项投影，以及“已有”与“有未完成事项”两个前端筛选。测试通过；浏览器无页面异常。Go 非空查询测试此前已验证相同事实聚合。本次确认了供应商列表的真实浏览器→认证 API→v36 SQL **读取链路**，没有验证列表写操作，也未覆盖服务器端筛选/排序/分页。

临时 API、隔离数据库、QA数据和转储已清理；正式数据库只读复核为 `36|false`，供应商和标准模型数量均为0。未修改 `model_bss_api_latest`，未新增 migration，未提交或推送。最新 Lovable 原型状态口径、多币种额度展示和列表服务器端分页/排序仍待核对。

## Stage 92：F12供应商/客户双主体只读面板真实联调（2026-10-24）

F12现有兼容读取从供应商扩展为 `SUPPLIER` 和 `CUSTOMER` 两种主体；客户主体校验按 `customer_profile.id`，列表查询始终同时限定 `subject_type` 与 `subject_id`。新增共用 `F12SubjectPanel.vue`，挂载到供应商详情和客户详情。保留只读边界：现有 v36 结构不能提供完整合同档案名/编号、稳定档案键、附件ID和不可变合同版本，也没有完整写入及受控附件服务。

`tests/f12-subjects.live.browser.mjs` 经真实内部登录启动当前工作区前端，代理至临时 API `8081`，并读取 PostgreSQL 隔离克隆。Playwright 分别打开 QA 供应商和客户详情的 F12 面板；二者故意使用相同数值主体 ID `10`。真实合同/额度 GET 均为200，供应商显示归属本主体的 `125.00 CNY` 当前额度及合同状态，客户显示 `500.00 CNY` 及其独立合同状态，供应商金额未混入客户面板，浏览器无页面异常。首次联调发现测试定位器对表格重复金额采用严格单元素匹配；改为精确文本并选择首个重复单元格后通过，应用行为本身正常。

隔离克隆在本轮仅应用仓库已有 `000037`，版本为 `37|dirty=false`；`supplier_alias` 表不存在，供应商主体详情和 F12读取仍正常。前序定向集成测试已在 clean v36 隔离库通过 `customer_profile` 同 ID 隔离和缺少 alias 表读取。正式运行库没有写入或迁移，`model_bss_api_latest` 未更新；本结果不代表 v36 主库页面/API整体已升级或部署。此切片只证明合同/授信读取链路，不证明新增、修订、归档、附件、授权或 readiness。

同步修正 F12 与曾用名交接文档中过时的状态描述：F12 客户主体读取和共用只读面板已存在；缺少 `supplier_alias` 时主体读取、当前名称搜索及不改名资料维护可用，曾用名及改名仍等正式结构交付。未添加或修改迁移，未提交或推送。

## Stage 93：供应商跨采购人只读与拒绝写入真实验收（2026-10-24）

新增 `tests/supplier-permission.live.browser.mjs`。Playwright 以真实 `buyer_b` 账号登录隔离 API，按供应商名称查询 `buyer_a` 归属的 QA 主体；列表响应 `can_edit=false`，页面“维护”入口禁用。脚本从实际页面请求取得该会话的 bearer 凭证，再直接 PUT 主体资料并携带幂等键，API 返回403。QA 名称、区域、税率及版本经隔离库核对未被改写；这覆盖了之前仅有 fixture 的非归属权限缺口。

测试连接当前工作区源码的临时 API `8081` 和 `model_bss_a_supplier_s03_e2e_20261024`（`37|dirty=false`）隔离库。首次页面无筛选加载因克隆库遗留的旧 QA 行返回500；按指定主体名称筛选后成功读取并完成反例。此异常发生于未筛选列表数据集，不是本次针对主体的403结果；后续应清理仅在隔离副本的旧 QA 数据或在干净克隆库重验全列表。主运行库未写入，`model_bss_api_latest` 未更新，未迁移、未提交或推送。

## Stage 94：主供应商工作区接入主体档案入口（2026-10-24）

新增主工作区“供应商主体档案”入口，跳转到 F4 主体列表并携带当前供应商名称、区域筛选及返回上下文；主体档案页打开时应用筛选，返回时还原供应商列表的名称、区域、合同、合作阶段和办理状态筛选。此前主体页路由 `menu=false` 且页面内无入口，新建与档案维护不易从主供应商管理到达。

先在 `tests/supplier-management.browser.mjs` 增加入口、筛选带入及返回断言，RED 因主页面没有该按钮而失败。实现后该 Playwright fixture 通过，`npm.cmd run typecheck` 通过。随后扩展真实联调脚本并以内部员工真实登录验证：从主“供应商管理”点击入口，前端发起带供应商名称筛选的真实主体列表 GET，API 返回对应隔离主体；返回操作恢复原列表筛选。真实页面/API/API数据库链路测试复用本轮临时 API `8081` 和隔离 v37 数据库，未写主库。

这一切片只开放并验证主体页导航，不等于共享测试配置、资质或主体合并已完成。未添加迁移、未更新正式 API 容器、未提交或推送。

## Stage 95：F12 双主体写入契约与增量结构请求（2026-10-24）

根据总览、A任务拆分、A详细设计和共享契约，补齐 F12 合同/授信写入规则及当前 `contract`、`credit_line` 与目标能力的差异。新增 `supply/f12-records.draft.md`，定义双主体隔离、合同稳定档案与追加版本、授信方向/币种/最新额度、首次办理授权、附件、幂等、冲突及页面验收；新增 `supply/supplier-f12-schema-request.md`，要求迁移维护人先检查现存重复版本并制定兼容/回退方案。

本切片只修改契约和交接文档，没有改 Go/Vue 代码、运行数据库或迁移，也没有执行测试；因此不构成 F12 写入实现或联调通过。`supplier-customer-a-plan.md` 与 supply 索引已链接两份新文档。下一步继续可独立完成的 F4 能力；正式 F12 写入需等结构请求审核/迁移和 D07 文件服务、B/C流程/主体上下文端口落实后接入。

## Stage 96：供应商工作台筛选、排序与分页接入真实 API（2026-10-24）

供应商工作台原先一次读取整表并在浏览器本地筛选。本阶段将名称/曾用名、区域、合同状态、合作阶段、办理状态组合筛选接到 `/api/internal/supply/suppliers`；查询结果由服务端返回 `list/total/page/size`。排序只接受白名单字段，按 ID 稳定收尾；没有多币种比较规则前不提供授信金额排序。分页大小最大 100。筛选计数与列表在同一只读 `REPEATABLE READ` 事务快照中读取。曾用名表未交付时，接口继续降级为当前名称检索。

TDD先扩展 `supplier-management.browser.mjs`，运行时因缺少“查询”入口失败；再加 Go SQL/参数契约测试，最初因查询实现缺失失败。实现后，Go 1.26 Docker 环境下 `TestSupplierWorkspacePageQuery*`、`TestParseSupplierWorkspaceQuery*` 及供应商列表契约测试通过；`npm.cmd run typecheck` 与 fixture Playwright 通过。

真实页面验收重启了仅连接隔离库 `model_bss_a_supplier_s03_e2e_20261024` 的临时 API `bss_s03_api_e2e`（8081；源码只读挂载），未动 8080 主 API。真实员工认证下 `supplier-workspace.live.browser.mjs` 从当前工作区页面读取隔离 v37 数据，组合筛选得到 QA 已有供应商，排序请求返回200，并验证实际请求携带 page/size/sort 参数。此切片只读，没有写业务行或迁移；主库与正式 API 未触碰。最新 Lovable 状态口径、曾用名结构交付和正式部署仍未完成。

## Stage 97：供应商详情资质安全读取（2026-10-24）

按现有 `supplier_qualification` 原生结构补齐只读资质页面/API：`GET /api/internal/supplier-entities/:id/qualifications?page=&size=` 返回资质ID、主体ID、名称、附件是否关联及上传时间；查询按主体筛选，在只读 `REPEATABLE READ` 事务中取数。旧 `file_ref` 可能是物理存储路径，SQL 与响应均不投影该值；现结构没有审核状态，页面明确写“审核状态未记录”，也不提供伪造的下载/审核通过按钮。真实上传、D07 `file_id` 绑定、授权下载和审核事实仍待对应能力接入。

TDD中先新增领域读取契约测试并确认缺失类型/方法导致编译红灯；再让供应商详情fixture断言资质区，首次因页面无资质入口失败。实现后，Docker Go 1.26 定向 `go test ./internal/domain/supply ./internal/api ./internal/infra/supplypg -run 'SupplierQualifications|SupplierQualificationHTTP' -count=1` 通过；`node --experimental-strip-types --test tests/supplier-entities.test.ts` 7项通过，覆盖主体隔离与拒绝包含 `file_ref` 的客户端响应；`node tests/supplier-management.browser.mjs` 和 `npm.cmd run typecheck` 通过。

真实 Playwright 使用 smoke_admin 登录临时 8081 API；页面直接读取本工作区代码及 clean v37 隔离库中的新建 QA 供应商和非空资质行。GET 返回200，页面显示资质名称/附件关联/未记录审核状态，JSON不含 `qa-e2e://` 哨兵路径。过程中发现并绕开了旧的原始 QA 供应商行缺少 `assignment_log`/`operation_log`、无法通过供应商审计读取校验的问题，使用正式 API 创建的 QA 主体完成页面联调。测试结束删除两条标记资质、临时主体及其归属/审计行；marker行剩余0，隔离 API 停止。隔离库保持 v37 clean；未迁移、未修改 `model_bss` 主库或 8080 `model_bss_api_latest`，未提交或推送。

## Stage 98：供应商合并只读影响预览真实联调（2026-10-24）

新增只读合并预览契约、领域投影、PostgreSQL `REPEATABLE READ` 仓储、内部 API 和供应商主体列表对话框。`POST /api/internal/supplier-entities/:id/merge-preview` 返回双方主体/revision、供应商/资源/申报/办理/合同授信/配置/资质/客户关系等关联数量、重复模型区域组合及复核提示。凭据和网站密码只投影存在/引用数量；不返回密钥、密码、`file_ref` 或物理路径。曾用名结构不存在时以 `aliases:null` 和 `ALIAS_STRUCTURE_UNAVAILABLE` 呈现，不伪装为零。当前没有 `can_merge` 字段，也没有合并写 API 或确认按钮；跨区域、重复资源和 B/C 引用冲突的处置规则仍待确认。

TDD 的领域/API用例和主体列表浏览器场景先因预览契约/入口缺失失败，补齐实现后通过。Go 1.26 Docker 定向 `go test ./internal/domain/supply ./internal/api -run 'TestSupplierMergePreview' -count=1`、`go test ./cmd/server -run '^$' -count=1`、前端 `npm.cmd run typecheck` 及 `node tests/supplier-management.browser.mjs` 均通过。新增 `tests/supplier-merge-preview.live.browser.mjs`：smoke_admin 真实登录，在临时 v36 克隆库通过页面创建两条唯一 QA 主体，页面调用真实 API 获取双方 revision/关联计数并显示预览，接口返回200；脚本确认响应不含凭据/文件路径/`can_merge`，预览阶段无 PUT/DELETE 请求，浏览器无异常。该真实验证覆盖本工作区源码、认证页面、API和PostgreSQL，不是 fixture 结果。

临时数据库为 `model_bss_a_merge_preview_live_20261024`，运行前确认 `36|false`；测试后删除 QA 克隆库及临时 API/PostgreSQL 容器。正式 `model_bss_api_latest` 与 `model_bss_pg` 保持运行，主 API `/healthz` 返回200，主库没有写入或迁移。源码尚未部署到正式 API，合并执行尚未实现；未提交或推送。

## Stage 99：供应商共享测试配置的 Inspector 接入契约（2026-10-24）

根据 DEC-017 明确 F4 数据边界：供应商自测使用供应商自己的 URL/Key/额度，内部测试使用平台配置；供应商配置一次并由 Inspector 后续复用，页面不回显 Key，不新增 BSS AES-GCM。新增 `supply/supplier-test-config.draft.md`，并在 S04 计划、供应链交接和索引中记录责任及接口请求。当前代码只有申报行/资源级凭据字段，未发现供应商级共享存储或 Inspector 持久配置 API。契约明确不挪用旧字段、不复用正式配置、不预建重复 Key 表；待 B/Inspector 提供真实保存/关联 API、稳定配置引用/版本和任务调用事实后，A再实现页面/API适配。

本阶段只更新契约与交接文档，没有修改代码、数据库或迁移，未运行测试。共享测试配置仍未实现或联调；F4源码与F12写入工作继续按计划推进。

## Stage 100：供应商资质受控上传与下载（2026-10-24）

在没有可复用 D07 文件提供方的情况下，实现仅供供应商资质使用的本地受控文件适配器：PDF/PNG/JPEG 正文校验、单文件不超过 2 MiB、随机 opaque 引用、私有目录写入、按主体/资质行授权下载。v36 原生 `supplier_qualification.file_ref` 只保存 opaque 引用；旧物理路径不会返回或下载。API 列表/上传响应只返回安全文件名和 `can_download`，不返回内部引用或磁盘路径；操作记录只含名称和主体，不含引用。页面已接入上传、列表刷新和下载；审核状态保持“审核状态未记录”。服务通过 `MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR` 启用，正式容器必须把该路径挂到持久私有卷。

TDD 先添加字节数不匹配拒绝下载的用例并看到失败，再补充校验；随后针对“存储只返回元数据却被宣称可下载”添加第二个 RED 用例并拒绝该状态。另为上传响应补安全 `file_name` 契约，首次 HTTP 测试因字段缺失失败，修正后通过。Docker Go 1.26 定向 `go test ./internal/infra/supplyfiles ./internal/domain/supply ./internal/api ./internal/infra/supplypg -run 'SupplierQualification|QualificationFileStore' -count=1` 通过；`go test ./cmd/server -run '^$' -count=1` 编译通过；前端 `node --experimental-strip-types --test tests/supplier-entities.test.ts`（7项）、`node tests/supplier-management.browser.mjs` fixture 和 `npm.cmd run typecheck` 通过。

新增 `model-pricing-web/tests/supplier-qualification-files.live.browser.mjs`。Playwright 使用真实内部登录，经本机 Vite → 临时 API 8081 → PostgreSQL v36 隔离克隆，创建 QA 供应商、上传 PDF、通过真实资质 GET 读回并下载，下载字节与上传内容一致；上传/列表响应未暴露 `file_ref` 或 opaque 引用。隔离数据库只读复核确认资质行、格式正确的 opaque 引用和 `UPLOAD` 操作日志已落库，日志不含文件引用。测试结束前主库仍为 `36|dirty=false`、供应商及资质行数为0；未改 8080 `model_bss_api_latest`，未迁移、提交或推送。临时 API、隔离库与 dump 随本轮结束清理。

此切片完成 F4 资质附件的当前源码和真实隔离联调，不代表正式环境已启用：8080 需要配置并持久挂载 `MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR` 后才能上传/下载。审核状态和历史物理路径迁移仍未接入；F12 附件仍需另行接入双主体授权及版本资料。

## Stage 101：供应商合同状态缺失值修正真实联调（2026-10-24）

修正供应商工作台合同状态的缺失事实处理。此前在没有合同或合同办理节点时，SQL 会用 `supplier.established` 推断“已归档”，否则默认“办理中”，把主体首次接入状态混成合同状态。现在只有合同资料或可信合同节点明确给出状态时才返回“办理中/已归档”；没有这些事实统一显示“未登记”。工作台新增“未登记”筛选，并按状态使用中性标签。合作阶段仍单独来自 `supplier.established`，不影响其他摘要维度。

TDD先增加 SQL 投影、参数解析及非空 v36 集成断言。RED 在当前 SQL 缺少“未登记”投影时按预期失败；实现后 Docker Go 1.26 `go test ./internal/api -run 'TestSupplierWorkspace' -count=1` 通过。`TestSupplierWorkspaceProjectionWithIsolatedV36Data` 在 clean v36 隔离克隆运行，覆盖已接入但没有合同/合同节点的供应商仍为“未登记”，以及潜在、已有、真实合同状态、筛选排序/分页等既有摘要行为。`node tests/supplier-management.browser.mjs` fixture Playwright 和 `npm.cmd run typecheck` 通过。

另新增 `tests/supplier-contract-status.live.browser.mjs` 并真实运行：内部账号登录当前工作区前端，由 Vite 代理到当前源码临时 API，API 连接 PostgreSQL v36 克隆。页面显示 QA 已接入供应商为“未登记”；选择该状态后真实列表 API 以 `contract_status=未登记` 返回对应非空行，浏览器无页面错误。该联调为只读。临时数据库由主库只读转储恢复后创建，临时 API、克隆数据库、QA 行和转储均已清理；随后只读确认正式库 `36|dirty=false`、本轮 QA 标记计数为0，正式 `model_bss_api_latest` 健康且未更新。没有新增或改动迁移，未提交或推送。

本阶段仅完成合同状态摘要的错误推断修正，不等于 F4 整体完成：供应商测试配置仍待真实 Inspector 持久配置接口，别名仍待统一迁移维护人交付结构，合并执行仍待冲突规则；F12 写入/版本/附件尚未实现。

## Stage 102：供应商报价审批入口统一到工作台（2026-10-24）

供应商管理页面原先有独立的“待审批/审批记录”页签、审批分配弹窗和模拟接入操作，与审批事项由工作台待办展示的最新交互口径重复，也会让 A 页面承担 B 的审批流程。现已移除这些审批操作，保留供应商详情中的只读报价信息，并增加“打开工作台待办”导航入口。

TDD 浏览器用例覆盖从供应商管理进入工作台、加载共享 inbox，且供应商工作区不请求旧审批列表/记录 API。改动前该场景因触发旧审批列表请求而失败；改动后 `node tests/supplier-management.browser.mjs` 通过。`npm.cmd run typecheck` 通过。该用例通过 fixture API 验证导航和请求边界，不构成真实认证、API、数据库联调；本切片没有改 Go、数据库或迁移，也没有部署、提交或推送。

## Stage 103：正式配置保存、Key保留与轮换真实联调（2026-10-24）

补充 `model-pricing-web/tests/supplier-formal-config.live.browser.mjs`，使用当前工作区前端、真实内部认证、临时 API 和 PostgreSQL v36 克隆库，在供应商模型详情“接入与配置”页完成正式配置新建、留空保留 Key、Key 轮换、整页刷新并重新认证后的持久化读取。新建返回 revision 1，留空更新为 revision 2，轮换为 revision 3；页面/API 响应只显示“已设置”和固定掩码，不回显本轮虚构 Key。隔离库只读核对确认配置 Key 已保存、对应三条操作记录存在且均不含 Key 原文。

首次联调发现 QA 供应商缺少正式归属审计行，补齐隔离夹具后再以克隆库实际 buyer_a 身份（staff 1）验证；测试 URL 使用保留 `.example` 域名时被正式公网 URL 策略按预期拒绝，改用公网 IP 形式继续验证。后续发现隔离库已有旧 QA 配置，浏览器用例误点首行；已改为每轮唯一配置名并按行定位。当前真实页面/API/数据库主链通过。最终隔离库版本为 `36|dirty=false`；测试后已停止并删除临时 API、克隆数据库。正式库仍为 `36|dirty=false`、供应商数量0，未更新 `model_bss_api_latest`，未新增迁移、未提交或推送。

此证据覆盖正式生产配置保存及安全投影，不等于 B/Inspector 的供应商共享测试配置已接入；后者仍待真实唯一保存方及接口。资源绑定/逐资源接入仍依赖 B 的办理和报告事实，F12 双主体写入/版本/附件也未因本切片完成。DEC-018 继续生效：现有 `api_key_enc` 列保存原始 Key，不使用 AES-GCM；响应和操作记录不返回原文。

## Stage 104：F12 双主体合同/授信写入命令校验（2026-10-24）

新增 `internal/domain/supply/f12_write.go` 与领域用例，定义合同和授信写命令的基础规范化及校验：主体仅 `SUPPLIER`/`CUSTOMER`，供应商授信方向固定 `SUPPLIER_TO_US`、客户固定 `US_TO_CUSTOMER`；金额为正数、最多两位小数并规范为精确两位字符串；合同/授信资料名称、编号、`file_id` 和操作原因必填。校验器不判断主体是否真实存在、操作者是否有权、文件是否属于该主体、流程是否可办，也不分配版本或写数据库；这些必须由后续事务适配器及 B/C/D07 提供方真实校验。

TDD 先添加两类写命令用例，Go 1.26 Docker 定向测试因目标命令类型/校验器缺失而失败；实现后 `go test ./internal/domain/supply -run '^TestValidateF12.*WriteCommand' -count=1` 与 `go test ./internal/domain/supply -count=1` 通过。该切片没有 API、存储、页面或浏览器联调，因此不计作 F12 写入闭环。未新增/修改迁移，没有连接或写入数据库，没有部署、提交或推送。

## Stage 105：F4 资质字段结构请求补齐（2026-10-24）

核对 `000033_target_model` 的 `supplier_qualification` 只有 `id/supplier_id/name/file_ref/uploaded_at`；A 详细设计 §5.3 还要求资质类型、有效期、审核状态和 revision。Stage100 的基本上传/下载代码及真实隔离联调不覆盖这些字段，当前“审核状态未记录”保持正确。新增 `supply/supplier-qualification-schema-request.md`，明确字段语义、D07 `file_id` 类型/归属确认、旧 `file_ref` 兼容、迁移预检/回退和验收场景；在 supply 索引与实施计划登记。

本切片仅新增 A 侧结构请求文档，没有改业务代码、迁移或数据库。当前工作区定向检查：`npm.cmd run typecheck`、`node tests/supplier-management.browser.mjs`、`node --experimental-strip-types --test tests/f12-subject-records.test.ts` 及 `git diff --check` 通过；供应商浏览器脚本使用 fixture API，本轮未做新的真实 API/数据库联调。后续由迁移维护人确认 schema 与 D07 文件ID，再实现资质资料维护表单和 API；审核状态值须由产品/流程提供方给出，不能从上传推断。工作区未提交、未推送。

下一步仍需按结构请求接入稳定合同档案/版本和授信维度存储，再实现真实主体/办理授权与 D07 文件归属校验；现有面板继续只读，不能用本切片作为开放写按钮的依据。

## Stage 106：F12 双主体合同/授信版本服务与领域规则（2026-10-24）

新增 `internal/domain/supply/f12_record.go` 和 `f12_record_service.go`。领域规则支持合同按稳定档案键追加不可变版本、归档另追加状态版本且保留正文/附件，以及授信按 `(subject_type, subject_id, direction, currency)` 追加精确金额版本。`F12RecordService` 编排合同新增、修订、归档和授信版本写入，通过事务端口要求主体锁、真实办理/维护授权、附件归属检查、版本写入及操作记录同事务完成。版本规划拒绝过期 expected version、重复或断档历史、主体/维度错配、ID冲突和超过 v36 `integer` 版本上限；已归档合同不可继续修订。新记录校验操作人和时间，统一转 UTC 微秒。没有用 `note` 或 `file_ref` 填补现有 schema 缺少的结构化字段。

TDD 先观察到版本规划、服务创建以及修订/归档主路径因占位依赖返回错误；实现后，Docker Go 1.26 下 `go test ./internal/domain/supply -run '^Test(AppendF12(Credit|Contract)Version|ArchiveF12Contract|F12RecordService)' -count=1` 与整个 `go test ./internal/domain/supply -count=1` 通过。覆盖合同新建、修订保留旧版、归档保留正文/附件、授信新金额取新版本、主体授权拒绝、文件端口调用和操作记录失败时不提交；事务回滚断言使用内存 UoW fixture。

此切片交付了领域服务和明确的事务适配端口，但尚无 PostgreSQL 适配、HTTP 路由、共享幂等响应或实际 B/C/D07 提供方装配，也未接页面写入，不能视为 F12 写入闭环。当前 `000033_target_model` 的 `contract` / `credit_line` 确实缺少结构请求中的档案键、资料名称/编号、D07 `file_id` 和来源字段，统一迁移维护人仍需审核结构请求并部署兼容结构。另只读核对 Docker：`model_bss_pg` 当前 `schema_migrations=36|false`，仓库迁移已到 `000037_customer_profile_workspace`；`model_bss_api_latest` 与数据库容器均在运行。本轮未应用迁移或重建容器。未新增或修改迁移、未写数据库、未运行 Playwright、未部署，未提交或推送。工作区已有的其他 A 改动均保留。

## Stage 107：供应商主体页复用 F12 双主体共用面板（2026-10-24）

供应商主体列表的“合同 / 授信”弹窗此前重复实现一套 F12 只读表格，与供应商详情、客户详情使用的 `F12SubjectPanel.vue` 并存。现改为在原弹窗中挂载同一共用面板，主体类型固定传 `SUPPLIER`，主体 ID 使用所选供应商；删去重复的本地加载、数据规范化和状态展示，避免两套面板逐渐出现不同规则。同步更新 F12 真实浏览器脚本定位器，仍断言真实合同、授信请求按该供应商主体发出。

TDD 在 `supplier-management.browser.mjs` 先加入共享组件及双 API 请求断言；首次运行按预期因页面没有 `.f12-subject-panel` 超时失败。实现后该 Playwright fixture 通过，`npm.cmd run typecheck` 通过；F12 双主体响应规范化测试也通过。此处 Playwright 使用 fixture API，不是本轮真实认证/数据库联调。只读 Docker 检查确认 API 与 PostgreSQL 容器正在运行，主机端口映射为 8080/5433，API 容器挂载的是 `bin/server-v36`；本轮没有确认可用的 QA 账户、QA 主体及隔离数据库连接，因此没有向运行库写数据或冒充真实联调通过。未改容器或迁移、未提交或推送。真实 F12 页面/API/数据库验证仍待使用当前源码 API 和隔离数据库执行。

## Stage 108：F12 合同与授信历史分页（2026-10-24）

共用 `F12SubjectPanel.vue` 此前固定请求第一页 100 条，超出部分没有翻页入口。现让双主体 API 客户端接收页码/页大小，面板按每页20条分别分页合同和授信历史；翻页始终带 `subject_type` 与 `subject_id`，当前额度仍使用独立的最新额度投影，不随历史页码被误当作当前值。此次仅扩展现有只读 GET，不新增数据库结构或写入口。

TDD 在 `supplier-management.browser.mjs` 准备每类22条供应商记录并先运行，旧页面一次加载22条而非20条，未满足分页断言；实现后 Playwright fixture 验证合同和授信各自第2页都请求 `page=2&size=20`、保留主体隔离并显示第21条数据。同步运行 `node --experimental-strip-types --test tests/f12-subject-records.test.ts`（2项通过）、`node --check tests/f12-subjects.live.browser.mjs` 与 `npm.cmd run typecheck` 均通过。此切片的分页操作使用 fixture API；未做真实认证或隔离库非空分页联调，因此不把它记为真实 API/数据库验收。未改迁移、未写库、未部署、未提交或推送。

## Stage 109：F12 双主体非空历史分页真实联调（2026-10-24）

扩展 `model-pricing-web/tests/f12-subjects.live.browser.mjs`，通过真实内部账号登录当前工作区前端，由 Vite 代理到临时 API 容器 `bss_a_f12_api_qa_20261024`（8081），后端运行当前工作区源码。API 连接 `model_bss_pg` 中的隔离数据库 `bss_a_f12_work_20261024`；该库从正式库只读克隆后应用仓库现有 `000037_customer_profile_workspace`，当前 `schema_migrations=37|false`。供应商和客户分别使用数值 ID 900001，各有 22 条合同与 22 条授信历史记录。

Playwright Edge 实际打开供应商主体列表和客户详情，对 SUPPLIER/CUSTOMER 两类主体的合同及授信历史分别点击第 2 页。四个 API 响应均为 200，包含匹配的 `subject_type`、`subject_id=900001`、`page=2`、`size=20`；API 的 `total=22` 且第 2 页返回 2 条，页面表格也分别呈现 2 行。翻页后当前额度仍分别为供应商 125.00 CNY、客户 500.00 CNY；同数字 ID 未造成数据串读。脚本同时验证从供应商工作区进入筛选后的主体档案并返回时保留筛选值。

首次执行暴露浏览器断言的选择器问题（同金额显示两次、记录 ID 不在表格展示、版本号不唯一），只修正测试定位方式后重跑通过；未改业务实现。`node tests/f12-subjects.live.browser.mjs` 通过，`node --check tests/f12-subjects.live.browser.mjs` 通过。该验收覆盖真实认证、当前前端、当前 API 源码和隔离 PostgreSQL 数据，不覆盖 F12 写入、附件、授权办理/readiness，也不代表已部署至 8080 正式 API。

临时 API 与隔离库为后续 A 联调保留；本轮没有写正式数据库、修改迁移、重建正式容器、提交或推送。测试后只读复核正式库为 `36|false`，正式库中本轮 QA 供应商标记数量为 0；隔离库为 `37|false`，其中 QA 主体仅用于本轮联调。

## Stage 110：F12 首次办理来源 revision 与 readiness A 侧源码（2026-10-25）

补齐 F12 首次办理来源追溯源码。`F12FlowWriteAuthorizer` 的流程授权现在必须在资料写入事务内同时核对主体、模块、办理人和流程状态，并返回真实流程 revision；普通主体维护另经 `AuthorizeF12OrdinaryWrite` 确认首次商务流程已结束，不能通过不带 flow 参数绕开指定办理人。合同/授信版本保存 `source_flow_revision`，普通维护保持 NULL；HTTP 只接受 `expected_flow_revision` 作为 CAS 比较值，后端不接受客户端提供实际 `source_flow_revision`。事务授权器必须比较预期 revision，并只返回可信实际值供落库。F12 只读 DTO/SQL 增加可选来源字段，旧 schema 返回空来源，不将遗留记录当成流程凭据。附件上传启用前也检查该增量列；附件下载新增独立的 `AuthorizeF12FlowRead` 端口，避免把读取授权混入写入/CAS。

新增 `GET /api/internal/f12/readiness`、`F12ReadinessService` 及 PostgreSQL `ListF12ReadinessEvidence`。调用者只能提交主体与 flow/module 标识；服务端 `F12BusinessContextProvider` 返回经过核实的主体、流程revision及模板必需项。A 从数据库读取同主体、同 flow/module 的当前有效合同/授信资料，校验资料、附件及来源revision，返回明确的版本引用、当前额度和缺失原因。流程 revision 或模板在读取前后变化时返回409；缺少真实提供方或 schema 时返回依赖不可用，绝不通过假模板或默认ready填充。

本机执行 `npm.cmd run typecheck` 通过，`git diff --check` 通过。环境未安装 Go/go fmt，因此未做 Go 编译；按 Docker维护约束没有运行 Go测试、数据库操作、Docker或Playwright。

**状态：源码和定向领域/HTTP用例已补，Go未编译、未运行测试、未访问Docker/数据库，也未做页面/API联调。** 当前主装配尚无 B/C `F12FlowWriteAuthorizer` 与 `F12BusinessContextProvider` 实际实现；结构请求尚未审核交付 `source_flow_revision` 等列。因此该接口目前是 fail-closed 接入契约，不代表 F12写入或首次办理链路可用。Docker恢复并收到用户通知后，定向验证普通维护绕过、流程主体/办理人错配、expected revision竞争、同主体来源资料readiness及双主体写入隔离。没有新增/修改迁移，也未提交或推送。

## Stage 111：F4 供应商测试配置 A 侧适配壳层（2026-10-25）

新增 `SupplierTestConfigProvider` 端口、领域输入校验和无 Key 安全投影；新增 `GET/PUT /api/internal/supplier-entities/:id/test-config`，Key 只作为写入参数，未知提供方错误统一 fail-closed；供应商详情页“档案与办理”加入测试 URL/Key 配置卡片。空 Key 表示让提供方保留已存 Key。未在 BSS 自建凭据持久化，也未实现或假设 Inspector API。

当前公共主装配没有注入 Inspector provider，因此运行接口仍返回依赖不可用，配置不能真实保存或供 B 测试调用。B/Inspector 仍需交付真实保存/关联 API、稳定配置引用/版本、认证方式、留空保留与轮换语义，以及测试任务如何指定并留存配置版本；采购归属授权和 Inspector 持久事实也须由提供方落实。

`npm.cmd run typecheck` 通过；`git diff --check` 通过。Go 工具链不可用，未编译或执行 Go 用例；遵守 Docker 维护约束，未运行 Docker、数据库或 Playwright，也未做真实页面/API联调。此阶段仅确认前端静态类型和差异格式，不能标记为 F4 配置功能验收通过。没有新增/修改迁移，未提交或推送。

## Stage 112：F12 附件下载文件名与浏览器释放时序（2026-10-25）

F12 共用面板原先直接用合同/授信的资料名称作为下载文件名，并在触发下载后立即回收 object URL。现增加文件下载响应元数据读取，优先采用服务端 `Content-Disposition` 的 `filename`/RFC 5987 `filename*`；清除路径分隔符和控制字符，保留资料名作为安全回退。object URL 改为点击后延迟回收，降低浏览器尚未开始读取时链接失效的可能。

`npm.cmd run typecheck` 通过，`git diff --check` 通过。没有运行浏览器或真实下载场景，故本条只证明源码与类型检查，不代表附件下载联调已验收。Docker、数据库、Go测试均未访问；没有修改迁移、提交或推送。

## Stage 113：F12 流程来源、实际办理人及说明展示（2026-10-25）

双主体合同/授信读模型现保留来源 flow、模块与 revision。前端仅在服务端返回完整来源且 revision 合法时，将经流程授权写入的认证 `created_by` 作为实际办理人 ID 展示；普通维护显示登记人 ID，不伪装成流程办理人。合同和授信历史表新增办理来源、实际办理人 ID 及变更说明列。增加 DTO 用例覆盖来源映射、说明保留及来源/revision 不完整时拒绝响应。项目当前未找到共享人员姓名目录 API，因此展示受校验的内部 ID。

`npm.cmd run typecheck` 通过，`git diff --check` 通过。新增 DTO 用例未运行；未运行浏览器、Go、数据库或 Docker 验证。真实流程当前办理人仍依赖 B/C 授权提供方，源代码准备不代表已完成联合验收。未改迁移、未提交或推送。

## Stage 114：F12 写入冲突的弹窗内刷新与显式重试（2026-10-25）

合同新增/修订、授信版本和合同归档遇到 HTTP 409 时，错误现在显示在对应编辑弹窗中，保留表单和已上传附件。用户点击刷新后，页面重载最新主体记录；若版本仍可用，就更新 expected version、换用新的幂等生命周期，并提示用户检查后手动重试。流程上下文存在时同时发出 `refresh-flow-context` 事件，重试按钮保持禁用，直到宿主回传新的流程 revision。合同已归档或授信版本仍歧义时不会开放重试。

`npm.cmd run typecheck` 通过；本条后续 `git diff --check` 通过。未运行 Playwright/组件用例、Go、数据库或 Docker。流程刷新事件的 B/C 宿主尚未接入，真实 409 重载、输入保留和幂等重试仍待浏览器/API验收。未改迁移、未提交或推送。

## Stage 115：F12 附件版本链归属与启动孤儿清扫（2026-10-25）

新上传的合同/授信附件必须由当前认证操作者上传；修订可以复用同一合同或授信版本链已有的附件，但不放宽主体和用途校验。文件验证端口接收预期上传人及当前版本链；在SQL事务中拒绝已被其他合同或授信链引用的文件，只有同链旧附件可沿用既有上传人。若文件存储返回部分结果或返回的附件元数据不符合约定，服务会在脱离已取消请求上下文的短超时内尽力删除文件，并保留原错误。另增加启动时孤儿扫描：只清理超过24小时、manifest有效且合同与授信均未引用的附件；缺少F12字段时跳过清扫，不影响服务启动。

增加定向用例源码，覆盖当前上传人匹配、同链附件复用、不同上传人拒绝、附件链上下文、过期文件筛选，以及请求上下文取消后的补偿清理行为。本轮未运行 Go 编译或测试（本机 Go 工具链不可用）；也未运行 Docker、数据库、Playwright或前端检查。外层幂等中间件在上传服务返回后才提交数据库事务，不能即时回滚文件；新清扫会在下一次应用启动且文件超过24小时后回收未引用文件，不是即时清理。SQL防跨链引用尚待隔离库验证。该切片待 Docker 恢复后的统一 TDD/联调验证。未修改迁移，未提交或推送。

## Stage 116：F4/F12 私有附件持久目录部署说明（2026-10-25）

新增部署说明，列出 `MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR` 与 `MODEL_BSS_F12_ATTACHMENT_STORE_DIR`，提供将两个独立命名卷只挂载至 API 容器的 Compose 片段，并说明环境变量未配置时的 fail-closed 行为、重建容器要求、卷备份/恢复及私有访问边界。注明仓库没有部署清单，示例服务名需替换；当前本地文件适配器不代表 D07 已接通。

更新 supply 入口和 A 实施计划链接。本阶段只修改文档；未访问 Docker、数据库或 API，未运行测试，部署和持久化验收仍待 Docker 恢复。没有修改迁移、提交或推送。

## Stage 117：F12 合同归档保留来源语义（2026-10-25）

代码审查发现，归档版本原先无论是否处于首次商务办理都复制前一版 flow/module/revision，归档 API 也没有流程 CAS 参数。现流程归档可携带 flow/module/expected revision，由事务授权器检查当前办理人并返回实际 revision；普通维护归档仍按主体授权，清空归档版本里的旧流程来源。原合同版本保持不可变，领域/API用例覆盖授权参数与来源边界。

`npm.cmd run typecheck` 通过。新增的 Go 领域/API用例未运行（当前无 Go 工具链）；未访问 Docker、数据库或 Playwright。此项是源码和测试用例已补、待运行验证；没有修改迁移、提交或推送。

## Stage 118：最新原型访问状态复核（2026-10-25）

本轮先发现已有分享包装页显示过期，随后从用户提供的原始预览地址打开了新的 `Token Operations Demo | Preview`，确认原型画布及工作台、标准模型库、模型广场、供应商管理、客户管理和告警的顶层导航。尝试点击供应商管理时浏览器交互超时，无法读取管理页细节；因此供应商合作阶段/办理状态、F4/F12字段与交互仍未核实，不标记原型对齐。此结果说明原始入口当前可打开，先前的过期提示属于旧分享页状态。

本阶段只更新计划和 SUMMARY；未访问 Docker、数据库或项目 API，未运行测试、未改迁移、未提交或推送。

## Stage 119：F4/F12 路由模块私有文件装配（2026-10-25）

`NewSupplyRouteModule` 新增 `SupplierQualificationStoreDir`、`SupplierQualificationBlobs` 与 `F12AttachmentStoreDir` 可选参数：资质可使用 A 的私有目录或注入共享 BlobStore；配置 F12 目录时在同一构造器中创建本地文件服务、事务内附件验证器与 F12 上传/下载服务。外部 F12 附件服务要求同时提供事务验证器；本地目录与外部附件接入冲突时拒绝模块启动，避免上传端与记录校验端使用不同文件来源。未配置目录或外部服务时保持附件能力未装配。

补充组合用例，断言两个目录均配置时相应服务存在，并扩展原模块构成断言覆盖 F12 读、写、readiness 及供应商依赖。`git diff --check` 通过。Go 编译和用例未运行（本机无 Go 工具链，且用户要求 Docker 恢复后再统一联调）。`cmd/server/main.go` 当前仍调用 `newSupplyRuntimeDeps`，并未切换到 `NewSupplyRouteModule`；A 构造器与公共主装配仍是不同状态。文档已纠正这一差异并列明新参数。未访问 Docker、数据库或 API，未改迁移、未提交或推送。

## Stage 120：F12 私有附件启动清理接入钩子（2026-10-25）

`SupplyRouteModule` 现提供 `CleanupF12AttachmentOrphans(ctx)`，将已配置附件服务的可选 `CleanupUnreferenced` 能力显式暴露给应用主装配。未配置 F12 附件时为空操作；已配置但未实现清理接口时返回依赖不可用，调用方可告警并继续启动。现有本地 PostgreSQL/文件服务会清理超过24小时且数据库未引用的有效manifest文件。新增组合用例覆盖清理数量、缺少清理器和未配置三种情况。

仅做源码与文档调整；`git diff --check` 通过。未运行 Go 编译/用例、Docker、数据库或 API。`cmd/server/main.go` 仍使用 `newSupplyRuntimeDeps`，尚未接入新模块及此钩子；未修改迁移、未提交或推送。

## Stage 121：F12 附件替换使用新幂等键（2026-10-25）

修复合同/授信编辑表单在已上传附件后重新选择文件时继续复用旧附件键和保存键的问题。每次文件选择都会为上传和对应的合同创建/修订或授信保存生成新幂等生命周期，原已上传但未引用的文件由既有启动清理回收。没有放宽附件归属或版本校验。

`npm.cmd run typecheck` 通过。尚未运行浏览器交互或 API 联调；下一轮 Docker恢复后定向验证：先选择并上传文件，再换选另一附件，确认第二次请求携带新键并保存到预期版本；校验2 MiB限制保持有效。`git diff --check` 通过。本轮未访问 Docker、数据库或 API，未改迁移、未提交或推送。

## Stage 122：F12 页面按主体责任人展示操作入口（2026-10-25）

供应商页把服务端主体响应的 `can_edit` 传入共用面板；客户页按真实会话员工 ID 与客户 `owner_sales_operator_id` 比对。非责任人仍可查看只读历史，并看到只读原因说明，不会看到新增/修订/归档/授信操作或附件下载入口；敏感操作处理函数也检查权限，服务端校验保持不变。B/C 嵌入首次办理页面时，需从可信办理事实决定 `canManage`，不能把普通页面的责任人比较当作办理资格。

`npm.cmd run typecheck` 与 `git diff --check` 通过。第一次类型检查发现供应商主体档案弹窗是第三个 F12 面板入口且缺少权限参数；已一并接入该主体行的服务端 `can_edit` 后复查通过。尚未运行页面浏览器场景或 API 联调；未访问 Docker/数据库/API，未改迁移、未提交或推送。

## Stage 123：F12 写路由幂等与流程授权边界复核（2026-10-25）

代码复核确认合同新增/修订/归档、授信追加及附件上传 POST 路由均经过公共幂等中间件。运行装配当前向 `F12RecordStore` 传入 nil 流程授权器，因此 F12 写入按设计 fail-closed；仓库现有 `onboarding_flow` / `onboarding_node` 结构没有可作为权威 CAS 的流程 revision，也没有可装配的 B/C F12 授权实现。不能从时间戳或节点 ID 推算 revision，后续需 B/C 提供同事务主体/模块/办理人/状态/revision 授权事实，并由统一迁移维护人交付对应结构。

本轮 `model-pricing-web` 的 `npm.cmd run typecheck` 通过。Go 不在宿主 PATH；未运行 Go 编译/测试、Docker、数据库、API 或 Playwright。仅更新本 SUMMARY；未改业务源码/迁移，未提交或推送。

## Stage 124：F4/F12 前端生产构建与原型访问复核（2026-10-25）

`model-pricing-web` 的 `npm.cmd run build` 成功，包含 `vue-tsc --noEmit` 和 Vite production build。Vite 提示主 JS chunk 压缩后约 1.71 MB（gzip 约 538 KB），本轮不拆包，以免扩大功能改动范围。

尝试用 agent-browser 打开当前 Lovable 预览时收到 `CDP response channel closed`；读取现有 in-app preview 时调用超时。因此本轮未核对供应商/客户详情内 F12 表单的最新视觉细节；沿用此前原型评审的范围，不宣称这轮完成视觉对齐。未运行项目 Playwright、Go、Docker、数据库或 API；未改业务源码、迁移、提交或推送。

## Stage 125：F12 依赖不可用时保留表单并给出明确提示（2026-10-25）

F12 共用面板现将后端 503/系统依赖错误转换成具体的保存提示，指出数据库结构或办理授权依赖可能未就绪，并说明当前草稿与已选附件仍保留；附件下载失败则显示文件服务专属提示。409、网络及其他错误仍显示原有消息，写操作保持 fail-closed。

`node --experimental-strip-types --test tests/f12-error-messages.test.ts` 3项通过；`npm.cmd run typecheck`、`npm.cmd run build` 与 `git diff --check` 通过。生产构建仍提示主 JS chunk 大于 500 KB。首次运行 Node 22 strip-only 测试遇到项目 `ApiError` 的参数属性语法限制，错误映射已改为读取标准错误对象字段后，定向单测通过。未运行 Go、Docker、数据库、API 或项目 Playwright；未改迁移、未提交或推送。实际 API 503 下草稿保留仍待恢复环境后的页面联调验证。

## Stage 126：F4 资质结构能力未知状态保护（2026-10-25）

资质分页 API 响应必须显式提供布尔 `metadata_supported`；缺少或类型错误时，前端契约解析失败，不再将其默认为 `false` 并误报数据库缺少结构。列表加载、重载失败期间状态保持未知，禁止打开资质上传并显示中性状态；仅服务端明确返回 `false` 时提示结构待交付。`node --experimental-strip-types --test tests/supplier-entities.test.ts` 7项通过，`npm.cmd run typecheck` 通过。未运行生产构建、Docker、数据库、API 或 Playwright；服务端标志与真实上传链路仍待恢复环境后联调。未修改迁移、未提交或推送。

## Stage 127：F4 资质文件下载 Blob URL 生命周期（2026-10-25）

资质下载触发后将 Blob URL 延迟 1 秒释放，与 F12 附件下载和报告下载的已有做法一致，避免立即撤销造成浏览器读取竞态。`npm.cmd run typecheck` 通过；尚未用浏览器验证实际文件内容。未运行 Docker、数据库、API 或 Playwright；未改迁移、未提交或推送。

## Stage 128：F12 客户办理主体 ID 映射边界（2026-10-25）

源码/迁移检查发现 A 的 F12 客户主体用 `customer_profile.id`，而 000033 的 `onboarding_flow.owner_id` 注释指向 `customer.id`；目前没有证据证明两表共享同一 bigint 身份。补充 `F12BusinessContextProvider` 与 `F12FlowWriteAuthorizer` 的接口约束、F12 schema 申请和实施计划交接：B/C 必须在可信数据中解析并验证该关联，写授权与 readiness 一致使用映射。没有提供方前继续 fail-closed，不实现基于数字 ID 相等的猜测映射。该项仅改注释与交接文档；未运行 Go/Docker/数据库/API/Playwright，未改迁移、未提交或推送。

## Stage 129：F4 供应商测试配置主体切换竞态（2026-10-25）

保存供应商 Inspector 测试配置时捕获主体 ID 和请求序号；响应返回前若页面已切换主体、重载或卸载，则忽略旧请求的成功响应和错误，不将旧主体的配置投影到当前页面。网络请求仍会由服务端对原主体重新授权。刷新期间清空未提交 Key，并在保存中禁用刷新。`npm.cmd run typecheck` 通过；尚未运行 Inspector provider、API、Docker、数据库或 Playwright 联调。未改迁移、未提交或推送。

## Stage 130：F4 测试配置提供方未知状态文案（2026-10-25）

首次请求完成前显示加载态；发生API错误时只展示错误信息，不再同时断言“Inspector提供方尚未接入”。只有拿到配置状态后才展示维护表单；页面不保存或回显 Key 原文。`npm.cmd run typecheck` 通过；尚未进行 Inspector/API/Playwright 联调。

## Stage 131：F12 首次办理 CAS 字段白名单（2026-10-25）

合同新建/修订/归档、授信追加和附件上传处理器的严格 JSON 白名单先前漏掉结构体已有的 `source_flow_id`、`source_module_key`、`expected_flow_revision`，因此首次办理 CAS 请求会在授权前被解码器拒绝。现已将三字段加入五个白名单；API 用例覆盖新建、修订、归档、授信追加和附件上传的主体/流程/revision转发，以及缺失 revision 拒绝。`node --experimental-strip-types --test tests/f12-subject-records.test.ts` 4项通过，确认前端可信流程字段投影符合请求契约。按 Docker 维护约束，本轮未运行 Go 测试/编译，也未运行 API、数据库、Docker 或 Playwright。B/C授权与结构仍是实际写入前置；未改迁移、未提交或推送。

## Stage 132：F4 资质上传元数据字段白名单（2026-10-25）

资质上传/修改 DTO 本身已包含类型和有效期字段，但严格 JSON 白名单遗漏新增的三字段以及更新的有效期字段，导致前端发送元数据时被拒绝。现已放行 `qualification_type`、`valid_from`、`valid_to`，并增加新增/修改 API 用例断言三字段传入领域存储接口。按 Docker 维护要求未运行 Go 测试/编译；真实元数据结构支持状态、API 与页面仍待恢复环境后的联合验收。未改迁移、未提交或推送。

## Stage 133：F4/F12 跨模块交接刷新（2026-10-25）

更新 `docs/Work_A/supply/public-integration-request.md`：记录本轮 HTTP 字段白名单修复、Go API 用例未运行状态、前端契约用例/类型检查证据，以及客户主体 `customer_profile.id` 与 000033 办理流程 `customer.id` 的映射责任。没有编译/运行后端、访问 Docker/数据库/API 或 Playwright；无迁移更改、未提交或推送。

## Stage 134：F12 PostgreSQL 错误映射 fail-closed 用例（2026-10-25）

新增纯单元用例保护 F12 缺列/缺表/缺函数时返回依赖不可用，避免接口误判为普通 500；同时固定唯一/序列化冲突、外键错误和缺行的领域映射语义。按当前约束未运行 Go 测试或编译，待 Docker 恢复后与 F12 适配器用例一起执行。未访问 Docker/数据库/API，未改迁移、未提交或推送。

## Stage 135：F12 主体切换异步响应隔离（2026-10-25）

F12 共用面板在合同新增/修订、归档、授信追加及附件下载开始时捕获主体 ID 和流程上下文。用户切换供应商/客户或权限撤销后，旧主体异步响应不会写入当前主体的附件状态、草稿错误、成功提示或触发下载；上传已完成但主体已切换时，孤儿文件仍由既有延迟清扫处理。`npm.cmd run typecheck` 通过。该修复尚未通过浏览器竞态验证；未运行后端、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 136：F12 保存期间锁定表单输入（2026-10-25）

合同、授信和归档请求等待上传/写入响应期间，原表单允许继续编辑或启动另一项操作，可能让附件与提交字段不匹配。现锁定正在提交的表单、文件选择、关闭/重复打开入口和列表刷新；网络请求使用提交开始时固定的主体与流程上下文。`npm.cmd run typecheck` 通过。尚未做浏览器交互验证；未运行后端、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 137：F4 供应商详情切换主体数据隔离（2026-10-25）

供应商详情路由复用同一组件时，切换供应商会重载资源列表，但档案、资质、操作记录、报价可能残留旧主体数据。现切换时取消旧请求、清空旧主体表单/数据并重载当前页签；资料读取、写入、下载和权限刷新都按请求开始时的供应商 ID 与页面代次隔离，旧响应不再更新新主体页面。`npm.cmd run typecheck` 通过。尚未做浏览器路由竞态验证；未运行后端、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 138：F4 资质保存期间锁定表单（2026-10-25）

资质上传/修订等待 API 返回期间，字段、文件选择和对话框关闭入口先前仍可操作，可能让成功回执覆盖用户后来修改的草稿。现保存期间禁用资质字段/文件输入、取消和关闭入口。`npm.cmd run typecheck` 通过；尚未进行浏览器交互验证，未运行后端、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 139：收紧已有资质存储目录权限（2026-10-25）

资质存储目录使用 `MkdirAll(..., 0700)` 只能约束新建目录；已有持久卷可能保留过宽权限。初始化现对通过真实目录/非符号链接检查的根目录执行 `Chmod(0700)`，失败则不启用文件存储；新增 POSIX 用例覆盖将既有 `0755` 目录收紧为 `0700`，Windows 下跳过该 POSIX 权限断言。Go 测试尚未运行（Docker维护中且主机无 Go）；未运行 Docker/数据库/API/Playwright，未改迁移、未提交或推送。

## Stage 140：F12 附件先保存、后进入 SQL 授权事务（2026-10-25）

F12 本地附件上传先前在共享 SQL 事务已锁定主体后才写文件，延长了主体行锁持有时间。现先写受控本地文件存储，再进入共享幂等事务中的 F12 主体锁定并复核当前真实办理授权；授权或事务失败时 best-effort 删除未引用文件，进程中断留下的孤儿仍由延迟清扫处理。公共幂等事务仍覆盖整个 HTTP 请求；本地文件适配不进行网络传输，后续 D07 外部文件传输须放在主体锁事务之外。新增保存顺序和授权失败清理用例。当前环境无 Go/gofmt，Docker仍在维护，本轮未运行 Go、数据库、Docker、API或Playwright验证；未改迁移、未提交或推送。

## Stage 141：清理附件落盘中断产生的孤儿 blob（2026-10-25）

F12 清扫器先前只遍历 `.json` 清单；进程若在 `.blob` 写完、清单写入前退出，内容文件无法进入候选列表。现改为遍历超过 24 小时的常规 `.blob`，只生成 opaque ID 候选；清理服务删除前仍逐条查询合同和授信引用，任何仍被引用的附件都保留。新增缺少 manifest 的孤儿文件用例。Go 测试及数据库引用保留场景待 Docker恢复后运行；本轮未运行 Docker、数据库、API或Playwright，未改迁移、未提交或推送。

## Stage 142：隔离供应商列表过期响应（2026-10-25）

F4供应商列表筛选、排序和分页连续触发时，旧请求可能晚到并覆盖较新的筛选结果；新请求失败时旧行也可能继续显示。`SupplierWorkspace.vue` 现按请求代次隔离响应，只有最新请求能更新列表、总数、错误和加载态，当前请求失败会清空旧结果，卸载会使未完成请求失效。`npm.cmd run typecheck` 通过；未运行浏览器竞态、Go、Docker、数据库或API联调，未改迁移、未提交或推送。

## Stage 143：隔离供应商测试配置保存状态（2026-10-25）

供应商配置卡片切换主体时，旧主体的保存请求即使被忽略响应，原 `finally` 仍会把共享 `saving` 状态清空，可能在新主体保存期间提前解锁输入或重复提交。现增加独立保存代次；主体切换和卸载会使旧 UI 回调失效，旧请求仍由服务端按其原主体/操作者重新授权。`npm.cmd run typecheck` 通过；未运行浏览器竞态、Go、Docker、数据库、API或Inspector联调，未改迁移、未提交或推送。

## Stage 144：拦截测试 URL 中的凭据查询参数（2026-10-25）

供应商测试 Key 不单独回显，但若 Base URL 自身含 `?api_key=...`、token 或类似凭据参数，GET 响应会间接暴露凭据。配置保存现拒绝常见凭据查询参数名；读取 Inspector 返回值时也执行同一检查，避免不符合契约的提供方响应被返回。新增保存和读取领域用例，覆盖 `api_key`、`X-Access-Token` 与 `token`。因当前环境尚未具备 Go/Docker 联调条件，用例未运行；未改迁移、未提交或推送。

## Stage 145：限制 F4/F12 私有附件读取与残片（2026-10-25）

F4 资质存储此前通过 `os.ReadFile` 读取落盘内容，没有按已定义的 2 MiB 文件上限限制本地已存 blob，且不拒绝符号链接；F12 已有大小限制但打开前未检查路径对象。现将两种附件存储统一使用受限读取器：Lstat 确认为普通文件、打开后以 `os.SameFile` 核对对象一致、按 blob/manifest 各自的上限读取，防止超大文件占用内存和路径替换。资质保存遇 blob 或 manifest 部分落盘失败时也清理残片。补了超限文件与符号链接用例；无 Go/gofmt 且 Docker 维护中，未运行 Go 或 Docker 验证。`git diff --check` 和 touched-file trailing-whitespace 检查没有发现空白错误（Git 提示 SUMMARY.md 的 LF 将来可能转换为 CRLF）。未改迁移、未提交或推送。

## Stage 146：资质上传在落盘前预检归属（2026-10-25）

资质文件原流程先写私有目录，再在插入资质记录的事务中校验当前采购人；失败时虽会删除文件，但未授权请求仍能短暂写入附件目录。现增加上传前的归属与员工状态预检，预检不锁主体行；最终资质记录事务仍锁定主体并再次校验，避免预检后发生归属变化时越权。新增“预检拒绝时 blob Save 调用为零”的领域用例。Go/gofmt 不可用且 Docker 维护中，本轮未运行后端测试、数据库、Docker、API或Playwright验证；未改迁移、未提交或推送。

## Stage 147：清理 F4 资质文件的崩溃孤儿（2026-10-25）

资质附件在数据库幂等事务最终提交前已写入本地私有目录；若进程在文件写入后崩溃，事务回滚不会触发同步删除。现文件适配可按 24 小时宽限列出旧的合法 opaque `.blob`/`.json` 引用（包括只写完 blob 的崩溃残片）；运行装配对每个候选查询 `supplier_qualification.file_ref`，确认未引用后才删除文件。任何数据库错误停止清理并记录日志，已引用附件不会删除。新增枚举用例覆盖新文件排除、旧文件和 blob-only 孤儿候选。Go/gofmt 不可用且 Docker 维护中，未运行后端测试或数据库验收；未改迁移、未提交或推送。

## Stage 148：Lovable 原型连接故障复核（2026-10-25）

使用 agent-browser 0.27 对用户指定预览执行 `open` 和 `snapshot` 均返回 `CDP response channel closed`；`doctor --offline --quick` 发现 Chrome 已安装、对应守护进程 PID 存活，但没有可用的 CDP 响应通道。直接绑定现有 in-app preview 后，读取 DOM 在 20 秒超时，工具明确报告自动化 kernel reset；浏览器 inventory 仍能看到 `Token Operations Demo | Preview` 标签标题。故障发生在浏览器自动化/DevTools连接层，未观察到 Codex 桌面应用退出；预览管理页面的 DOM/交互仍未核实。本次未运行 Docker、项目测试、数据库或业务 API，也未改业务代码。

## Stage 149：F4/F12 前端类型检查（2026-10-25）

运行 `npm.cmd run typecheck`（`vue-tsc --noEmit`），当前前端源码类型检查通过。该结果只说明 TypeScript/Vue 类型一致；没有运行单元测试、浏览器脚本、API、数据库或 Docker 联调，也不代表 F4/F12 页面链路已测通。

## Stage 150：F4 资质上传预授权 HTTP 回归用例（2026-10-25）

静态核对新增上传预授权接口后，发现 API 测试中的资质存储 fake 尚未实现该能力，成功上传 fixture 会因 fail-closed 被拒绝。现补齐 fake 的采购归属检查，并增加非归属采购人 HTTP 上传用例，要求返回403、不得创建业务记录且不得调用 blob 存储；成功上传用例同时断言预授权被调用。Docker维护中且本机无 Go，本轮没有运行后端测试或格式化；未运行 Docker、数据库、API或Playwright，未改迁移、未提交或推送。

## Stage 151：F12 readiness 前端查询契约（2026-10-25）

后端已有 readiness 查询入口，但前端无对应请求方法和响应边界校验。新增 `f12SubjectRecordsApi.readiness` 与 `f12Readiness.contract.ts`，严格校验主体/流程标识、服务端 revision、资料要求项、合同/授信版本引用及 `ready`/缺失原因的一致性；不把请求里的上下文当作权限事实。共用面板收到流程上下文后显示齐备状态、缺项和版本引用，并在服务端 revision 与当前办理上下文不一致时请求刷新。面板增加 UI-only 的 `flowCanManage` 提示输入，所有写操作仍由服务端重新授权；异步回调校验主体及流程上下文，切换 subject/flow/module 时清理旧草稿，避免旧响应覆写新页面。新增纯前端定向用例：正常齐备、币种级缺项、主体不匹配和矛盾投影拒绝，3项通过；`npm.cmd run typecheck` 通过。当前供应商和客户页面尚未给共用面板传入可信 `flowContext` 或当前办理人资格，故页面实际尚不显示流程 readiness/办理入口，也不代表真实 API 联调完成。未运行 Go、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 152：拒绝绕过资质元数据必填（2026-10-25）

资质上传接口先前通过“类型/有效期至少一项非空”区分新旧请求，显式提交空字段会回退基础上传，允许绕开资质类型必填。现改用 JSON 字段存在性选择路径；空字符串由领域校验拒绝，`null`/非文本值由 API 按 400 拒绝，均在写附件前结束。PostgreSQL 写事务也检查资质元数据六列：全量结构齐备后，完全省略元数据的旧请求返回 `ErrQualificationMetadataRequired`/400；旧库或部分结构仍兼容基础上传。新增 HTTP 回归用例覆盖显式空字段和 null，原有省略字段用例保留旧结构兼容覆盖。当前没有 Go/gofmt，未运行后端用例、数据库、Docker、API 或 Playwright；代码格式和事务结构仅做静态核对，未改迁移、未提交或推送。

## Stage 153：F12 归档合同重复修订返回冲突（2026-10-25）

领域层已用 `ErrF12ContractArchived` 阻止归档合同继续追加内容版本，但公共 HTTP 错误映射未将该状态错误归为 409，客户端会看到通用 500。现将此领域错误加入现有 409 冲突映射，并添加归档合同修订 API 用例。没有 Go/gofmt，本轮未运行该回归用例或其他后端验证；未运行 Docker、数据库或 Playwright，未改迁移、未提交或推送。

## Stage 154：F12 流程权限优先于主体归属权限（2026-10-25）

F12 面板原先将普通主体维护权限与流程办理权限做 OR 合并，导致主体归属人即使不是当前流程办理人，也会看到流程写入和附件下载入口。现将按钮权限抽为纯函数：无流程上下文时使用主体维护权限；存在流程上下文时只接受明确的 `flowCanManage === true`。定向用例先确认原错误（目标反例失败），修正后3项全部通过；`npm.cmd run typecheck` 通过。真实页面/API越权仍由服务端授权保护，完整页面权限矩阵待环境恢复后验收。本轮未运行 Docker、数据库、API、Go、构建或 Playwright，未改迁移、未提交或推送。

## Stage 155：F4 曾用名结构能力明确投影（2026-10-25）

别名业务代码已先行，但正式 `supplier_alias` 表尚未由结构维护人交付。后端主体列表、详情和保存回执现统一返回 `aliases_supported`，五个必需列须有兼容类型、非空及所需默认值后才报支持；能力探测错误不会伪装成结构缺失。前端响应契约要求页面信封与每条主体的能力值一致；结构缺失或不完整时搜索框去掉曾用名承诺，列表/档案表单说明名称修改暂不可用并锁定名称输入，其余主体资料维护仍可继续。创建新主体仍可用并提示后续改名依赖结构交付。

定向 Node 单测 8 项全部通过，`npm.cmd run typecheck` 通过。后端能力探测改为要求五个实际列齐备，并新增旧适配器 fail-closed、能力提供方错误传播、无表兼容以及隔离 schema 中“列不完整/列齐备”两种能力断言；本机无 Go，新增 Go 用例未运行。`git diff --check` 通过（SUMMARY 现有 LF/CRLF 提示）。Docker 维护中，未运行 Go 编译/API、数据库、Docker 或 Playwright，未执行迁移、未提交或推送。曾用名正式搜索和修改仍待增量结构交付后的联调验收。

## Stage 156：F4 资质元数据结构能力检查（2026-10-25）

资质接口此前仅计数六个元数据列；列名齐备但日期/revision/创建人类型不兼容时，页面仍可能显示“结构已交付”，后续查询或写入再失败。现把能力探测收紧为校验实际 SQL 所需类型与空值语义：日期列为可空 `date`、revision 为非空 `integer/bigint`、创建人为可空 `bigint`，资质类型与审核状态为文本且允许历史空值。探测不兼容时继续使用旧列表/基础上传路径并显式返回 `metadata_supported=false`，页面提示“结构缺失或不兼容”；元数据写入口仍在事务中重新检查。

新增隔离 schema 用例覆盖类型不兼容与结构兼容两种情况；资质页面提示已改为“结构缺失或不兼容”。`npm.cmd run typecheck` 通过。Go 用例标记为待运行；本机无 Go 且 Docker 维护中，未运行 Go、数据库、Docker、API 或 Playwright，未改迁移、未提交或推送。

## Stage 157：F12 附件上传预授权与事务内复核（2026-10-25）

F12附件上传此前先写文件，再做唯一一次授权。现增加短事务预授权：先锁定主体并验证认证员工、责任归属或真实流程办理权限，提交释放主体锁后才写文件；随后在实际资料绑定的共享幂等事务内再次锁定主体并重新授权，避免预授权与写入之间的权限/办理状态变化导致越权绑定。授权失败时不会先落盘；文件保存后若事务复核或业务写失败，仍执行尽力删除并由延迟清扫处理进程中断孤儿。文件 I/O 仍不占用最终业务事务的主体行锁。

新增顺序用例覆盖“预授权→写文件→事务内复核”及预授权失败不落盘。`F12FlowWriteAuthorizer` 未装配或 F12 结构不可用时，在保存文件前按 fail-closed 返回。Go用例未运行（本机无 Go，Docker 维护中）；未运行数据库、Docker、API 或 Playwright，未改迁移、未提交或推送。

## Stage 158：F12 增量结构类型能力检查（2026-10-25）

此前F12附件能力只检查增量列名是否存在；若列名齐全但 `source_flow_revision` 或 `created_by` 类型错误，附件可能已保存后才在资料写入失败。现统一增加F12结构探测，要求合同/授信增量列实际为文本类、流程revision为 `integer/bigint`、操作人ID为 `bigint`。F12写事务在执行领域操作前于当前事务检查；附件上传在落盘前检查，并在主事务再次检查。缺列/不兼容类型返回依赖不可用，不尝试迁移，不产生成功回执。数据库类型错误SQLSTATE映射为依赖不可用。

新增纯逻辑测试覆盖完整、缺列和错误revision类型；新增隔离schema数据库用例覆盖列类型错误被拒及兼容结构被接受。Go及数据库用例待Docker恢复后运行；本轮未运行Go、数据库、Docker、API或Playwright，未改迁移、未提交或推送。

## Stage 159：F12 增量字段可空性保护（2026-10-25）

继续按结构申请核对 F12 兼容约束：新增字段需兼容已有历史行，流程来源字段也必须允许普通维护保存 NULL。现有 Stage 158 的类型能力探测补充 `is_nullable` 检查，所有增量字段必须可空；若结构负责人把历史兼容字段设为 NOT NULL，写入和附件能力均按依赖未就绪处理，不会在普通业务请求中暴露数据库错误或生成无主附件。

新增纯逻辑用例覆盖 `source_flow_revision NOT NULL` 拒绝；隔离 schema 用例覆盖兼容列齐全后再将历史 `created_by` 改为 NOT NULL 时被拒。Go/数据库用例待 Docker 恢复后运行；本轮未运行 Go、数据库、Docker、API 或 Playwright，未改迁移、未提交或推送。

## Stage 160：核实 F12 首次办理事实的现有结构边界（2026-10-25）

静态核对仓库迁移后确认：000033 的 `onboarding_flow` 只有 `status/current_node/created_at/updated_at`，`onboarding_node` 没有 revision；它们没有可直接用于 CAS 的流程版本，A 不以 `updated_at` 合成版本。000002 的 `customer_profile` 与 000033 的 `customer` 未定义映射列/外键，但 000033 的 `onboarding_flow.owner_id` 注释将客户 owner 指向 `customer.id`；A 的 F12 主体身份是 `customer_profile.id`。因此首次办理写授权/readiness 仍需 B/C 提供真实 revision、资料要求模板及事务内可验证的客户主体映射；代码保持 fail-closed，不按相同数字 ID 关联，也不把静态迁移当成运行库已升级的证据。

已将这些明确缺口同步至 F12 结构申请与实施计划；本轮只读源码/文档，没有改迁移、数据库、容器、业务代码，也未运行测试或提交推送。

## Stage 161：F12 明确依赖失败后的幂等重试（2026-10-25）

F12 保存或附件上传收到明确的 HTTP 503 / 错误码 10000（服务端将该请求记录为 FAILED）时，前端此前仍保留同一幂等键；再次点击会被中间件按 FAILED 状态拒绝，形成无法重试的表单。现只对该明确终态响应清除本地幂等键，使相同内容的下一次提交生成新键；网络断开/超时等结果不明场景仍复用原键，以保持安全重放。由于后端默认业务键按操作者、请求体和路径计算，已完成的请求仍会重放原结果。

新增 F12 API 请求层回归场景，并将资源接入的 503 用例同步改为验证新键；`npm.cmd run typecheck` 通过，`npm.cmd test` 138 项全部通过。此为本机前端/MWS 测试，不代表真实 API、Go、数据库或 Docker 联调；本轮未运行 Go、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 162：F12 写单元在共享幂等事务内保存点回滚（2026-10-25）

公共幂等中间件会在业务返回错误后持久化 `FAILED` 状态；F12 事务适配此前直接借用该外层 SQL 事务，若领域操作已写入部分数据后遇到非 SQL 中止类错误，回调错误本身不会自动撤销已完成的写入。现 `F12RecordStore.Within` 为每个领域单元开启唯一保存点：成功时释放；任意业务错误时回滚到保存点，保留外层幂等事务用于记录失败结果；保存点清理自身失败时关闭外层事务，避免提交部分业务数据。

新增隔离数据库用例，在保存点内先写入探针行再返回应用层错误，要求外层事务内查不到该行且仍可继续查询。用例以 `SUPPLY_F12_SAVEPOINT_ISOLATED_DSN` 和 `model_bss_a_` 数据库名前缀保护，待 Docker 恢复后运行；本轮未运行 Go、数据库、Docker、API 或 Playwright，未改迁移、未提交或推送。

## Stage 163：Lovable 最新原型只读检查仍不可用（2026-10-25）

为核对 F4 资质和 F12 双主体资料面板的最新字段与交互，检查当前内嵌 Lovable 标签；标签仍显示“Loading...”，CUA 绑定 tab ID 和 providerTabId 分别约39秒、34秒超时。改用独立 Playwright CLI 只读打开用户提供的预览地址，命令返回 `Error: Session closed`，没有取得页面 DOM 或交互快照。故本轮未确认最新资质/F12 UI 是否与源码一致，也未据此改动业务行为；不得将其记为原型对齐或 Playwright 联调通过。未登录、未提交表单、未运行 Docker/API/数据库测试，未改迁移、未提交或推送。

## Stage 164：F4 资质附件下载禁止缓存（2026-10-25）

内部资质附件可能包含供应商证明资料；下载响应现增加 `Cache-Control: no-store`，避免浏览器或中间缓存保留文件副本，并保留原有 `nosniff` 与下载文件名处理。现有 HTTP 用例增加响应头断言；Go用例未运行（Docker维护中、本机无Go）。本轮未运行 Docker、数据库、API真实联调或Playwright，未改迁移、未提交或推送。

## Stage 165：F4 资质维护输入错误分类（2026-10-25）

`UpdateMetadata` 此前将非法操作者、供应商/资质 ID 和 revision 与缺失依赖统一返回 `ErrDependencyUnavailable`，会把可修复的请求错误映射成 503。现将缺失 service/store 保持为依赖不可用；无效 ID 和 0/越界 revision 返回对应字段错误，且在调用持久化前拒绝。新增领域用例覆盖五类输入并断言没有触发存储写入。用例未运行（遵守 Docker 维护期间暂停后端验证）；本轮只做源码审阅与 `git diff --check`，未运行 Go、数据库、Docker、API 或 Playwright，未改迁移、未提交或推送。

## Stage 166：F12 切换主体时清空旧页面数据（2026-10-25）

供应商/客户共用 F12 面板切换主体时，此前只有请求完成后才替换合同、授信投影；若新主体读取失败，旧主体数据仍留在页面。现于主体类型或 ID 变化时同步清空合同历史、授信历史、当前额度及分页计数，再加载新主体，避免展示跨主体残留。`npm.cmd run typecheck` 和 `git diff --check` 通过；Playwright 场景待 Docker 恢复后验证，未运行真实 API、数据库、Docker 或浏览器测试，未改迁移、未提交或推送。

## Stage 167：F4 资质存储适配器输入错误分类（2026-10-25）

资质 metadata 领域服务已将无效 ID/revision 分类为字段错误；PostgreSQL 适配器入口仍将相同请求问题返回依赖不可用。现对缺失 store/resolver/log 继续 fail-closed，对非法 actor、supplier、qualification ID 和 0/越界 revision 返回字段错误；新增定向用例断言 SQL resolver 未被调用。Go用例未运行（Docker维护中），仅源码审阅；未运行后端测试、数据库、Docker 或 Playwright，未改迁移、未提交或推送。

## Stage 168：F4 资质附件下载校验采购归属（2026-10-25）

资质文件 GET 此前只要求内部认证，任一内部员工都可能下载任一供应商附件。现在把认证员工 ID 从 HTTP 传至领域服务和 PostgreSQL 读取事务，在同一只读快照内校验员工有效状态、供应商归属采购人及资质记录后才返回 opaque 文件引用；已冻结供应商的归属采购人仍可读取历史附件，写操作继续要求供应商处于 ACTIVE。资质列表对非归属采购人不返回可下载标志/文件名，前端隐藏下载按钮并显示归属提示。

新增 HTTP 用例覆盖非归属采购人下载返回403且不打开 blob、列表不暴露下载信息；既有成功下载与旧物理路径拒绝用例同步适配新签名。`npm.cmd run typecheck` 与 `git diff --check` 通过。Go/gofmt 不可用且 Docker 维护中，本轮未运行后端用例或数据库/API/浏览器联调；未改迁移、未提交或推送。

## Stage 169：F12 授信写回执核对方向与币种（2026-10-25）

HTTP 写处理器原先只检查授信回执主体、版本、操作人、非空金额和附件 ID；若写提供方错误返回其他方向/币种，API 仍会回传成功。现在回执还必须与本次请求方向及币种一致，否则按读取契约错误失败关闭；补充方向与币种各一条回归用例。Go/gofmt 不可用且 Docker 维护中，用例未运行；本轮未运行数据库、Docker、API或Playwright，未改迁移、未提交或推送。

## Stage 170：F12 普通维护授权的流程创建并发边界（2026-10-25）

只读核对 `supply_chain.go` 的供应商首次办理创建事务：它先锁 `submission` 行，再插入 `onboarding_flow`，没有先锁供应商主体；F12 写入则先对 supplier 行加锁，再调用 `AuthorizeF12OrdinaryWrite`。两条事务没有共享互斥，因此 A 若仅查询“没有 PROCESSING onboarding_flow”后放行普通合同/授信写入，会与并发流程创建竞态，不能作为可靠授权。当前 `newSupplyRuntimeDeps` 也尚未装配 `F12FlowWriteAuthorizer` / `F12BusinessContextProvider`，F12 写入和 readiness 仍 fail-closed。将具体锁序要求补入 `public-integration-request.md` 与实施计划：B需让首次流程创建和A普通维护共用主体锁或提供等价的同事务串行化事实；C另需提供 customer_profile 到流程 customer 的权威映射及一致锁校验。仓库迁移止于000037，F12扩展列仍由统一迁移维护人审核交付。此为静态源码/文档审计，不是测试结果；没有修改业务源码、迁移、数据库或容器，也没有运行Go、前端、Docker或Playwright测试，未提交或推送。

## Stage 171：F12 写能力要求业务版本唯一索引（2026-10-25）

结构申请要求合同版本唯一于 `(contract_key,version)`，授信版本唯一于 `(subject_type,subject_id,direction,currency,version)`。此前能力探测只确认增量列类型/可空性，迁移若漏建唯一索引仍会开放写入口。现 F12 写事务及附件上传/下载共用的结构探测，还检查 PostgreSQL 中对应表的有效唯一索引和准确键列；合同接受全量唯一或 `contract_key IS NOT NULL` 部分唯一，授信不接受限制性部分索引。新增纯逻辑用例及隔离schema用例，覆盖缺失、非唯一及授信限制性部分索引拒绝；尚未运行。因本机没有 Go/gofmt 且 Docker 维护中，未运行编译、Go测试或数据库；未改迁移、业务数据、容器，未提交或推送。

## Stage 172：F12 HTTP 写回执使用规范化命令（2026-10-25）

F12 领域校验会 trim 资料字段并把授信金额标准化为两位小数；HTTP handler 先前却把领域回执与原始请求比较。若字段带首尾空格，领域事务可能已提交，HTTP 随后误报500。现合同新建/修订和授信新增在调用 writer 前运行相同领域校验，以规范化后的命令执行并校验回执，确保拒绝输入在写前返回400、合法空格输入的回执与实际写入一致。补充 HTTP 回归用例模拟领域回执规范化，覆盖合同与授信文本字段及授信金额格式。由于本机没有 Go/gofmt 且 Docker 维护中，用例未运行；未运行数据库、Docker、真实 API 或 Playwright，未改迁移、未提交或推送。

## Stage 173：最新 Lovable 原型只读访问复核（2026-10-25）

再次尝试从当前 Codex 内嵌浏览器和 Playwright CLI 读取用户指定的 Lovable 预览。内嵌标签仍为 `Loading...` 且绑定超时；Playwright CLI 无法附加当前 Edge（未启用远程调试，缺少 `DevToolsActivePort`），独立启动 Edge 会话随后关闭，CLI 会话列表为空。本次仍未取得原型 DOM 或截图，因此供应商管理列表/详情、资质和共享测试配置，以及 F12 合同/授信编辑面板的最新字段与交互尚未核对；不宣称已对齐原型。浏览器访问失败不影响其他 A 侧源码开发。本次未运行 Docker、测试或数据库操作，未改迁移、未提交或推送。

## Stage 174：F4档案/资质与F12面板快速收尾（2026-10-25）

本轮复用现有页面与API，集中补明确交互缺口：

- 供应商主体表单保存及冲突刷新期间锁定字段和关闭，保存前复用既有请求校验；冲突未刷新不能重复保存。税率回填保留实际小数，不通过四舍五入改写服务器值。主体档案列表取消旧查询、只接收最新结果；曾用名能力未读到时不误报缺结构。供应商列表进入详情/档案后返回，保留筛选、页码、每页数量和排序；列表总数缩减导致空页时回到有效页。
- 资质维护409后保留名称/类型/日期/说明，提供显式刷新最新修订；刷新同时重新读取主体权限与资质列表，显示最新资料摘要并保留草稿，核对后才允许使用新revision重试。当前页找不到目标、权限撤回或结构不可用时不放行重试；没有新增审核枚举或审核流程。
- F12合同表增加当前/历史版本及保存时间，授信表单和历史明确方向；新增纯函数按现有后端decimal(20,2)规则校验额度和币种，并在上传文件之前拒绝非法输入。保存/冲突刷新期间锁定表单、文件和关闭入口；读取失败时阻止基于未知版本写入。成功写入后发送`changed`，供应商主体页/弹窗及客户宿主刷新摘要；客户改动限于F12接入。页面说明去除数据库版本/部署内部细节。
- 结构、真实授权、Inspector、附件部署及组件参数集中到`public-integration-request.md`的F4/F12交接表；同步实施计划、supply索引及验收清单，保留未交付与未验状态。

本机`npm.cmd run typecheck`通过；`node --experimental-strip-types --test tests/f12-subject-records.test.ts`定向5/5通过，覆盖新增额度边界与既有双主体/流程来源投影。首次新增用例发现decimal.js的`isPositive()`把正零视为非负号，已改为显式`greaterThan(0)`后定向通过。此证据仅证明前端类型与纯函数规则，不证明页面保存/附件/数据库链路。资质409真实重试、导航/查询竞态、双主体写入及附件下载待Docker恢复并交付结构和真实提供方后验证。最新原型视觉与字段核对仍未完成；本轮未调用Docker、后端测试、数据库或Playwright，未部署、未改迁移、未提交或推送。

## Stage 175：供应商完整日志与合同跨页冲突刷新（2026-10-25）

供应商操作记录源码扩展为主体、资质、正式配置、合同、授信、资源、计划七类。SQL按业务表确认供应商归属并使用SUPPLIER主体范围排除客户同数字ID记录，资源为空的主体仍可显示档案日志；同一快照内统计/分页，时间及数字日志ID稳定倒序。正式配置/计划/资源控制日志转换为业务动作摘要，不读取或返回before/after正文。接口支持可选entity_type白名单筛选；页面增加分类、安全摘要详情和关联资料区域导航，正式配置仅提供摘要及现有维护入口指引。模型端仍只读资源/计划，未新增通用日志工具或迁移。

F12合同查询源码增加contract_key及latest_only，按主体类型/ID/档案的最大version精确查最新，防止当前20条历史之外的版本使冲突重试失效。重复最高版本返回409、档案列缺失返回503；兼容旧列表读取。修订与归档刷新均使用新查询；修订显示服务器最新名称/编号/版本/附件状态供核对，草稿及已上传附件保留。不选新附件时沿用刷新后的同档案附件。流程上下文更新还须完成合同资料刷新才能放行重试，不以新流程revision代替合同CAS。

本机`npm.cmd run typecheck`通过；`node --experimental-strip-types --test tests/source-operations.test.ts tests/f12-subject-records.test.ts`定向9/9通过，覆盖非资源日志投影/跨供应商及模型范围隔离、关联导航、合同主体/档案错配和重复最高版本拒绝。新增/调整的Go领域、HTTP、SQL临时表用例均未运行；临时表用例覆盖七类日志、无资源主体、客户排除、合同目标位于可见页之外、重复最新版本及缺档案列。真实接口、SQL执行和浏览器交互仍未验证；当前原型视觉仍待核对。本轮未调用Docker、后端编译/测试或数据库，未部署、未改业务迁移、未提交或推送。

## Stage 176：标准模型库 / 模型广场对齐本地v0.22（2026-10-26）

按用户本轮指定的本地HTML原型，实际读取并截图核对标准模型库、模型广场和供应来源。差异及实现边界见`supply/2026-10-26-model-pages-v022-alignment.md`。标准模型库增加全局/逐模型抓取状态区，F2无真实接口时明确未接入并禁用更新，不产生模拟事实；资料顺序、历史核验状态和日期格式对齐。维护仍保留单一保存核验、资料待补齐保存、权限、冲突输入保护及历史快照。

广场改为从已有资源接口完整分页读取，按当前筛选范围聚合，不再拼接标准模型前100条或混入无来源模型。支持供应商/公司/类型/节点多选，搜索包含供应商及接口模型ID；有可用/待接入/无可用三类状态与实际可报价条数独立。详情按ID直接读取模型；来源多选及四类数值上下限复用既有sourceFilters，排除缺失与旧目标指标，Decimal排序且空值末尾。缓存及指标复用既有格式化函数。报告直接打开独立测试列表，继续使用原单次报告、文件授权和资源操作流程。模型→供应商接入模型→返回模型→返回广场保留资源/区域及两级筛选、排序、分页。

定向验证：

- 新增规则用例先因目标模块缺失失败，随后通过；最终执行`node --experimental-strip-types --test tests/model-plaza-prototype.test.ts tests/resource-contract.test.ts tests/standard-model-contract.test.ts tests/model-filters.test.ts`，16/16通过。
- `node tests/standard-model.browser.mjs`通过：单一维护入口、真实0价格、冲突保留输入、单次保存核验、历史只读、操作记录、非维护人只读、待补齐保存、窄屏。
- `node tests/model-plaza-supplier-detail.browser.mjs`通过：广场→模型→供应商四页签→往返、模型/资源/区域上下文与两级筛选；补齐现有供应商页新增的资质/测试配置夹具及日志提示断言，没有修改这些业务能力。
- `node tests/model-plaza-prototype.browser.mjs`通过：104条资源跨两页读取、多选并集、供应商搜索、待接入/已下架、100条之后模型直接读取、真实0/缺失/旧目标指标、区间过滤、空值末尾、独立测试列表与单次详情、返回筛选、390px页面无整体横向溢出。已查看使用产品样式的桌面/手机截图。
- `npm run build`通过，包含vue-tsc；Vite提示现有入口产物大于500kB，未引入新依赖或调整公共打包配置。`git diff --check`通过。

以上浏览器测试使用真实Vue组件和接口夹具；未运行真实后端/数据库/Inspector联调，未部署、未改业务迁移、未提交或推送。前端多选聚合读取完整资源分页，大规模性能及未来服务端多选聚合下沉属于后续优化边界。

补充：广场读取失败与筛选校验分别保留，输入关键词/重置不会把读取失败伪装成空结果。浏览器用例已覆盖503后继续筛选、错误提示保留、显式重试成功。

## Stage 177：标准模型库两页签按原型复核修正（2026-10-26）

用户指出Stage176后标准模型库的“标准模型”和“操作记录”仍与原型有差异。本轮再次在Edge中读取本地v0.22的两个页签及日志详情，取得列头、布局、列宽、筛选方式及截图，不把先前功能验证当作视觉对齐证明。

- 标准模型页：抓取状态条移到参数表卡片之外；筛选栏改为原型的模型公司/模型类型/部署节点三项可搜索多选，下拉内勾选，选择即查询，关键词防抖查询及重置；取消上一版额外的查询/更多筛选/排序工具栏。参数表使用随容器宽度的固定布局、原型九列比例、紧凑行距、交替背景与固定首列，窄屏仅表格容器横向滚动。类型用小标签，容量/API/生命周期参数按业务名称行内展示，能力用支持/不支持/未核实/未披露状态，价格去掉展示尾零，按币种/单位数量/条件/维度分组；金额仍保持原始decimal字符串，真实0、极小价格、特殊单位和不同条件不混合。不在名称列堆叠核验标签或放跳转链接，维护仍为末列唯一入口。
- 标准模型和日志列表均读取完整匹配分页后展示，原型没有分页操作栏；读取中固定筛选快照，发现总数变化、重复ID或缺页则提示重试。模型核验、报价门槛、保存及版本业务接口不变；未接入F2时抓取栏仍显示真实未接入，不生成模拟抓取事实。
- 操作记录不再复用通用资源表，单独显示“操作、对象、操作人、时间、详情”五列，中文动作、上海时区秒级时间、操作/人员/时间列头排序、“共N条”及查看弹窗。对象从已加载模型名称或按ID读取获得，读取失败保留模型ID，记录不会丢失。名称属于当前模型名称，不冒充历史名称快照。详情只显示实际已有的操作人/时间/对象/操作说明/记录编号；现有工作台日志接口没有before/after快照，页面明确没有该明细，未伪造原型JSON快照，也未透传凭据或provider原始数据。

新增`standard-model-presentation.test.ts`先因目标模块不存在而失败，完成实现后3/3通过；与标准模型契约、筛选契约共同定向执行10/10通过。更新`standard-model.browser.mjs`以真实Vue组件及产品样式验证三项勾选自动查询、精简表格、101条模型跨两页完整加载、101条日志跨页完整加载、五列及中文动作、对象名、排序、详情弹窗、手机容器滚动；继续覆盖单次保存核验、真实0价格、冲突输入保留、只读权限、待补齐保存、历史版本及模型内日志。桌面内容区1210px、手机390px截图保存至本机忽略目录`.qa/`。

本轮没有修改通用资源日志组件、公共路由、后端或数据库结构，没有部署或提交推送。浏览器使用接口夹具，不能据此宣称真实数据库/后端联调通过。前端构建包含类型检查，构建产物仍有既有大包提示。

Stage177最终验证：`npm run build`通过（含vue-tsc）；`node --experimental-strip-types --test tests/standard-model-presentation.test.ts tests/standard-model-contract.test.ts tests/model-filters.test.ts`共10/10通过；更新后的`node tests/standard-model.browser.mjs`通过，包含模型101条完整读取及101条日志、1210px/390px检查。已实际查看最终标准模型、记录列表及手机截图；`git diff --check`通过。原型与实际业务数据条数/参数取值不同，不复制原型虚构模型、抓取状态或历史记录。

## Stage 178：供应商管理真实页面/API/隔离库定向联调（2026-10-26）

在专用 PostgreSQL `bss_a_tdd`（迁移 `37 / dirty=false`）和只读挂载当前工作区源码的临时 API 上完成定向联调；没有改日常运行库、迁移或正式 API。

- 修正供应商资质单条写入响应误走分页结构校验的问题：上传接口此前虽成功，前端仍因单条响应缺少分页级 `metadata_supported` 而报错，导致弹窗不关闭且列表不刷新。新增单条资质行规范化和回归用例；用例先失败后通过。PDF上传、API/数据库重载、列表展示及字节一致下载现已在真实隔离链路通过。
- 真实登录下通过供应商新增/编辑/税率保存与重载；非归属采购人可读但不能编辑，直接档案写和资质上传返回403；下载PDF且安全元数据不含存储路径或file_ref。
- 通过供应商详情第2页上下文往返、真实合同状态筛选、“未登记”展示，以及供应商操作记录非空查询/分类/安全摘要详情/关联导航。真实日志数据覆盖档案CREATE/UPDATE及资质UPLOAD；另外五类（正式配置、合同、授信、资源、计划）因隔离库无真实对应记录，未声称通过。
- 前端定向契约用例与`npm.cmd run typecheck`通过；资质HTTP/领域/SQL等Go定向用例在Docker Go容器通过；Playwright真实浏览器脚本通过。agent-browser doctor可用，但open/snapshot遇到`CDP response channel closed`，本轮浏览器证据由Playwright提供。
- 供应商级Inspector测试配置接口真实GET返回503，确认主装配尚无真实`SupplierTestConfigProvider`。正式配置/绑定/准入仍因无资源及B侧真实流程/报告事实而未完成真实页面闭环；F12合同授信也未在本轮用真实增量结构和附件提供方联调。

本轮临时API/隔离数据库按当前A联调目标保留，未部署到8080日常API；未执行全量回归、迁移、提交或推送。证据文件保存在项目忽略目录`.qa/`，不纳入Git。

## Stage 179：标准模型“维护”弹窗对齐v0.22（2026-10-26）

用户继续指出点击维护后的内容与原型不一致。本轮在Edge实际打开本地v0.22标准模型维护弹窗，读取字段、价格表和尺寸并截图，确认其为880px单页、五项多行资料、六列价格表。将原来的顶部分页签、状态横栏和大量独立标识/状态行改为原型主布局：厂商/类型与名称/官方ID两列，输入输出、容量、能力、API、生命周期五个多行框，价格表（计费项/官方单价/币种/单位/适用条件/移除），官方资料URL、核验日期、取消/保存。保存成功后关闭弹窗并刷新列表。保留后端要求的操作说明；历史版本、日志、补充来源与额外计费信息移到次级入口，不丢弃既有功能。

新增独立文本适配函数：无修改回填保留原字段键、状态及多行内容；编辑单行保留其他条目；新补充说明生成有效稳定标识，重复已知项和非法模态明确报错。能力支持/不支持/未披露/未核实独立处理，没有明确结论的说明不视为通过。价格金额始终字符串；主表的单位文本可转换既有代码与精确数量，不改未编辑的单位数量表示，价格来源、维度和任务保留。可在折叠区维护价格任务/来源/维度和其他官方来源。原有资料不全保存、后端单次原子核验、绑定权限、幂等、未保存离开保护、409输入保留及版本追溯继续使用。

验证：

- 新增`standard-model-editor.test.ts`先因模块不存在失败；最终与展示/请求/筛选契约共同执行15/15通过，覆盖无修改往返、未知状态、多行内容、同名字段区分、重复拒绝、模态错误、特殊及精确单位、价格维度/补充来源保留和原对象不变。
- 更新`standard-model.browser.mjs`通过，使用真实Vue组件和产品样式：880px单页/五资料框/价格表，编辑容量与能力、保留原字段键、真实0价格及8位小数、增添缓存价格项并正确传任务及单位、原价格来源保持；409后保留文本与价格，刷新核对后保存，成功关闭并刷新；历史版本/日志次级入口、只读/待补齐、两页签原验证及390px弹窗/表格容器滚动继续通过。已查看维护顶部、底部和手机截图。
- `npm run build`通过（含vue-tsc）；`git diff --check`通过。构建仍提示既有入口大包。同步更新已有`standard-model.live.browser.mjs`的字段、价格和保存后重新打开选择器，`node --check`通过；本轮未运行该真实联调脚本，不能将语法检查视为真实接口/数据库验收。

本轮只改A标准模型维护组件、价格编辑组件及相关适配和测试，没有修改后端、数据库、公共路由或资源操作记录。浏览器验证使用接口夹具；没有部署、提交或推送。截图与临时脚本保存在本机忽略目录`.qa/`。

## Stage 180：Docker恢复后的A侧真实联调补充（2026-10-26）

本轮重新核对分支、容器、数据库与源码挂载：当前 `work-A-fix` / `6c8255a`，工作区原有改动全部保留。隔离库 `bss_a_tdd` 为 `37 / dirty=false`；临时 API `18081/18082` 健康、RestartCount 为 0，均只读挂载本地当前后端源码。日常库 `model_bss` 为 `36 / dirty=false`；`model_bss_api_latest` 当前健康但 RestartCount 为 7，容器运行 `/app/server`（挂载 `bin/server-v36`），并非当前工作区源码。日常库/API未写入、未升级、未重部署。浏览器测试均用独立 Playwright/Vite 实例；5173当时未监听。

### 真实页面/API/隔离PG

- 标准模型真实页面回归：先保存资料不完整模型，重载仍显示缺失项且不可报价；完整资料通过当前单一“保存并留痕”操作后记录核验；再次改成不完整资料后资格失效。真实登录、Vue、API、v37隔离PG贯通，页面无错误；数据库核对3个版本和对应操作记录。调试阶段曾因响应等待器安装过早超时；将等待器移到保存点击前，原场景通过，不是产品缺陷。
- 供应商档案真实新增/编辑/税率重载通过；比例 `0.0625` 展示 `6.25%`。列表第2页筛选/排序进入详情并返回恢复上下文；合同“未登记”状态筛选通过。
- 资质真实上传、重载、授权下载通过，下载字节一致，响应未泄漏物理路径；Stage178修复的单条上传响应解析问题继续有效。
- `buyer_b` 读取另一采购人供应商/资质为只读；档案更新、资质上传及本轮补入脚本的正式配置新增均通过真实 API 返回403。被拒绝的正式配置请求没有落库（隔离库配置行仍为0）。
- 供应商操作记录真实非空分页、分类、安全摘要和关联导航通过；真实记录只覆盖主体CREATE/UPDATE及资质UPLOAD。正式配置、合同、授信、资源和计划五类没有真实业务记录，本轮不宣称这些上游写入流程已通过。
- 工作台公告：GET详情保持本人UNREAD；本人标记后刷新为READ；切换查看他人仍为UNREAD且只读；伪造他人阅读请求返回400。真实Vue/API/隔离PG通过，浏览器无页面错误；隔离数据仅有当前人员自己的阅读行。

### 定向 Go/SQL/前端检查

- API和领域：正式配置、工作台公告、供应商资质、合并预览、计划创建/修订/执行、内部改价计划及幂等反例定向用例通过。
- PostgreSQL专用测试库（名称受 `model_bss_a_schema34_` 安全前缀保护）：正式配置原生事务/日志/回滚、公告读写事务、标准模型写入/精确字段/并发、模型操作记录、计划维护/并发、配置绑定与停用竞争均通过。测试使用隔离事务和临时表；不代表真实B报告/办理提供方。
- `bss_a_tdd` 上 `TestSourceOperationsTemporaryPostgres` 只创建事务内临时表，验证七类日志、客户同数字ID排除、无资源供应商日志及安全投影，通过；没有写入持久业务行。
- 真请求不同请求体同幂等键拒绝、已提交BLOCKED 409重放、计划HTTP和内部改价HTTP定向用例通过。它们中的内存幂等测试只证明中间件行为；真实HTTP/PG同键409的既有Stage83证据单独保留，不扩大为异body真实PG证明。
- `npm.cmd run typecheck`、受影响浏览器脚本 `node --check` 及 `git diff --check` 通过。没有跑全量回归或整站构建。
- agent-browser仍无法建立CDP会话（`CDP response channel closed`），安装其浏览器运行时因代理 `127.0.0.1:12000` 当前拒绝连接而失败；本轮实际页面证据由Playwright提供，没有改代理设置。

### 未验与边界

供应商测试配置仍因主装配缺少真实Inspector提供方而GET 503；不伪造测试凭据或B报告。v37隔离库没有资源/计划/正式配置行，故未做真实资源绑定、接入、停用被引用配置、ONLINE准入或报价链路。资质类型/有效期/审核增量列、F12合同授信结构/附件持久提供方、B/C授权及readiness均未交付；真实双主体F12写入仍未验。工作台多条并列计划和公共待办的跨人员非空汇总仍未验。资质变更元数据、F12、Inspector与B/C依赖继续按交接表处理，不运行迁移、不写日常库。

本轮无业务代码缺陷需修复；新增正式配置403断言和公告Playwright验收脚本属于测试源码。没有提交、推送、拉取合并或改写历史。临时容器/隔离库及旧测试数据仍需在本轮A联调整体结束后清理；日常环境保持原状。

## Stage 181：A范围标准模型日志与广场查询配套后端（2026-10-26）

用户针对此前两项后端建议明确要求落实，并限定A负责范围。本轮仅补F1日志查询投影、F3资源查询及相关页面接线；不改C通用工作台日志、B办理/测试事实、公共主路由、主装配或迁移。

- F1：新增标准模型全局摘要、逐模型摘要及逐模型单条详情GET接口。查询既有operation_log，限定standard_model；日志/对象ID字符串化，保留大于2^53的精度，列表名称取当时after/before资料。详情解码已知模型结构后白名单投影before/after，未知字段不透传，旧格式或缺快照返回null，跨模型详情404。沿用ModelRepository已有装配与内部人员权限，不新建日志存储。
- F3：resources新增公司/类型/供应商/区域/状态JSON数组多选、系数/耗时/RPS/TPS精确闭区间，兼容原单选但拒绝同维度混用；新增admitted_at排序。SQL先收窄维度候选，基于同事务可信资源/报告投影判定区间，再聚合、排序及分页。0与缺失/过期明确区分。沿用后端现有可信事实装配机制，没有新增物化聚合/缓存或开展规模性能压测。
- 前端：标准模型日志切到A专用接口，历史名称不再查询当前资料替代，详情可读取安全前后快照；广场每次读取一个grouped页，来源读取一个flat页，移除页面对allRows的依赖。选项改用现有远程搜索接口，供应商往返保留来源筛选/排序/页码。

验证：先增加测试并看到缺少实现的RED结果，再实现。领域、SQL适配全包测试通过；A全部TestSupply定向HTTP、标准模型日志、资源筛选等通过。专用PostgreSQL库`model_bss_a_schema34_20261026_models_backend`中执行日志/资源SQL和HTTP用例通过，日志包含超安全整数ID、历史名称、白名单、归属、稳定分页，资源包含多选聚合、0/大数区间和上架时间空值末尾。数据库夹具使用事务内临时表，不写日常库。

前端15项契约测试通过；三个Edge浏览器脚本通过，覆盖新接口的大日志ID及历史详情、104条来源的服务端分组单页请求、多选JSON/零值区间传参、报告、失败重试、导航上下文、来源服务端分页/页码往返与手机滚动。生产构建含vue-tsc通过，仍有既有大包提示；真实标准模型联调脚本只更新接口匹配并做语法检查，本轮未执行该脚本。`git diff --check`通过。

扩大Go包回归时，API包既有`TestRouterSupplierPortalProfileNotGuardedByRequirePerm`失败：源码区块截取把内部客户路由中的RequirePerm包含进去；相关`router.go`及该测试没有本轮修改。服务装配可编译，但不能据此声称API全量回归通过。协议及限制记录于`docs/Work_A/supply/model-pages-backend-contract.md`。本轮没有迁移、日常API部署、提交或推送。

## Stage 182：工作台同资源多计划真实定位（2026-10-26）

使用当前源码临时Vite代理、真实员工认证、工作区Go API和专用`bss_a_tdd` v37隔离库，运行`model-pricing-web/tests/workbench-plan-navigation.live.browser.mjs`。脚本经真实API创建同一离线QA资源的ONLINE计划与PRICE_CHANGE计划，再从真实工作台逐项打开；确认投影保留各自原生`plan_id`和完整深链，详情页分别读取到正确计划/原因。另用错误区域打开同一资源/计划，真实资源API拒绝该区域，页面不显示计划详情或实际完成入口。资源仍为OFFLINE、`quote_eligible=false`、测试状态`NOT_TESTED`，浏览器无pageerror。该夹具只证明A工作台的多计划定位、上下文校验和读取，不证明B测试、资源准入、报价或跨人员共享待办。

验证后按唯一QA标记清理资源、计划及关联操作日志、事件和幂等记录，计数均为0；测试会话已退出，临时Vite服务已停止，临时配置已删除。日常`model_bss` v36及8080 API未写入。Playwright脚本语法检查和计划区域/资源匹配契约定向测试通过。尝试使用agent-browser时其CDP响应通道关闭，未能执行；Playwright真实页面/API/隔离PG联调成功。未执行迁移、提交或推送。

## Stage 183：供应商档案与资质日志真实页面读取（2026-10-26）

复用`bss_a_tdd`中已有明确标记的供应商25和资质UPLOAD记录，并为其余五类直接插入有唯一QA标记的配置、供应商合同、授信、资源和计划读取夹具；同数字ID另插入CUSTOMER合同/授信反例。使用真实内部登录、当前Vue页面、当前源码隔离API `18081`及v37隔离PG，运行`tests/supplier-operations.live.browser.mjs`：供应商“操作记录”页返回七类非空日志；逐类调用真实API筛选并断言分类纯度；供应商合同/授信各返回1条，排除同ID客户记录；API响应不含`before`、`after`、`api_key`、`api_key_enc`、`file_ref`或物理路径；档案与资质详情导航回正确业务区域。浏览器无pageerror，测试结束主动退出会话，测试脚本语法检查通过。

本阶段证明七类日志读取/分类/安全投影和供应商/客户同数字ID合同授信隔离的真实页面/API/PG链路。主体及资质记录来自隔离库既有QA业务；正式配置、合同、授信、资源和计划五类通过直接SQL准备标记夹具，仅证明下游日志查询与展示，不证明这些业务上游写入流程。所有夹具在finally中清理；六类表的QA标记残留计数均为0。没有日常库写入、迁移、提交或推送。

## Stage 184：标准模型库与模型广场真实前后端联调（2026-10-26）

按用户要求补做完整应用→真实后端→数据库链路。使用Playwright/Edge、真实员工/供应商登录、Vite代理、工作区Go API（本轮独立18083容器）和专用model_bss_a_schema34_20261026_models_backend。不拦截或伪造响应；只写专用测试数据，不动日常8080或其他会话API，无迁移。

两个真实浏览器脚本均通过。维护脚本完成页面新增→补齐核验→移除价格，三次写请求200；SQL核对模型9007199254741026三个版本状态INCOMPLETE/VERIFIED/INCOMPLETE、核验人空/1/空、价格0/2/1、三条MANUAL_SAVE。模型页面脚本通过七组检查：大ID/历史名称/安全快照和跨100条日志分页、25模型grouped页20+5及聚合排序、供应商/公司多选OR与跨维度AND、区域及接口ID搜索、25来源flat分页与0区间、真实独立报告/缓存0%及上架时间空值末尾、400/401/403/404、过期版本409不写新版本、第二名员工只读与写403、返回上下文。最终主场景44条观测请求无5xx，页面无pageerror。

发现并修复远程选项503：A运行/模块装配使用ResourceReadStore缺少选项能力，两处改为已有ResourceRepository；新增模块选项回归，重启本轮API后完整场景通过。未改公共主路由、C通用日志或B办理/报告逻辑。A领域/SQL/HTTP/装配定向回归及服务/夹具编译通过。

390px完整壳层公共布局仍溢出：侧栏190px，顶部工具栏右边界540px；A表格容器可横向滚动。按A边界未改公共布局，单独记录未解决。资源/报告为显式隔离测试数据，不证明B平台调用/申报/准入、C报价或规模性能；此前公共路由测试失败不在本轮修复范围。

可复跑生成器在model-pricing-backend/tests/modelpageslive/main.go，限制指定空白专用库与测试口令；证据JSON、SQL和截图在前端忽略目录.qa/model-pages-live/。详见[真实联调验收](./SUMMARY.md#2026-10-26模型页面真实联调历史记录归并)。无提交、推送或部署日常API。

## Stage 185：供应商档案 revision 冲突真实页面/API/隔离库验证（2026-10-26）

使用 Playwright、真实内部登录、当前 Vue 页面、只读挂载工作区源码的隔离 API `18081` 与专用 PostgreSQL `bss_a_tdd`（迁移 `37 / dirty=false`），运行 `model-pricing-web/tests/supplier-profile-conflict.live.browser.mjs`。脚本创建唯一标记的临时供应商，在页面打开维护表单并保留一份未提交草稿；随后通过真实认证 API 并发更新同一主体，将数据库 revision 从1推进到2。页面提交旧 revision 后真实返回409，草稿仍在且保存被阻止；用户显式载入最新资料后页面显示并提交 revision 2，保存成功返回200，数据库 revision 到3。浏览器无 pageerror。

该切片验证了“过期版本拒绝写入→保留用户输入→显式刷新→基于新版本重试”的供应商档案冲突恢复闭环，不代表资质元数据/F12/Inspector/B/C上游流程已联调。测试结束后脚本仅在精确匹配的隔离 PG 容器中删除本次唯一标记的供应商及关联日志/归属记录，并检查主体已清理；日常库 `model_bss`（36）和日常 API `8080` 未写入、未升级或重部署，无迁移。测试源码保留在 `model-pricing-web/tests/supplier-profile-conflict.live.browser.mjs`；`node --check` 与 `git diff --check` 通过。无提交或推送。

## Stage 186：正式配置停用与资源绑定 PostgreSQL 竞争验证（2026-10-26）

运行现有 `TestResourceBindingDisableContentionPostgres34`，针对当前 v37 隔离库 `bss_a_tdd` 做只读数据库副本，并在临时数据库 `model_bss_a_schema34_20261026_supplier_disable_stage186` 上执行；测试使用当前工作区源码、现有 Go 1.26 镜像和资源/配置原生 PostgreSQL 适配器。测试以两组真实并发事务及 `pg_blocking_pids` 确认两操作共用供应商锁：绑定先完成时，随后停用返回 `ErrConfigInUse`，资源保留绑定且配置仍为 `ACTIVE`；停用先完成时，随后绑定返回 revision 冲突，资源保持未绑定且配置为 `INACTIVE`。本场景由测试代码构造办理证明夹具，只验证 A 的事务锁顺序、引用检查和原生存储，不证明 B 的真实审批/办理或页面/API链路。

该临时副本的版本核对为 `37 / dirty=false`，Go 定向集成用例通过；随后删除副本及容器内转储文件。原隔离库 `bss_a_tdd` 与日常 `model_bss`（36）未被此测试写入，无迁移、部署、提交或推送。正式配置停用的页面/API真实资源场景仍待后续验收。

## Stage 187：内部改价计划同幂等键异请求体冲突（2026-10-26）

在现有 `TestPriceChangePlanHTTPPostgres36CommitsExecutionAndBlockedReplay` 中补入同幂等键、不同计划请求体的 HTTP/PG断言。使用当前源码的真实幂等中间件、路由与 PostgreSQL 仓储，在一次性复制的 v37 隔离库运行；首次计划创建成功后，同一键提交不同 `supplier_model_id`/原因的请求按公共幂等契约返回400，数据库仍只有原计划与一条 `DONE` 幂等结果，未覆写首次响应。用例随后继续通过原有计划成功执行、BLOCKED提交和同键409原响应重放场景。Go 1.26定向测试通过。

本用例使用测试中注入的认证员工及资源数据，证明内部改价计划 HTTP/持久幂等语义，不是浏览器或真实人员认证验收，也不覆盖B的真实流程。测试数据库副本和转储已删除，未修改生产业务代码、日常库、迁移或容器；无提交或推送。原交接验收表曾把异请求体冲突状态写为409，与公共幂等契约及现行实现的400不一致，本轮按公共契约修正文档。

## Stage 188：同步最新 main 与保留 A 工作区（2026-10-26）

按用户最新要求通过本机12000代理fetch，`origin/main`从`f6d6cf0`更新到`a7c5ef4`（B申报/测试平台PR #8）。原`work-A-fix@6c8255a`是新main祖先，因此直接快进28个提交，没有产生本地合并提交或改写历史。同步前为69个已跟踪改动及全部未跟踪文件创建保留快照`5721323b26cdd7c8799e925f20f01fd98e518511`，另保存180份文件的校验清单；快照已应用，继续保留仅作恢复用途。

恢复A改动时仅`internal/api/supply_chain.go`有文本冲突：保留A的供应商事实查询、曾用名兼容及分页，保留B新增的申报驳回/revision与状态机入口调整。另3个重叠文件自动融合：运行装配保留B的AdmissionReportGate和A仓储/附件接线；错误映射保留双方新增分支；前端supplyChain API保留B驳回字段和A分页投影。校验180份原文件无缺失，176份内容与快照一致，4份因上游融合变化；已消除冲突且恢复未暂存状态。

Go服务入口及API、supply、testplatform、supplypg、testplatform适配包仅编译通过（`go test -run '^$'`，没有执行测试）；前端`npm run typecheck`及`git diff --check`通过。本轮未做完整回归、数据库写入、迁移或API部署，未提交或推送。新源码含000038–000042迁移，后续需先检查隔离库结构及测试平台配置，再验证B的新提供方，不能沿用此前“缺实现”的结论或把新接口已入库当作运行验收。

## Stage 189：B新接口、最新数据库与A剩余场景测试设计（2026-10-26）

按用户要求核对`main@a7c5ef4`的申报、报告、内部测试、接入闸门和主装配，新增[联调测试计划](./ab-integration-plan.md#ab-plan)，并同步TDD索引、交接清单、F4/F12计划及跨模块接入文档。计划覆盖DB01～04最新版本升级/旧数据/备份恢复/脚本目标库保护，AB01～09真实A/B链路，A01～06此前未验的独立场景，以及资质/F12依赖齐备后才执行的EX01/02。已通过证据按具体子场景复用，不扩大为合并后新链路已通过。

本轮Docker只读实测：`bss_a_tdd=37/false`、`model_bss=36/false`，仓库迁移最高42。把执行时最新M版的隔离库升级、真实字段核对和日常库/API/前端最终版本对齐列为交付门槛；42的down不恢复压力勾选数据修正，计划要求备份恢复演练。

源码核对发现需优先复现的差异：闸门报告字段名、运行admitter/binder缺项、B报告必需元数据与前端仅比率缓存兼容、草稿资源创建及提交门槛口径、目标指纹/报告选择、内部复测UI与凭据来源、正向分派路由和重复轮询注册。另确认旧档案冲突脚本固定旧API/清理库，需要先防串库改造。上述均记为静态核对发现，不记作已运行失败或已修复。

本轮只设计测试、更新文档及读取当前状态，没有执行新业务用例、调用Inspector、修改应用源码、迁移、重启、提交或推送。文档`git diff --check`通过；新用例全部待执行。

## Stage 190：BSS有版本记录数据库统一42（2026-10-26）

按用户“统一到42版”的明确授权，使用本地标准`migrate/migrate:v4.18.3`镜像及仓库原始迁移执行`goto 42`，未修改迁移文件、force版本或手工更新schema_migrations。

| PostgreSQL容器 / 数据库 | 升级前 | 升级后 |
|---|---|---|
| model_bss_pg / model_bss（日常） | 36 / clean | 42 / clean |
| model_bss_pg / bss_a_f12_work_20261024（历史联调） | 37 / clean | 42 / clean |
| model_bss_pg / model_bss_a_supplier_s03_e2e_20261024（历史联调） | 37 / clean | 42 / clean |
| bss-a-tdd-20261026-pg / bss_a_tdd（当前联调） | 37 / clean | 42 / clean |

升级前逐库生成custom-format备份、校验归档及SHA256，恢复到四个临时副本演练原始迁移；演练成功后短暂停止对应BSS API、重新备份并正式升级，再恢复原来运行的API。未操作其他项目容器。两个`model_bss_a_schema34_*`无迁移版本记录的旧schema测试夹具不作为运行库，未force或删除。

四库均验证：19类关键表行数不变、8类既有业务内容摘要一致、14个新增列/草稿时间可空/申报编号生成列/3个索引存在。迁移42前后压力勾选true计数本次均为0；未发生该数据修正的实际行变化。此结果证明迁移和所检查数据保持，不替代比率、指纹、申报及F12新业务验收。

演练临时数据库及容器内转储已删除；升级前备份、校验清单、快照和执行脚本保留在后端忽略目录`.qa/db-upgrade42-20261026/`，未纳入Git。42的down不恢复压力勾选原值，完整回退按备份恢复。四个备份均已通过实际恢复演练，正式升级前另留最终备份。

8080、18081、18082的`/healthz`均200、db=up/status=ok；三个API恢复后运行且RestartCount=0。日常API仍为原来挂载的`bin/server-v36`二进制（文件名不表示当前DB版本），本轮未更换；两个隔离API按原`go run ./cmd/server`重启。B新接口业务联调、当前main日常构建部署与前端接线继续按Stage189计划进行。本轮未写测试业务数据、提交或推送，原工作区保留。

## Stage 191：删除多余BSS测试环境并保留日常42版（2026-10-26）

按用户截图及“不要的环境删除”授权，核对容器ID、挂载、网络、PG连接及日常依赖后，删除`bss_a_f12_api_qa_20261024`、`bss_s03_api_e2e`、`bss-a-tdd-20261026-api`、`bss-a-tdd-20261026-api-qual`、`bss-a-tdd-20261026-pg`五个旧/临时容器。临时PG独占匿名数据卷和三个`bss-a-tdd-20261026-*`专用空网络一并删除；共享Go缓存、代码bind目录及其他项目容器未操作。

删除前为`bss_a_tdd`及日常PG内四个无人连接的历史测试库分别生成新归档，逐库实际恢复到可销毁副本，并核对15类表行数及6类业务内容摘要一致；20个私有资质文件也逐文件保存SHA256。归档及清理脚本位于后端忽略目录`.qa/environment-cleanup-20261026/`，不上传Git。随后删除`bss_a_f12_work_20261024`、`model_bss_a_schema34_20261020`、`model_bss_a_schema34_334c39`、`model_bss_a_supplier_s03_e2e_20261024`四个历史测试库；临时PG数据已归档后随专用卷移除。恢复演练副本均已删除。

最终BSS仅保留原ID的`model_bss_api_latest`和`model_bss_pg`。PG非模板库仅`model_bss`及系统维护库`postgres`；`model_bss=42/clean`，清理前后数据快照一致。日常API持续运行、`/healthz`正常，未停止/重建日常容器或改其数据卷；未改应用源码、迁移、提交或推送。后续测试先执行DB04的显式环境保护，按需建立一个42版临时环境并在结束时清理，不默认连接已删除的18081/18082或旧QA库。

## Stage 192：日常42版环境首批A/B适配与真实页面冒烟（2026-10-26）

环境统一：仅复用日常两个BSS容器，构建工具直接使用API容器内Go1.26.9；没有新建库、容器、恢复副本或运行迁移。使用显式`BSS_LOCAL_INTEGRATION_DB=model_bss`的新增只读PG用例，独立核对数据库名与42/clean；既有隔离测试的保护没有取消。前端5173已启动并代理8080。保留原API容器ID，更新挂载程序为当前工作区构建；挂载文件名`server-v36`仅为历史名称，不代表数据库或源码版本。

RED真实复现：`AdmissionReportGate`查询`latest_report_id`返回SQLSTATE42703；B报告空列表为nil；未开始任务返回零值测试日期、缺少测试项、无条件认定目标匹配；历史报告会拼接其他任务的测试通过结论。前端仅比率PROVIDED和未开始/启动失败的可空日期新断言均失败。

GREEN修复：使用原生`latest_test_report_id`并校验报告资源归属；报告空列表返回`[]`，测试项/操作人/场景与实际`tested_at`投影到A契约。未实际记录的测试及完成时间保持null，不以创建时间冒充；COMPLETED仍要求实际日期。每份报告保留自身结论，不拼其他任务；有可信非空且一致的行/任务指纹才展示目标匹配。前端支持仅百分比（含真实0%），不反算Token并拒绝非法值。

| 验证 | 结果与精确边界 |
|---|---|
| 日常PG只读探针 | 原生接入查询及空报告列表通过；没有写业务数据 |
| 新增Go投影与既有HTTP定向用例 | 缺失日期、测试项、不借用其他任务结论及安全读取通过 |
| 受影响后端包回归一次 | `internal/api`、`internal/infra/supplypg`、`internal/domain/supply`通过；`cmd/server`编译通过。既有需隔离库环境变量的测试本轮不运行，不计通过 |
| 前端定向与类型检查 | resource-contract六项、supply-front-completion三项通过，`vue-tsc --noEmit`通过 |
| Playwright + 本机Edge真实联调 | `smoke_admin`真实登录；标准模型库入口及真实列表API；B报告空列表真实HTTP200/`[]`；无认证401；模型广场正式入口通过，无页面JS错误。首轮脚本使用错误路由`/internal/resources`超时，改为实际`/internal/model-plaza`后通过，不记为产品缺陷 |
| 部署 | 同一日常API更新后运行、RestartCount=0、healthz为db=up/status=ok；数据库42/clean；只保留两个BSS容器 |

可复现源码：`internal/infra/supplypg/ab_report_contract_test.go`、`tests/resource-contract.test.ts`、`tests/supply-front-completion.scenario.ts`、`tests/ab-daily.live.browser.mjs`。截图/只含断言的结果JSON在前端忽略目录`.qa/ab-daily-20261026/`；本地构建中间文件在后端`.qa/ab-live-20261026/`，均不上传Git。agent-browser浏览器进程不稳定，诊断后使用现有Playwright与Edge，不安装新工具。

本轮没有Inspector出站、申报或报告业务写入，不将投影单测和空历史HTTP扩大为AB03/04全部通过。R04真实admitter/binder/办理提供方仍未装配，正向接入不可勾选；报告job_id与证据test_report.id命名空间、非空历史分页/文件、完整目标变化/晚到报告仍待AB03/05验收。A01～06、AB01/02/06～09与EX条件项保留待验。未提交、推送或改写历史。

## Stage 193：日常42版非空报告历史、下载与接入依赖拒绝（2026-10-26）

范围：继续AB03/04的A消费链路及AB07依赖缺失反例。仍使用`model_bss_api_latest:8080`、`model_bss_pg/model_bss:42/clean`、前端5173，没有新建数据库/容器或运行迁移。真实账号`smoke_admin`经5173登录，Playwright使用现有Edge；页面与API未拦截模拟响应。

夹具由`tests/ab-daily-reports/main.go`显式日常库开关生成：A模型/供应商服务和资源控制存储建立读取对象；B真实`TestPlatformStore.CreateJob/CompleteJob`写入12个任务、9份报告及JSON文件。申报行只是明确标记的下游读取夹具，未经过B真实提交、审批、办理或Inspector，不能作为这些流程的验收。最终用例run为`QA-AB-20261026-092904`：模型3，供应商8/9，资源3/4，任务13～24，报告10～18；任务24与报告18刻意不相等。

RED→GREEN：真实浏览器先发现缺少`test_report_id`；日常库只读断言进一步复现12个CreateJob都漏存传入目标指纹、文件ID使用job_id。修复CreateJob持久化`target_fingerprint`，重新删除/生成夹具验证真实写入，没有手工补值。历史条目保留以job_id为稳定URI，并显式返回`test_job_id/test_report_id`；附件ID使用真实test_report.id，当前引用标签按证据报告ID比较。页面只有比率却显示空Token对的断言先失败，修复后通过。分页脚本曾将Element Plus分页项误当button、及未归一化第一页空白，按真实DOM修正后通过，不记为产品缺陷。

| 定向场景 | 实际结果 |
|---|---|
| 非空历史 | 12条分10+2两页，ID不重复；QUEUED/RUNNING的测试及完成日期null；真实页面可往返翻页 |
| 指标 | 比率0.00%、37.25%、NOT_PROVIDED、0/100 Token明细真实存储/API可区分；RPS真实0.00与连通耗时null不混淆；页面验证37.25%与0.00%，比率专有报告不显示空Token对 |
| 身份与文件 | job_id、test_report.id和附件ID各归其位；详情显示报告与任务；真实HTTP与浏览器下载同一JSON的SHA256一致，Content-Disposition为attachment、Cache-Control为no-store |
| 错误访问 | 跨资源报告404、错误文件ID403、未登录下载401；仅隐藏本轮最后一份文件时下载404，finally恢复，不触及其他文件 |
| 绑定/接入 | 真实PUT绑定及POST接入因运行提供方为空返回503；读取及独立PG核对revision=1、PENDING_TEST、ever_admitted=false、latest_test_report_id为空，报价不可用。该请求在依赖入口即拒绝，不算真实办理权限403或成功接入通过 |
| 检查 | 浏览器7组检查通过，无pageerror；API/supplypg/supply/testplatform相关Go包一次回归通过（包含本轮显式日常库断言）；前端typecheck通过 |

当前API已部署本轮后端修复，原容器不变。新增/扩展测试源码为`tests/ab-daily-reports/main.go`、`internal/infra/supplypg/ab_report_contract_test.go`和前端`tests/resource-reports.live.browser.mjs`；报告消费契约见`supply/supply-resources.draft.md`。截图、下载字节、精确ID清单及JSON断言仅放忽略目录`.qa/ab-report-live-20261026/`与后端`.qa/ab-live-20261026/`，不上传Git。

已精确删除本轮业务夹具和报告文件。清理后resource/test_job/test_report/supplier计数恢复0/0/0/0，standard_model保留原有1条，报告文件目录无本轮文件。没有清库或删库；登录和失败幂等请求等正常技术记录按系统机制保留。

AB03/04的报告存储→真实API→真实页面/文件消费已验，不等同Inspector出站/回收、报告可信选择或准入。AB07成功路径仍缺真实`AdmissionProofLoader`、与当前来源/目标版本匹配的报告证据、`CompleteRow`事务回写及admitter/binder运行装配；不复用stage/simulate中的固定版本或临时资格。AB01/02/05/06/08/09、A01～06和EX条件项继续按计划，尚未全量验收。

## Stage 194：工作台权限/回滚、F1日常42写入及内部补测入口（2026-10-26）

依据用户对三项剩余工作的实现授权，在同一日常`model_bss`42/clean、API8080和前端5173推进。用户随后明确Inspector配置测试先不测，因此本轮不生成或索取真实Key、不配置Inspector出站、不以替身当真实平台通过。

### 标准模型42版写入回归

复用`standard-model.live.browser.mjs`并支持显式使用现有5173，不另起Vite或测试API。首次真实完整保存失败，页面显示网络错误且后端无PUT；定位本机`.env.local`仍直连8080，PUT跨域预检未被允许。改为既有`VITE_API_BASE_URL=/api`同源代理后，真实浏览器依次通过：新增待补齐→填写八类资料/价格/核验日期→单一保存并记录人工核验、报价资料就绪→移除必要价格后新版本核验失效、报价资料阻断→历史/操作记录。模型6的版本1/2/3和三条MANUAL_SAVE日志经独立PG核对；模型及日志已按QA名称/精确ID删除，保留原有模型1。失败首轮夹具也已精确清理。

### 工作台A组合联调

最终夹具`QA-AB-20261026-103042`，模型8、资源7/8、真实staff3（smoke_admin）与staff1（buyer_a）；不从旧截图推定主体或人员ID。资源OFFLINE状态由明确QA存储夹具准备，无测试证据、无报价资格；转交也使用QA控制存储转换，只验A旧计划权限，不冒充C转交入口或B准入通过。

| 场景 | 真实页面/API/PG结果 |
|---|---|
| 多账号 | 非发起人修改/取消/执行上下架计划403；改价由指定办理人执行，创建者不能代办；GET权限随真实操作者返回 |
| 两计划提醒与导航 | 同资源ONLINE/PRICE_CHANGE分别绑定plan_id；实际worker发布两条提醒、重复运行0；独立PG资源摘要不变。浏览器在工作台筛选另一人员并实际点击“详情”，打开正确计划；查看他人后认证身份不变，非本人任务不出现实际完成按钮 |
| 转交及修订/取消 | 新资源归属人不能接管旧计划；原发起人仍可修订/取消。修改日期保留原始expected_resource_revision=2；取消及旧修订调度不再投递，已发提醒不重复 |
| 真事务失败 | 仅对本轮第二条QA资源注入notice插入失败，经真实HTTP执行改价返回500；独立连接比较资源/计划摘要、日志/notice/outbox数量完全一致。移除故障后新意图执行成功；同键重放无重复业务效果 |
| BLOCKED执行 | 资源版本变化后实际执行409且BLOCKED已提交；同键同body重放409，同键不同body400，原计划/资源/日志/notice/outbox结果不被覆盖 |

5组检查通过、无pageerror。仅匹配QA行的故障trigger/function已删除；提醒worker探针会先拒绝处理其他资源的到期计划，没有影响其他业务。测试结束已删除QA计划、阅读/通知、事件、资源、供应商、模型及相关日志。

### 内部补测实现及验证边界

RED：原内部POST允许浏览器自带model/URL/Key直接启动，最小断言实际得到200且starter被调用；页面只有“补测指引”，真实浏览器找不到“发起补测”。改造后只接受category，目标由服务端加载；源码还发现测试平台轮询注册两次，静态断言先失败，删除重复注册后数量为1。

实现：新增补测能力GET和类别提交；运行端使用平台独立`internal_base_url/internal_api_key`，不取供应商测试Key；以真实认证staff及资源控制源行为边界，匹配正在办理的供应商申报与唯一启用的CONFIG节点，只允许其指定办理人，采购归属/管理员/工作台查看人不能代替。授权查询加入请求事务的节点共享锁；缺配置不启动。内部任务记录可信INTERNAL来源及平台目标指纹，响应不回传Key；浏览器提交只含category及幂等键，成功返回任务号并刷新最新报告，进行中按5秒刷新，卸载停止轮询。测试完成不自动接入或报价。

验证：三个API定向用例通过（禁止覆盖、服务端平台目标与真实actor、未配置不启动）；原生日常库授权用例在同一事务内准备/回滚受控节点事实，验证非办理人和已完成节点403、当前指定人员可加载身份，且不返回供应商凭据；不证明B审批生成这些事实。真实浏览器验证新按钮、平台额度提示、无Key输入、禁止提交、实际无办理资格POST403及覆盖凭据400。前端单项契约替身仅验证category/幂等请求与安全回执；不计Inspector联调。

相关api/supplypg/supply/testplatform/config/worker Go包一次回归与前端typecheck通过。当前API已更新。源文件：`tests/workbench-permissions.live.browser.mjs`、`tests/standard-model.live.browser.mjs`、`tests/resource-retest.live.browser.mjs`、`tests/resource-retest.scenario.ts`；后端`internal/api/resource_retest_test.go`、`internal/infra/supplypg/internal_retest_integration_test.go`及QA工具。证据只在忽略目录`.qa/workbench-daily42-20261026/`、`.qa/f1-daily42-20261026/`、`.qa/retest-daily42-20261026/`，无新增结果md。

清理后standard_model/supplier/resource/plan/test_job/test_report计数为1/0/0/0/0/0，QA故障trigger及孤立QA流程为0，日常服务保持运行。API登录与失败幂等等正常技术记录按系统策略保留；未迁移、提交或推送。

尚未验收：真实Inspector配置、平台模型ID映射、真实发起→轮询/回调→报告回收与重启持久化，按用户要求暂不测；平台内部报告与供应侧当前目标/来源版本的权威证据转换仍需AB05/07，不自动选择为准入证据。标准模型与工作台上述范围已通过，不扩大为ONLINE准入、B审批/办理、C转交入口或全部A剩余项已完成。

## Stage195 — A绑定/接入装配与供应商批准改价适配（2026-10-26）

范围：用户要求实现运行绑定/接入及批准改价应用。复用现有领域服务和控制快照，不新增迁移、环境或审批流程，不动Git历史。本次只完成A可独立交付的接线/适配与定向验证；B的真实办理事实尚未注入。

### 契约与实现

- `SupplyResourceWorkflowProviders`集中接收B的Proof/Report/CompleteRow、批准来源Verify/Complete及Actions。运行构造新增可选提供方参数；`WireSupplyResourceWorkflow`真正创建binder/admitter/approved-price服务，依赖缺失时503，不再是对nil服务套闸门。准入报告验证仍在同一写事务内，不以独立读取放行。
- 批准改价新增PG工作单元、POST `resources/:id/apply-approved-price`、GET `resources/:id/workflow-actions`。真实认证staff、精确资源/来源版本、批准金额、完成依据和公共幂等事务贯穿全链。外部布尔确认不能替代B批准/分派事实。
- 改变系数时资源、控制快照、安全来源日志、B行完成、outbox和公告共事务；同价只留痕与完成来源，不增版本/事件/公告。事件沿用资源改系数契约及嵌套source引用；不更改接口模型ID、目标、报告、配置、上下架状态或历史客户报价。
- 已接入资源原来隐藏操作页签，新开“配置与改价”页签。批准金额只读选取，失败保留完成依据/原因；真实GET动作不可用时不开放相应按钮，写接口仍独立校验。
- 提供方接口和接入责任已同步到supply-resources、public-integration-request及联调计划/交接清单。

### 定向验证

| 层次 | 场景与实际结果 | 证据边界 |
|---|---|---|
| RED | 日常运行deps的binder/admitter原为空；批准改价POST原404 | 两条最小断言先失败；实现后通过 |
| 运行/接口 | runtime非空、真实auth actor与版本来源、同键重复仅调用一次、拒绝自报approved、字符串版本、外部确认、403/409/503映射、缺写提供方不宣告可操作 | HTTP成功/鉴权提供方为受控端口，不等于B流程已接通 |
| 日常42版PG | 6个场景：改变系数、同价确认、非办理人、旧资源版本、B完成回写失败、outbox失败，全部通过 | 真实PG事务/控制快照/日志/公告/outbox；B Verify/Complete是明确测试替身，非真实审批。夹具在同一事务内创建并回滚，未建其他库 |
| 领域回归 | 现有ResourceCoefficient的来源/金额/版本拒绝、目标保持、同价、计划发起人及失败回滚定向通过 | 复用领域用例，不扩为全站回归 |
| 前端 | `resource-workflow.test.ts`及`source-operations.test.ts`共4项通过；`npm run typecheck`通过 | 契约解析、批准来源金额、完成确认、精确字符串版本与日志投影/导航 |
| 浏览器+日常API | 实际登录；动作读取、绑定、接入、批准改价在缺B提供方时503；已接入资源页签可达；批准表单只提交批准来源/金额和完成依据，真实POST503后保留输入；资源revision不变，无pageerror | 为呈现批准表单，仅workflow-actions GET使用明确的UI契约响应；POST及资源查询走真实日常API。不是成功办理/接入或真实B批准验收 |

Go命令：现有API容器 `/tmp/bss-current` 中，`BSS_LOCAL_INTEGRATION_DB=model_bss MODEL_BSS_CONFIG=/app/config.yaml go test ./cmd/server ./internal/api ./internal/infra/supplypg ./internal/domain/supply -run 'TestDailyRuntimeAssemblesBindingAndAdmission|TestApprovedPrice|TestResourceWorkflow|TestResourceAdmissionHTTP|TestResourceConfigBindingHTTP|TestResourceCoefficient|TestSupplyRouteModule' -count=1` 通过。随后能力响应校验的小改动只重跑相关API测试，Go build通过；前端类型及单项契约通过，Playwright使用已安装Edge完成上述页面定向检查。

主要测试代码：`cmd/server/supply_runtime_test.go`、`internal/api/supply_approved_price*_test.go`、`internal/api/supply_workflow_test.go`、`internal/infra/supplypg/approved_price_integration_test.go`、`model-pricing-web/tests/resource-workflow.test.ts`、`tests/resource-workflow.live.browser.mjs`。终端输出/截图/临时二进制在忽略目录`.qa/ab-live-20261026/`及`.qa/workflow-daily42-20261026/`；不上传，不新增results文档。

### 部署、清理与剩余

当前二进制已替换同一`model_bss_api_latest`的既有挂载，未创建测试或备份环境。浏览器两条明确标记QA资源及其模型/供应商/申报/日志已按精确清单删除；PG事务测试全部回滚。清理核对运行库42、dirty=false；supplier/resource/plan/test_job/test_report均0，与本轮开始前一致。正常认证和幂等技术记录按系统策略保留；无迁移、提交或推送。

仍未完成的真实业务：B Proof的流程/来源/目标版本、选定且同目标的双类测试证据、CompleteRow、批准改价来源/分派Verify及完成回写/动作列表，需真实提供方注入后继续AB07/08。当前42表结构没有可直接代用的流程版本；baseline/stress原生为不同报告，不能以常量revision、buyer管理员兜底或拼接“默认通过”替代。A后续仍负责适配、运行注入和联合验收。Inspector配置及外部调用按用户要求继续暂不测。

## Stage196 — 原生办理来源、双报告证据与两条真实页面成功链（2026-10-27）

本轮承接Stage195的A适配，完成日常运行默认注入。分支仍为work-A-fix@a7c5ef4，保留全部既有未提交修改；无Git提交/推送、迁移或额外环境。Docker Desktop当时关闭，启动既有Desktop及两个BSS日常容器；Go与PostgreSQL检查均在既有容器完成，前端5173已恢复。

### 契约与实现

1. B实际`SupplyChainRuntime.Approve`在同一审批事务中写真实submission.revision并调用`RecordNativeApproval`，保存安全不可变申报/流程/节点/行/目标关联；A提供方从该快照和当前CONFIG节点读取办理资格，buyer/管理员不能兜底。来源版本使用批准时的实际申报revision；FlowRevision明确为审批operation_log.id的决策引用，不新增或伪造流程表版本。无可信历史关联时拒绝办理，不按供应商推测流程。
2. 在批准进入办理后创建A正式资源控制快照。对B旧版已分配的test-only资源，仅在无A控制快照、未接入且主体/模型/区域一致时接管；已有正式控制状态不能被覆盖。B草稿物理资源分配的旧行为没有在本轮重写。
3. 原生Proof、批准改价Verify/Complete、CompleteAdmission和Actions默认通过`NewNativeSupplyWorkflowProviders`接入newSupplyRuntimeDeps。当前指定人来自真实节点；来源revision、行内容、资源版本逐次核对，写入及完成回写共用公共幂等SQL事务。
4. 新增显式“同步测试结果”操作：POST `/resources/:id/refresh-test-evidence`只收expected_revision/reason，不接收report_id、通过布尔或凭据。按该批准行两个指定任务槽位选baseline/stress，校验资源/类别/状态/来源/指纹，保存两个真实报告/任务引用；ReadSelectedReport兼容此固定双引用，不生成合成报告、不取任意最新类别结果。序号/界面锚点用其中较新的真实report.id，完整双引用保存在审计事实。
5. 实际接入逐行回写ADMITTED、批准改价逐行回写COMPLETED；全行完成才结束CONFIG。首模型存在等待的CONTRACT时只开启该节点并保持流程/申报办理中，否则结束流程/申报。资源/日志/来源回执/B行/outbox/公告仍同事务提交。

### RED/GREEN与定向检查

| 层次 | 实际检查及结果 | 范围 |
|---|---|---|
| RED→GREEN | daily runtime原只返回缺依赖，新增不存在资源应404的断言先失败、真实提供方后通过；申报revision漂移Proof原返回nil，新增断言先失败，补真实头版本比对后409通过；前端can_refresh_tests原被丢弃，断言先失败，解析适配后通过 | 真实失败历史，不倒推TDD |
| 后端成功链 | 实际B Approve方法→真实流程/节点和不可变关联→B CreateJob/CompleteJob保存baseline/stress→A同步→采购人维护配置→当前办理人绑定/接入→资料未齐阻断报价→维护人单一SaveManual补齐核验后可报价→真实批准改价与完成回写，全部通过 | 报告结果是明确QA产物，不调用Inspector；没有替换办理/来源/完成提供方 |
| 后端反例 | 非办理人绑定拒绝；来源revision漂移拒绝；其他任务晚到不覆盖当前证据；替换指定stress任务/目标立即阻断报价，恢复匹配的指定引用后恢复；仅改系数保持报告/配置/目标及资格 | 现有ResourceCoefficient、批准改价6个事务场景、绑定/接入HTTP和选择引用校验一起定向回归 |
| 真实页面接入 | buyer_a为当前指定办理人，smoke_admin为采购归属人/模型维护人。真实GET动作、显式同步、配置列表、绑定、完成确认及接入均200；smoke_admin直接绑定403；未齐模型只阻断报价，模型核验HTTP后真实GET可报价 | 两个独立真实认证浏览器context；未拦截或mock任何页面请求 |
| 真实页面改价 | 经实际Approve方法建立PRICE_CHANGE来源后，页面选择批准系数0.33→填写完成依据/原因→200；非办理人403、旧source_revision409；同键同请求重放200且回执一致、资源仅增一版、系数0.3300，报告/配置及可报价资格保持 | 无模拟workflow-actions或写入提供方 |
| 独立PG核对 | 页面结束后PRICE_CHANGE行COMPLETED、submission COMPLETED、CONFIG/flow COMPLETED；本轮资源RESOURCE_ADMITTED及RESOURCE_COEFFICIENT_CHANGED各1条 | 实际外部连接查询，不以页面成功提示替代回写 |
| 负例兼容 | 旧resource-workflow.live.browser脚本改为原生无批准事实场景：GET无可办动作，写不能成功；仅为展示错误保留草稿的GET契约响应被替换，真实POST403且输入保留 | 此项单独标注UI契约替换，不扩大为正向流程；正向脚本完全无mock |
| 前端 | npm run typecheck通过，resource-workflow.test.ts与source-operations.test.ts共4项通过 | 请求/权限投影、精确版本及安全日志导航 |

后端命令：现有API容器/tmp/bss-current下，`BSS_LOCAL_INTEGRATION_DB=model_bss MODEL_BSS_CONFIG=/app/config.yaml go test ./cmd/server ./internal/api ./internal/infra/supplypg ./internal/domain/supply -run 'TestNativeHandlingDaily42Success|TestDailyRuntimeAssemblesBindingAndAdmission|TestApprovedPrice|TestResourceWorkflow|TestSelectedReport|TestResourceAdmissionHTTP|TestResourceConfigBindingHTTP|TestResourceCoefficient' -count=1`，四包通过；当前源码go build通过后更新原API挂载二进制。之后只有测试工具/文档改动，无需重复全站回归。

真实页面脚本：`model-pricing-web/tests/native-handling.live.browser.mjs`，分admit/price两段；`BSS_NATIVE_MODE=browser/prepare-price/cleanup`用于建立及精确清理QA源数据。后端测试源码`cmd/server/native_handling_test.go`。Playwright使用本机Edge；报告产物通过B实际存储写入测试目录，测试结束删除。截图/checks与临时manifest仅存忽略目录`.qa/native-handling-20261027/`、`.qa/ab-live-20261026/`，不入Git。

### 清理、部署与剩余边界

通过精确ID/QA主体清理本轮模型、供应商、资源、正式配置、申报、流程节点、报告任务、日志、公告、事件和临时审批人员；既有人员审批资格没有改动，临时有资格人员仅为测试创建并删除。正常认证和幂等技术记录按系统策略保留。清理后standard_model/supplier/resource/plan/test_job/test_report=1/0/0/0/0/0、QA-NATIVE人员0，数据库42/dirty=false；原API running、RestartCount=0。前端5173与API8080可用。未新增容器/库、迁移、提交或推送。

当前A提供方不再缺失，AB07及AB08的“批准来源之后A办理应用/回写”可勾选。仍未覆盖：B正向领导待办/flows HTTP入口（旧approve/simulate写路由保持下线，未重新开放），供应商从门户提交到领导点击批准的全页面链、首模型后续CONTRACT/F12的实际完成、草稿test-only资源语义统一；Inspector真实配置/出站/回收和平台内部报告来源转换继续按用户要求暂不测试。不能把“实际审批方法已调用”写成“领导工作台审批按钮已测通”，也不能把QA报告产物写成Inspector实测。

## Stage197 — 工作台真实审批分派、引用配置保护与ONLINE页面验收（2026-10-27）

本轮完成用户选中的三项：42版配置引用停用保护、最新Lovable标准模型UI复核、ONLINE执行和领导工作台→A办理。直接使用既有日常API8080、PG/model_bss42及前端5173；未新建环境、迁移、提交或推送，保留所有既有工作区修改。

### 契约与代码

- 新增原生GET `/internal/flows/submissions/:id`、GET `/internal/flows/nodes/:id`、POST `/internal/flows/submissions/:id/approve`。只读投影不含供应商测试URL/Key；审批使用真实认证审批资格、expected_revision及实际在用办理人，首次模型要求合同办理人。
- `ApproveVersioned`复用已有实际审批逻辑，加入公共幂等SQL事务；锁定并核对人员资格、供应商/申报及版本，创建真实流程/指定节点和安全来源快照。保留旧方法用于既有调用，不恢复旧approve/simulate HTTP或模拟测试准入。
- 工作台查询增加`SUBMISSION_APPROVAL`共享审批待办、`SUBMISSION_HANDLING`唯一CONFIG办理待办，去重旧节点投影。详情内审批分派；指定人从同一工作台进入资源办理，完成后已办只保留查看链接。首次CONFIG完成开启CONTRACT，不伪报整单完成。
- 配置引用保护领域/API已有，无需改写；新增真实页面断言。标准模型880px手动补全弹窗与最新原型对应字段/交互一致，本轮无需改动该表单。

### 定向验证结果

| 项目 | 实际结果 | 边界 |
|---|---|---|
| TDD/Go | 新任务类型深链原为空，断言失败后补工作台深链；新增flows缺依赖503、旧approve/simulate404保护通过。实际B审批→双报告→A办理成功链定向回归通过，三包绿色 | 不等于全站回归；报告为QA产物 |
| 领导待办页面 | 临时真实有资格账号看到待审批事项；打开详情、选择buyer_a测试配置办理人及首次合同办理人、填写意见、审批分派200；buyer_a刷新本人工作台后打开指定任务，进入正确资源操作页 | 未拦截/mock任何请求；起点为明确QA待审批申报，不包含供应商门户提交前段 |
| 审批防重/授权 | 非资格账号403、旧版本409；同键重放200同回执；同业务新键按公共语义幂等200同回执，修改决策请求409；独立SQL确认仅1条审批记录和1个流程 | 不把公共幂等语义误记为新键必须409 |
| 真实A办理 | 同一审批生成的资源同步指定baseline/stress报告→绑定配置→逐资源接入→MODEL_NOT_READY阻断报价→单一手工补全核验后可报价，全部200；采购人不能代当前办理人绑定 | B CreateJob/CompleteJob实际落库；Inspector未调用，不以默认通过替代资格 |
| 引用配置停用 | 实际采购人从配置表停用返回409；重新加载仍ACTIVE/revision1。非采购人停用403；资源revision不变。独立SQL确认配置仅创建时1条FORMAL_CONFIG_REVISION，没有停用版本 | 不读取/输出Key；本轮验收引用保护，已有新增/轮换证据复用 |
| 工作台完成回写 | CONFIG完成后本人TODO移除、DONE中仅1条；页面勾选已办、打开详情只能查看资源，不能继续办理。领导DONE保留审批记录 | 首次CONTRACT为IN_PROGRESS、指定人员3，submission保持PROCESSING是预期 |
| ONLINE计划 | 从已接入且可报价资源真实页面创建并完成OFFLINE，再创建并完成ONLINE；创建不改状态，执行各增1版；ONLINE恢复ADMITTED和可报价；报告/配置保持，同键重放200且回执一致 | 独立SQL：两计划DONE；RESOURCE_ADMITTED/OFFLINED/ONLINED各1条，提醒调度2条 |
| 最新Lovable | Playwright直接加载实际lovable.app iframe，标准模型“维护”→“手动补全”；880px弹窗字段顺序、六列价格表/增减、URL/日期、取消/保存并留痕和未披露说明与本地截图核对一致 | 12000当时无监听，改用获准联网环境直连成功；F2模拟抓取不计真实实现；仅核对本轮指定标准模型范围 |
| 前端 | typecheck通过；三份真实浏览器脚本无pageerror，Node语法检查通过 | 不包含Inspector、其他页面全量UI回归 |

命令：现有API容器`/tmp/bss-current`，`BSS_LOCAL_INTEGRATION_DB=model_bss go test ./cmd/server ./internal/api ./internal/domain/workbench -run 'TestNativeHandlingDaily42Success|TestNativeFlow|TestNativeSubmissionTasks' -count=1`，三包通过。当前源码go build后替换原API挂载二进制；此后仅注释、测试工具及文档变化。前端`npm run typecheck`及三份脚本`node --check`通过。

浏览器脚本：`model-pricing-web/tests/leadership-flow.daily.browser.mjs`（审批和complete两段）、`native-handling.live.browser.mjs`（admit）、`online-plan.daily.browser.mjs`。Go夹具`cmd/server/native_handling_test.go`的leadership/evidence/cleanup模式用于真实账号/待审批数据、B报告产物和精确清理。脚本初次遇到Element Plus选择器遮挡、页面停留同路由未刷新的定位问题，修正脚本后完整重跑通过；QA证据准备曾漏ReportJSON，真实存储未产生报告并被409拒绝，补真实QA产物后通过，未降低生产准入校验。

截图/checks只存忽略目录`model-pricing-web/.qa/leadership-flow-20261027/`、`native-handling-20261027/`、`online-plan-20261027/`、`prototype-20261027/`；临时manifest/二进制在后端`.qa/ab-live-20261026/`，均不入Git，不新增results.md。

### 清理与剩余

按精确清单删除本轮QA模型、供应商、资源、配置、申报、流程、计划、报告任务、日志、公告、事件、临时审批账号/登录会话和人员；既有人员资格未更改。清理后standard_model/supplier/resource/plan/test_job/test_report=1/0/0/0/0/0，QA-NATIVE账号/人员均0，数据库42/dirty=false；API与PG运行，8080健康`db=up`，5173返回200。正常认证/幂等技术记录按系统策略保留。

Stage196“领导正向flows未注册/工作台审批未测通”已由本阶段覆盖。仍未验收：供应商门户草稿/自测/提交到领导的前段、驳回/重提全页面组合、首次CONTRACT/F12完成、历史草稿test-only资源语义统一、真实Inspector配置/调用/回收及平台内部报告来源转换。F4/F12其他结构、授权与主体映射仍按各自清单推进；本轮不宣称A全部工作完成。

## Stage198 — 按产品截图修正手动补全主表单（2026-10-27）

用户提供当前实现与产品原型对照截图，指出额外操作说明、补充来源、历史链接及控件样式差异。Stage197主要核对字段及保存语义，对完整呈现判断不足；其“无需UI改动”的结论由本阶段更正。

### 调整范围

- 主表单在核验日期后结束，不再插入补充来源、计费信息折叠块、操作说明和历史/日志链接。列表每条模型的“更多”提供历史版本、操作记录和补充资料；补充资料仍可编辑官方来源、价格维度、逐项来源及自定义说明，返回主表单统一保存。
- 计费表扩大列宽并保留可见横向滚动，单位改为直接文本输入，增加计费项为满宽按钮；调整弹窗关闭按钮、表头、控件高度、间距和页脚。日期使用右侧日历图标的原生date输入，保留上海日期上限及原有校验。
- 仍为单一保存并留痕。未填写自定义说明时发送真实动作描述“手动补全官方资料”或“新增标准模型资料”，服务端reason契约不变；完整性、来源、版本、维护人和核验门槛不变，原补充来源/维度不因主表单隐藏而丢失。本轮无后端、迁移和Docker部署变化。

### RED/GREEN与定向验证

| 验证 | 实际结果 |
|---|---|
| RED | 新Playwright断言主表单不含操作说明，原实现实际命中1个输入，失败1≠0 |
| UI定向 | `standard-model-layout.browser.mjs`通过：主表单无额外区域、原生日历、满宽增加按钮、单次保存带非空reason且保留补充来源；“更多→历史”可读；“更多→补充资料”修改来源和说明后提交保持价格；窄屏横向滚动，无pageerror。此项使用明确API夹具，不冒充真库验收 |
| 既有页面回归 | `standard-model.browser.mjs`通过：精确金额、冲突保留输入、重新读取、单次保存核验、历史只读、操作记录、非维护人只读、不完整保存及窄屏。脚本同步新入口和五个主文本区域 |
| 日常42版真实链路 | `standard-model.live.browser.mjs`使用真实smoke_admin认证、5173→8080、model_bss42，无请求模拟；新增待补齐200→完整单一保存核验200→移除输出价格再次保存200且不可报价，历史3版/日志3条，无pageerror。没有在主表单填写额外说明，验证默认动作留痕仍可写入 |
| 类型及差异检查 | `npm run typecheck`、本轮文件`git diff --check`通过 |

截图/JSON仅存前端忽略目录`.qa/standard-model-layout-20261027/`及`.qa/f1-layout-live-20261027/`。真实链路创建的精确QA模型89及其3条日志已删除，原模型保留，standard_model计数恢复1，数据库42/dirty=false；未新增容器/数据库、未提交或推送。Inspector及其他模块不在本轮测试范围。

## Stage199 — 新待办驳回重提与首次合同授信任务接续（2026-10-27）

范围为用户选中的工作台驳回及首次合同授信接续。复用实际B申报存储和原生审批，不新增流程体系、DDL或环境。**驳回→供应商修改重提→再次审批→A接入已真实贯通；合同任务投影/导航已接，真实F12资料保存和终态办结仍未通过。**

### 实现及责任

- 新增POST `/internal/flows/submissions/:id/reject`，仅收字符串expected_revision和原因。新待办详情提供“驳回申报”，无须先选后续人员；原因必填，失败保留输入。服务端独立校验真实在用审批资格和PENDING_APPROVAL版本，批准后不能再以驳回方式撤销既有办理。
- 修正已有Reject另开事务及遗漏操作者的问题：加入公共幂等SQL事务，决策、真实operator_id/name及原因同事务；旧驳回入口继续复用该方法。工作台投影本次驳回到实际决策人员已办；重新提交后再次进入共享资格待办。
- 原生详情增加真实nodes、办理人姓名/状态、选定节点、flow_id及flow_revision。flow_revision沿Stage196定义，引用实际批准日志ID，不伪造流程表版本。CONTRACT节点使用`SUBMISSION_CONTRACT`投影到指定人员，跳进工作台合同授信详情和现有A F12共用面板，带可信主体/流程/模块/版本。
- 修正共享v_work_item的ONBOARDING_NODE.biz_id实际为flow.id，旧按node.id去重会出现“合同签署”和新合同任务重复。只对已有原生批准关联的CONFIG/CONTRACT按flow.id去重，不改公共视图或迁移。
- 新增POST `/internal/flows/nodes/:id/complete-contract`及完成消费者：同事务锁主体/申报/节点/流程，重验指定人、决策版本、CONFIG完成和真实F12齐备快照的主体/模块/版本；齐备才保存节点、流程、申报终态和带资料版本的操作记录。客户端不提交ready或资料ID替代齐备查询。main构造接收现有SupplyDeps.F12Readiness，当前提供方未装配则503，无资料时不默认通过。
- 供应商实际修改页遇到原标准模型资料待补齐，A目录适配原来因空公司/官方ID拒绝整个列表。已改为校验真实ID和文本类型，空资料明确显示待补齐；不改变标准模型核验/报价门槛，不伪造公司或模型身份。

### 定向证据

| 场景 | 实际结果与限制 |
|---|---|
| RED | 新驳回及合同完成路由断言先得到404；实现后缺依赖503通过。真实供应商修改页因目录待补齐报错，新断言得到1≠0；适配后通过。合同待办去重断言先见旧ONBOARDING_NODE多1条，按真实flow.id修复后0条 |
| 驳回事务 | `TestNativeRejectionDaily42SharesTransaction`通过：外层回滚使申报保持待审批；非审批人拒绝、旧revision拒绝、实际操作人留痕且只有1次决策，重复决策拒绝。显式日常QA夹具精确清理 |
| 真实驳回页面 | 有资格临时账号从工作台打开新详情，未填原因不可驳回，填写后200；非资格403、旧版本409；同键重放200同回执。驳回后无流程/正式资源，实际审批人员已办可查 |
| 真实修改重提 | 临时真实供应商以现有手机号验证码认证，从历史“修改重提”进入A表单，修订采购系数/自测配置和说明、实际PUT保存200；由B真实存储准备明确QA双报告后，实际页面提交200，领导再次看到待审批，系数为0.4500。第一段脚本曾因Key定位/目录校验失败停在修改页，修正后继续同一真实申报完成；不将失败整段冒充一次全绿 |
| 再审批及A接入 | 同一重提申报经真实页面再次审批分派，指定A人员同步报告、绑定/接入和模型核验后可报价通过；兼容B已分配自测资源的既有接管逻辑，未改变草稿test-only语义。报告均为显式QA产物，未调用Inspector |
| 合同真实接续 | CONFIG完成后指定人员工作台仅1条合同授信任务，无旧节点重复；新详情展示真实人/节点并打开A面板。非指定人完成403、旧决策版本409、无齐备提供方503；页面说明依赖未就绪且完成按钮禁用。独立SQL确认CONFIG COMPLETED、CONTRACT IN_PROGRESS、申报PROCESSING/revision5、实际完成日志0条 |
| 合同事务消费者 | `TestNativeContractCompletionPortDaily42`在同一实际QA合同节点上使用明确F12齐备端口替身：缺齐备409；成功候选更新终态/审计后由外层回滚，独立连接确认仍IN_PROGRESS。仅证明消费者与事务回滚，不证明真实合同资料或F12成功办结 |
| 编译/前端 | cmd/server、api、workbench三包定向通过，当前Go build通过并更新同一API；npm run typecheck通过。真实浏览器各已完成阶段无pageerror；无浏览器请求mock |

后端命令：已有API容器`/tmp/bss-current`中，`BSS_LOCAL_INTEGRATION_DB=model_bss BSS_NATIVE_MANIFEST=/tmp/bss-native-handling.json go test ./cmd/server ./internal/api ./internal/domain/workbench -run 'TestNativeRejectionDaily42|TestNativeContractCompletionPortDaily42|TestNativeFlow|TestNativeSubmissionTasks' -count=1`通过。浏览器复用`leadership-flow.daily.browser.mjs`、`native-handling.live.browser.mjs`，新增`workbench-rejection.daily.browser.mjs`的reject/edit/submit/contract阶段。QA夹具新增真实供应商认证和supplier-evidence模式；准备报告调用实际B存储，不走外部Inspector。

### 当前未完成与清理

运行库42未具备F12申请的contract_key、document_name、file_id、source_flow_id/source_flow_revision等字段；A运行构造的F12流程授权和齐备提供方仍需装配。因此不能把任务跳转成功写成“首次合同授信全闭环完成”。结构仍由约定维护人审核落迁移，A继续负责自身F12授权/齐备适配与接线，不能全部转为B责任；结构和真实提供方到位后补首次资料上传/版本/归档/额度→完成→工作台已办的真实验收。B提交测试门槛及草稿物理测试资源旧语义另行协调，本轮没有改写。

截图/checks仅存忽略目录`.qa/workbench-rejection-20261027/`及原native/leadership目录，不新增results.md。独立SQL确认本轮REJECT与APPROVE各1条，均记录真实审批人。全部QA模型/供应商/资源/配置/申报/流程/测试产物/日志/公告/事件、临时领导和供应商账号/会话均按精确清单删除，既有账号资格未改；计数恢复1/0/0/0/0/0、QA账号及人员均0、数据库42/dirty=false。8080健康、5173返回200。未创建新容器/库、迁移、提交或推送。

## Stage200 — v43结构、供应商首次F12保存及真实办结（2026-10-27）

用户明确要求由A配置v43并继续实现，本轮授权覆盖F12迁移和日常部署。已检查远端main仍为a7c5ef4，原最高迁移42；保留work-A-fix全部原有未提交改动，不提交、推送或改写历史。

### 实现与数据库

- 新增`000043_f12_records` up/down：合同9列、授信8列元数据，稳定contract_key、名称/编号、opaque file_id、来源flow/module/revision、原因及实际创建人；保留旧列及历史NULL，复用现有version。建立合同档案/版本和授信主体/方向/币种/版本两个唯一索引，新记录及来源完整性CHECK。F4曾用名、资质字段不在本迁移中。
- 日常升级前42/clean，合同/授信均0条，无重复版本；备份`.qa/f12-v43-20261027/model_bss_before_v43.dump`（285857字节），pg_restore目录检查659个TOC条目。带42/clean预检、迁移锁、DDL和schema_migrations更新在同一事务提交；实际43/dirty=false。没有只修改版本号。
- `NativeSupplierF12`接真实原生APPROVE关联、supplier/submission/onboarding_flow/CONTRACT节点，实际批准日志ID作为已有来源版本。首次写入仅指定人，普通采购人不得绕过进行中的首次节点；可在CONFIG阶段准备资料，办结必须等CONFIG完成。结算币种由供应商事实读取，缺失不能默认CNY。
- A运行构造已装配F12RecordStore授权、F12BusinessContext及ReadinessService。完成HTTP读取资料借用原公共SQL事务，GET使用只读快照；前端依据真实齐备事件禁用/启用完成按钮。完成日志保存当时实际合同/授信版本，客户端不能自报ready。
- 同名日常API重建以启用`MODEL_BSS_F12_ATTACHMENT_STORE_DIR=/app/private/f12-attachments`及宿主`.private/f12-attachments`绑定目录；仍只使用两个BSS日常容器，无额外数据库/测试环境。更新当前源码二进制；旧挂载文件名server-v36不代表数据库仍36。私有目录已加入.gitignore。

### RED/GREEN及定向联调

| 场景 | 实际结果 |
|---|---|
| v43能力与装配RED/GREEN | 42库F12结构能力false、原运行Readiness为空；补迁移及构造后`TestF12Daily43SchemaCapability`和`TestDailyRuntimeWiresNativeF12`通过 |
| 附件读取RED/GREEN | 真实GET下载500，定向测试复现`SELECT FOR SHARE in a read-only transaction`；读授权去行锁后通过。采购人读取反例原403，复用真实采购归属链后通过，其他人员仍403；写授权未扩大 |
| 真实审批与首次资料 | 临时真实审批账号从工作台批准，CONFIG/CONTRACT均指定buyer_a（staff1），实际采购人及维护人为smoke_admin（staff3）。buyer_a通过真实A面板上传PNG合同和授信，保存200；初始缺资料ready=false，补齐后true；前序尚未完成时完成按钮禁用、直接POST409，非办理人完成/写入403 |
| 真实配置及商务办结 | B真实存储准备显式QA baseline/stress产物，未调用Inspector；真实页面同步报告→绑定正式配置→登记接入→模型核验后可报价，CONFIG完成并激活CONTRACT。指定人工作台出现1条合同待办，非旧视图重复任务 |
| 版本与完成页面 | 同一合同修订追加V2并复用V1受控附件，历史V1保留；授信100.00→80.00形成两版，当前值80.00，齐备引用合同V2及授信V2。填写说明后实际页面完成200；旧来源版本409、非办理人403、同键重放200同回执、改变完成原因的新请求409 |
| 独立SQL及已办 | submission104为COMPLETED/revision3，CONFIG和CONTRACT均COMPLETED且有实际完成时间；COMPLETE_CONTRACT日志仅1条、操作者staff1，快照精确引用合同V2及授信80.00/V2。实际待办0、已办1，完成后详情只读 |
| 重启与授权下载 | 真实页面点击下载及原始字节校验通过；重启同一日常API后，3个附件SHA-256仍一致，原办理人和当前采购人可下载，已办详情仍只读。没有用浏览器mock代替真实文件 |
| 回退保护 | 存在本轮F12数据时执行down的单事务保护检查，按预期抛出`F12 v43 data exists`，没有删除列、索引或业务数据；随后独立SQL仍43/clean及完整版本 |
| 编译/类型检查 | Go四包F12定向回归及实际v43结构/授权/装配测试通过，当前server构建通过并部署；npm run typecheck通过。未跑全站/全部历史隔离库回归 |

源码：`internal/infra/supplypg/f12_native_flow.go`及`f12_daily43_test.go`、`cmd/server/f12_runtime_test.go`；浏览器新增`tests/f12-first-onboarding.daily.browser.mjs`的documents/complete/persist阶段，复用leadership-flow及native-handling的真实审批/接入阶段。Go真实检查在现有API容器中使用`BSS_F12_V43_TEST=1`及本轮native manifest显式开启；`go test ./cmd/server ./internal/infra/supplypg ./internal/domain/supply ./internal/api -run 'TestDailyRuntimeWiresNativeF12|TestNativeF12ReadOnlyAuthorization|TestF12Daily43SchemaCapability|TestF12' -count=1`通过。完成真实夹具清理后不要直接重跑依赖该manifest业务行的授权用例，须先准备新QA流程。

截图/checks仅存前端忽略目录`.qa/f12-first-v43-20261027/`，转储、临时配置、manifest及二进制在后端`.qa/f12-v43-20261027/`，不进入Git，不新增results.md。按精确主体清理本轮所有业务行、临时账号/人员和三个附件及其sidecar；最后standard_model/supplier/resource/plan/test_job/test_report/contract/credit_line=1/0/0/0/0/0/0/0，QA人员0，私有目录文件0，数据库43/clean。原账号资格未修改，既有模型保留。

### 仍未覆盖

本阶段解除Stage199的“42缺F12列、A授权/齐备未装配、首次供应商无法办结”阻断。**客户F12主体/流程/普通维护提供方的真实映射尚未接入**，不能用customer.id与customer_profile.id相等冒充；D07仍是后续文件集成。归档、跨页冲突、双主体同数字ID和并发版本等未在本轮真库/页面回归，已有领域/历史证据不等同本阶段已验。F4曾用名/资质结构、草稿测试资源语义和真实Inspector配置/调用/任务回收继续按各自交接项推进。

## Stage201 — 供应商详情、分阶段批量办理、曾用名和资质元数据（2026-10-27）

范围为用户指定的四项。分阶段规则由DEC-019确认：每条独立校验/返回结果，正式配置与自身双测仍为准入前提，取消仅结束未接入办理行；已接入资源仍走下架计划。保留工作区和Git历史，不提交推送；只使用日常API/PG和5173。

### 结构与实现

- 备份43/clean后应用新`000044_supplier_metadata`，DDL及迁移号更新同一事务，最终44/clean；不改历史v43。新增supplier_alias五列、主体内唯一约束和RESTRICT外键，以及supplier_qualification的类型/有效期/审核状态/revision/创建人六列，日期及修订号CHECK。既有资质file_ref和历史NULL保留，不猜测D07 file_id，不生成审核通过事实。
- 升级前备份`.qa/f4-v44-20261027/model_bss_before_v44.dump`可读，pg_restore目录668条。已有新数据时down按预期拒绝删除结构；本轮未执行备份恢复演练。
- 新增原生供应商进度与全状态申报历史GET，页面显示实际节点、人员、行状态、提交时间、版本及系数，详情复用真实flows。查询仅使用当前supplier/submission身份，未拿旧supplier_profile数字ID拼接quote_sheet历史。
- 新增批量resource-actions：CONFIGURE登记实际外部配置及当前正式配置；CONFIRM核对本资源/目标/配置/来源版本匹配的配置完成事实，再复用既有真实逐资源准入及B回写；CANCEL由指定人员结束未接入行并留痕，不下架或删除资源/配置/报告。
- 批次复用公共幂等事务，每条有独立保存点；本条失败回滚后继续，合法请求HTTP200、results内逐条200/400/403/404/409/503/500。页面允许多选、逐条选配置、填写依据、显示结果；失败不冒充整批通过。
- B终态计数增加CANCELED，部分取消不结束其他行；全部结束推进CONFIG。全取消且没有已接入行时关闭未开始CONTRACT，不遗留商务待办、不误报接入。取消行不能再绑定/接入/补测，已关闭节点的合法历史资料仍可读取，写入仍禁止。
- 同名API补资质私有目录环境及宿主绑定，保留F12挂载；更新源码构建二进制。只有两个日常BSS容器，没有新测试库/容器；二进制挂载旧文件名server-v36不代表实际库版本。

### RED/GREEN与定向验收

| 场景 | 实际结果 |
|---|---|
| 新结构与入口RED | 43版曾用名能力false、进度/历史及批量入口404；v44与真实路由/装配后同一目标用例通过 |
| 曾用名真实页面 | 采购人从详情维护，改成不包含旧名的新名称；用旧名称在主体接口及供应商列表查到同一ID且仅一条；改回原名后别名保留新名称、排除当前名称，无重复 |
| 资质真实上传 | 类型“营业执照”、2026-01-01～2027-01-01、PNG及原因保存200；独立读取metadata_supported=true、revision1、created_by3、审核事实缺失。采购人下载字节一致，其他员工403 |
| 资质真实维护冲突 | 非采购人PUT403、起始晚于截止400；另一次合法维护先保存R2，原页面R1保存409且名称草稿保持；显式刷新最新修订后原草稿重试200，最终R3，原附件不变、审核仍未记录 |
| 实际节点及历史 | 三模型审批后查询返回真实6个节点映射及人员staff1；详情无“待B接入”占位，节点可进原工作台。报价页读取当前主体全状态原生申报并打开三行详情，不再只请求旧pending接口 |
| 三模型分阶段 | 临时真实领导从工作台审批分派；第1条通过B真实存储准备明确QA baseline/stress，另外2条缺测试。真实多选配置3条均200；确认开通第1条200、另2条各409；只有第1条资源ADMITTED。页面取消剩余2条均200、B行CANCELED，CONFIG结束后CONTRACT正常激活 |
| 取消及重复保护 | 对已接入第1条取消结果409且资源不变；各批次同键重放200同结果。另一单模型真实申报全部取消，非指定采购人403；实际CONFIG COMPLETED、CONTRACT停用、申报处理结束、待办0，资源保持原状态且ever_admitted未置真 |
| 取消后补测RED/GREEN | 在回滚事务中保持其他行仍可办理的CONFIG上下文，取消行原来仍获内部补测目标；补CANCELED查询限制后明确Forbidden。未调用Inspector，也未提交该场景的临时节点调整 |
| 历史读取RED/GREEN | 全取消停用CONTRACT后历史读取授权原来Forbidden；读取允许查询历史节点、写入仍要求enabled后，实际指定人及当前采购人的只读事务检查通过，其他人仍Forbidden |
| 持久化及独立SQL | 同一个API重启后资质日期/R3/PNG保持；独立SQL见CONFIG_COMPLETE3条、CANCEL_ACCESS2条，主批次行ADMITTED/CANCELED/CANCELED，CONFIG COMPLETED/CONTRACT IN_PROGRESS，未产生其他资源接入 |
| 页面及类型回归 | 新日常Playwright的profile/stages/qualification/all-cancel/persist通过，无请求mock及pageerror。受影响model-plaza-supplier-detail和supplier-management的fixture页面回归通过；4项操作日志Node用例通过，类型检查通过。fixture不冒充真库验收 |
| Go及构建 | api/supplypg/supply/cmd-server四包定向通过，含v44能力、F12兼容与只读授权、取消补测、资质/API及运行装配；server构建成功并部署。未跑全站/全部旧隔离库用例 |

新日常脚本`model-pricing-web/tests/supplier-v44.daily.browser.mjs`；夹具复用native_handling的真实三模型/单模型审批、明确QA报告及精确清理。浏览器脚本首次遇到多模型严格定位、Element Plus隐藏checkbox、旧fixture缺新字段及F12不再显示原始ID的断言，已修正选择器/fixture后完成定向重跑；没有降低生产校验。

Go检查在现有容器显式设置`BSS_SUPPLIER_V44_TEST=1`、`BSS_F12_V43_TEST=1`及本轮native manifest，运行`go test ./internal/api ./internal/infra/supplypg ./internal/domain/supply ./cmd/server -run 'TestCanceledRowCannotRetestDaily44|TestSupplierDaily44|TestF12Daily43|TestNativeF12ReadOnly|TestNativeFlow|TestResourceBatch|TestSupplierQualification|TestSupplierAliases|TestDailyRuntimeWiresNativeF12' -count=1`通过。依赖QA行的用例在精确清理后必须重新准备夹具，不能拿已删除对象直接重跑。

### 清理与边界

两组QA的主体、四个模型、资源、配置、申报/流程、资质/别名、文件、测试产物、日志/公告/事件、临时账号/人员均按精确清单清理。最后standard_model/supplier/resource/supplier_alias/supplier_qualification/submission/test_job/test_report=1/0/0/0/0/0/0/0，QA账号0，资质私有目录文件0，日常44/clean，API运行且RestartCount=0；原模型和既有账号资格保留。截图/checks在前端忽略目录`.qa/supplier-v44-20261027/`，转储、临时配置、manifest/二进制在后端`.qa/f4-v44-20261027/`，私有文件目录已忽略，不入Git，不新增results.md。

本阶段完成曾用名及资质元数据，不代表权威资质审核写入或D07已接；状态缺失仍显示未记录，文件继续使用A本地受控适配。Inspector按用户约定未调用。客户F12映射、供应商合并执行、F2及未列明的跨模块场景仍独立推进。取消只结束办理行，保留资源和历史，不调用外部下架/停止测试，也不扩展B的同模型重新申报资源复用协议。

## Stage202：供应商F12、日志与附件保护收尾（2026-10-27）

### 范围与修复

仅执行用户指定的三项：供应商F12收尾、新业务日志、附件清扫/失败保护。环境为原日常`model_bss_api_latest:8080`、`model_bss_pg/model_bss=44/dirty=false`及5173真实认证页面，没有新增数据库、容器或迁移，也未调用Inspector。

真实页面发现普通维护上传附件返回400/source_flow_id：附件接口把可选的流程字段误列入必填字段。新增`TestF12AttachmentOrdinaryMaintenanceOmitsFlowFields`先复现400/calls=0，再移除该三个字段的无条件必填要求后通过；流程办理仍须成组提供有效来源及revision并通过既有授权，没有放松流程权限。当前运行API已部署此修复，历史命名`server-v36`的二进制文件不代表数据库版本。

### 实际通过

- F12真实页面：准备23个独立合同档案，目标初始位于第二页；另一请求新增V2后，原V1提交得到409，草稿和已上传文件保留；按档案键查询最新版本，显式重试得到V3，仅上传一次。最新版本即使移动到另一页也可恢复。
- 合同V3归档生成V4/ARCHIVED，V1～V3正文、附件保持不变；原附件和新附件均可按权限下载。归档后继续修订409，非归属人员归档403。
- 真实HTTP并发：同一合同expected=1的两个修订，仅一个200、一个409，版本只有V1/V2；同主体/方向/币种的两个授信增版同样200/409，历史仅两版。
- 合同失败一致性：只针对标记QA主体与reason注入真实operation_log写入异常，业务插入后的请求500；独立GET确认合同及日志总数均不变。未绑定文件不可下载；移除故障后以新意图复用上传文件保存成功并可下载，不重复上传。
- 资质失败一致性：真实HTTP上传字节后注入日志异常，响应500；资质记录和日志均未新增，私有目录文件集合保持原样，没有遗留该失败上传的blob/清单。
- 日志：实际合同/授信写入后按类型查询；合同日志分页1/2无重叠，第二页详情→业务资料跳转通过。另用B原生批准/办理夹具，真实资质上传及UPDATE、CONFIGURE产生CONFIG_COMPLETE、CANCEL产生CANCEL_ACCESS后，验证分类、供应商范围、安全摘要、真实页面详情→关联资源及资质导航。列表/详情不暴露file_ref、原始快照或Key。配置完成/取消仍复用既有逐条授权及幂等实现，不重造审批。
- 清扫：`TestAttachmentCleanupDaily44`使用同一个日常44库和临时文件目录，真实合同/资质引用保留，48小时前的未引用文件删除，刚上传的未引用文件保留。F12在完成结构检查并列出候选后关闭专用真实连接池，引用查询失败，删除数0且文件保留；资质真实数据库连接失败同样不删。未影响运行API的连接池。F12写入后的最终授权失败清除刚上传文件。

这里测试了实际清扫服务，不宣称模拟了操作系统崩溃、全量重启故障或磁盘损坏。F12独立上传成功但资料保存失败的文件保留用于重试，超过24小时仍未引用才可清扫；资质上传和资料登记为单个业务动作，失败即清除新文件。

### 验证入口与清理

- `tests/a-f12-closeout.daily.browser.mjs`：`f12`、`fault`、`recover`、`logs`、`qualification-fault`均通过，真实Edge/Playwright、真实登录、无route mocks、pageErrors为空。
- `tests/supplier-v44.daily.browser.mjs`新增`closeout-logs`通过；测试报告事实使用明确QA产物，未调用Inspector。
- Docker Go：`TestF12` API组、`TestF12Attachment|TestDeleteF12`存储组、`TestSupplierQualificationFiles|TestSupplierQualificationMetadata`领域组、`TestAttachmentCleanupDaily44`两子场景通过；前端`npm.cmd run typecheck`通过。
- 可重复准备/精确清理入口：`cmd/server/a_closeout_test.go`，依显式manifest和QA标记操作；真实故障注入只命中该QA主体。原生夹具清理同时补齐资质操作日志删除。
- 最后核对：schema=44/false；标记供应商、标记业务日志、故障trigger/function、两组QA合同、资源、资质及test_job均0；standard_model保留原1条。两个私有附件目录文件均0；API运行、RestartCount=0。仅原两个BSS容器，其他项目容器未操作。
- 截图/checks在前端忽略目录`.qa/a-closeout-20261027/`及`.qa/supplier-v44-20261027/`，manifest/校验SQL/临时二进制在后端忽略目录`.qa/a-closeout-20261027/`；均不上传Git。回归测试源码应随业务代码交接，没有新增results.md。

剩余边界：客户F12身份/授权与流程映射、D07/权威资质审核、真实Inspector、供应商合并执行仍未因本轮完成而验收；主体切换/慢请求等页面竞态没有在本轮扩展重跑。

## Stage203：供应商管理列表税率显示修复（2026-10-27）

- 问题：`SupplierWorkspace.vue`直接在API比例字符串后拼`%`，`0.0600`被显示为`0.0600%`。
- RED：已有`supplier-management.browser.mjs`补充比例夹具及实际表格税率单元格断言，修复前失败，actual=`0.0600%`、expected=`6%`。
- GREEN：列表改用`formatSupplierTaxRate`，通过Decimal乘100后格式化，避免浮点尾差；不修改API/数据库保存单位、成本或毛利计算，其他详情页面不在本次改动范围。
- 定向测试`tests/supplier-tax.test.ts`两组通过，覆盖`0.06/0.0600→6%`、`0.0625→6.25%`、`0.29→29%`、`1→100%`、空值/null/undefined→`—`、`0/0.0000→0%`。
- 供应商管理Playwright/Edge浏览器回归通过，包含新增税率断言、四页签导航、筛选返回及原有主体维护场景；这是接口夹具页面验证，不冒充真实数据库写入联调。`npm.cmd run typecheck`通过。
- 本次只修改前端显示及测试，没有新建环境、修改迁移、写日常业务数据或提交推送；既有工作区改动保留。

## Stage204：门户前段、提交门槛及资源正式接管（2026-10-27）

### 确认与范围

用户明确采用DEC-020/021：至少一行有效连通通过即可提交，压力测试不强制；保留B草稿物理测试资源，批准后由A接管并防重。旧批次门槛及禁止草稿写resource的记录由这两个决定替代。逐资源自身双测、正式配置、模型资料/报价保护继续保持。本次不重写B测试平台/申报后端，不调用Inspector。

### 定向TDD修复

1. 真实门户草稿保存409后原来只有报错，缺少恢复入口。实际HTTP并发复现后，增加“刷新最新版本并保留草稿→核对服务器说明→按最新版本重试”。保留用户系数、说明与凭据输入，轮换新提交意图；服务端模型行集合变化或已不可编辑时拒绝直接覆盖并保留输入，需重新核对。
2. 提交响应200后，路由守卫因submitting仍为true阻止回申报记录。真实浏览器复现waitForURL失败；成功回执后固定已保存基线、关闭编辑和提交中标志，再导航，避免假未保存提示或留在可重复提交页面。
3. B的测试专用resource尚无采购人/正式控制快照，A读取原来返回不一致错误并影响列表。真实日常库测试先失败，再在A查询中排除“关联申报行且无RESOURCE_REVISION”的物理分配，批准后重新可见；不伪造采购人、revision或准入资格。既有PG临时表测试夹具同步包含原生submission_row结构。

### 实际验证

- `supplier-portal.daily.browser.mjs`的draft阶段：真实供应商登录→新增并保存草稿→从草稿列表重开→Key不回显→另一请求更新版本→当前保存409且输入保留→刷新最新版本并保留草稿→重试200；Escape取消离开仍保留输入，放弃离开不改已保存数据；标记的待审批申报实际撤回，最终WITHDRAWN。无route mocks。
- submit阶段：使用B实际CreateJob/AttachTestJob/CompleteJob持久化一份明确QA baseline通过报告，没有stress。页面可提交；编辑说明后实际保存+提交200，进入PENDING_APPROVAL并跳回申报记录，无错误离开弹窗。为验证导航修复，仅把本轮未批准的标记QA申报重置为DRAFT后重放一次；此为夹具准备，不代表真实撤回会自动变成草稿。
- evidence阶段Go：实际B EnsureTestResource重复调用复用同一个ID；A正式列表total=0、详情NotFound。随后仅准备真实B存储格式QA连通报告，没有真实Inspector调用或伪造A准入事实。
- adopt阶段：实际B领导认证/HTTP批准，接管同一ID；A资源列表从0到1，详情200及实际供应商模型页面可读。相同已批准请求重放200/同结果，改用当前不同依据的重复请求409；独立Go核对物理resource=1、RESOURCE_REVISION=1、origin_row_id为本次草稿行。缺压力及配置，quote_readiness仍false。
- 报告由实际B仓储写入，但内容是明确标记QA产物；不据此宣称Inspector配置/出站/回调/轮询完整通过。
- Docker定向Go：`TestPortalDraftResourceDaily44` evidence/adopt通过；存储`TestResourceProjectionEquality|TestResourceReadRejects|TestResourceReadQueries`、领域`TestResourceReader|TestResourceSnapshot|TestResourceProjectionRejects|TestResourceDraftReport`、API `TestNativeFlow|TestResourceBatch|TestSupplierSubmission`通过。早期无匹配测试的探针不计结果。前端typecheck及10条主体/Key相关Node回归通过。

### 部署、清理与剩余

日常PG原先停止导致API重启，已恢复原PG/API；没有创建新环境。当前运行API已部署正式资源读取修复，5173使用当前源码。44/dirty=false，本轮标记供应商、资源、申报、测试任务等已用原生QA清理器按供应商/资源精确清理，保留原标准模型1条；旧未提交工作区不动。测试源码保留，截图/checks在前端忽略`.qa/portal-daily-20261027/`，manifest/临时构建在后端忽略`.qa/a-closeout-20261027/`，不入Git。

Inspector、共享测试配置真实provider、客户F12、供应商合并/网站资料及门户完整注册附件仍按各自范围推进。本阶段只验上述确定的门户及A接管链路，不宣称F8/整个A范围全部完成；未提交推送。

## Stage205：团队v43与A的v44/v45迁移对齐（2026-10-27）

### 编号与升级

远端main的`000043_supplier_subject_link`为团队已提交迁移，必须保留。A原本尚未提交的`000043_f12_records`改名为`000044_f12_records`，原`000044_supplier_metadata`改名为`000045_supplier_metadata`；down中的版本提示同步，DDL不变。当前45组up/down配对且数字编号无重复，团队43两文件Git内容哈希与origin/main一致。Stage200/201记载的43/44是当时本地编号，不覆盖历史事实；新部署以本阶段及[升级路径](../supply/database-v45-upgrade.md)为准。

升级前日常库为旧本地44/false：F12和供应商元数据已经存在，但缺supplier.subject_id。不能直接跑新44/45重复建列，也不能只强制更改版本号。本轮在原model_bss库用临时表/事务回滚验证团队正常43→44→45 DDL及主体回填、唯一/CHECK约束；再对本机旧44补齐做真实结构事务预演并回滚，没有创建其他库或容器。

随后暂停原API，取得最终备份，在单事务内使用一次性补齐脚本：限定旧44/clean结构，拒绝歧义主体匹配，原样执行团队43，检查三组结构及21张业务表行数/内容摘要，再将版本更新45并COMMIT。supplier摘要只排除允许回填的新subject_id。完成后恢复原API；业务摘要未变化。正式脚本及结构断言保存在`docs/Work_A/supply/sql/`，不用于团队正常43/44库。

### 升级后定向验证

- 只读事务结构断言通过：supplier主体关联；F12 17个元数据列；supplier_alias及资质六列；唯一索引键/谓词、外键和已验证CHECK。查询确认model_bss=45/false。
- Docker Go使用现有API容器，更新五处测试数据库版本门槛；gofmt -d无差异。显式保留兼容opt-in `BSS_F12_V43_TEST=1`、`BSS_SUPPLIER_V44_TEST=1`，执行 `go test ./internal/infra/supplypg ./cmd/server -run '^(TestF12Daily43SchemaCapability|TestSupplierDaily44Capabilities|TestDailyRuntimeWiresNativeF12)$' -count=1 -v`，三项均PASS，不计跳过用例为通过。文件/变量中的历史版本不是运行库版本。
- 在已经45的日常库重试一次性脚本，按预期在前置DO抛出“requires ... v44/clean”，未执行后续DDL；再次查询45/false，防止误重跑。
- API `/healthz` HTTP200；model_bss_api_latest、model_bss_pg均running、RestartCount=0。前端原来未启动，已用现有Vite依赖在5173启动，HTTP200；没有新建Docker环境。
- 最终contract/credit_line/supplier/supplier_qualification均0行，standard_model原1行保留。此轮没有添加业务QA资料，不重复此前全部页面写入联调，也未调用Inspector。
- git diff --check通过（仅已有CRLF提示）；分支仍work-A-fix，HEAD a7c5ef4；origin/main已取到1fe8907但完整代码尚未合并。保留全部既有A改动，未提交、推送、重置或改写历史。

### 备份、文档及限制

最终升级前转储为后端忽略目录`.qa/schema-v45-20261027/model_bss_before_canonical45_final.dump`，SHA256=`146C4246DBA47CF0E6B8F858BA5FE494749D344D1C6FC250B1ACAE4241CBB187`；目录解析通过，较早转储还做过结构解析，未做完整恢复演练。原附件私有目录继续保留且不入Git。不能用正常45→44 down冒充恢复旧本地44；实际恢复须停写、保留新增资料、整体恢复旧备份与匹配的旧迁移目录/附件，细节见升级路径。本轮未执行down/restore。

Work_A三份索引、交接清单、公共接入、F12/曾用名/资质结构申请及文件部署说明已补充当前编号和历史区别；不新增results.md。QA脚本/终端输出/备份仍在忽略目录。上一轮Go验证曾因自动审批服务用量限制未执行；本轮重新通过正常审批后，以上三项已实际执行通过，限制已解除，不把上一轮拦截算作测试失败。
## Stage206：最新main账户设置合并及A改动保留（2026-10-27）

### 合并方式与冲突处理

通过12000代理重新fetch main，远端仍1fe8907（PR9供应商账户设置，前置130bdf2），比原HEAD a7c5ef4多2个已有远端提交，改动24文件。真实索引无暂存项；先用独立临时索引保存完整本地文件树并预演三方合并，文本冲突0。重叠8文件，其中6个共享文件须合并双方增量，另2个团队v43 up/down与远端内容一致。

共享文件为main.go、router.go、config.go、env.go、配置示例及后端.gitignore。保留main的账户服务/受保护路由/上传目录与忽略规则，并保留A的运行装配、配置和QA/私有目录忽略。A之前的迁移重号已在Stage205解决：团队43主体关联、A的44 F12及45供应商元数据，不再另造迁移。

为避免ff覆盖脏文件，保存相关文件原字节及全工作树Git对象快照，核对预演后没有新改动，再仅暂存还原受影响文件、移开已存在的两份同内容团队文件，执行git merge --ff-only origin/main，再将三方合并结果恢复至工作文件。没有stash或新建业务commit，没有git reset/rebase/推送。随后以独立索引重新收集完整文件树，其Git树ID与预演结果完全一致，验证其余全部A改动和新增文件没有遗漏。最后只格式化main.go/router.go并更新本文档。

本地HEAD已1fe8907，仍为work-A-fix，跟踪origin/work-A-fix不变；与origin/main无提交差异，但A未提交改动继续保留。git ls-files --unmerged为空，真实暂存区为空。临时索引、共享文件字节备份及manifest在后端忽略目录`.qa/main-merge-20261027-172116/`，不入Git，不新增环境。

### 受影响验证

- Go在现有API容器编译四包并运行15个顶层定向用例（不含子场景），全部PASS：账户字段规范/DTO/锁定/分页参数、仓储构造/锁定/空名、A原生flow及批量缺依赖、日常45库F12/供应商结构能力、配置Load及F12和资源绑定/接入运行装配。不是全量回归。
- 命令：`go test ./internal/api ./internal/infra/supplypg ./internal/infra/config ./cmd/server -run '^(TestNormalizeAccountUpdate.*|TestBuildSupplierAccountDTO.*|TestPositiveIntQueryBounds|TestCurrentRegionLocked.*|TestNewAccountStore.*|TestFirstNonEmpty.*|TestF12Daily43SchemaCapability|TestSupplierDaily44Capabilities|TestDailyRuntimeWiresNativeF12|TestDailyRuntimeAssemblesBindingAndAdmission|TestLoad|TestNativeFlow.*|TestResourceBatch.*)$' -count=1 -v`；显式设置原日常库相关opt-in，不调用Inspector。
- `go build -o /tmp/bss-main-merge-check ./cmd/server`成功，完整服务端可构建；产物仅在现有API容器的/tmp，没有替换正在运行的服务二进制。
- `npm run typecheck`通过；`node --experimental-strip-types --test tests/supplier-account.test.ts tests/supplier-tax.test.ts`共10项通过。
- `npm run test:browser:account`通过：账户锁定/可编辑、保存留痕、必填拦截、操作记录与详情；为新main自带的MSW接口fixture浏览器回归，不冒充真实账户写入/上传验收。临时Vite随脚本结束关闭，日常5173仍独立运行。
- 再次只读核对数据库45/false；本轮无DDL或业务数据准备。差异空白检查通过，未提交、推送或改写历史。

### 运行和后续边界

合并源码及编译通过不等于已部署。当前model_bss_api_latest仍运行Stage204的既有业务二进制，当前数据库已Stage205的45版；本轮只同步容器内编译源码与检查产物。要验收main新增账户设置真实接口，须再更新运行API并核对门户上传目录持久化；新main提供的账户身份解析opt-in真库用例需要非空身份数据，本轮未运行，不以跳过计通过。A此前完成的业务场景保留原证据，不随main同步重新全部勾选。

## Stage207：交接、结果及迁移说明一致性核查（2026-10-27）

本轮只更新文档和做静态核查，没有执行Go/浏览器/数据库测试、迁移或运行API部署；不新增业务通过结论。现状引用Stage205/206：日常45/false，main@1fe8907已合并，运行API仍为Stage204二进制。原A代码及未提交迁移保持，未新增业务提交或推送。

### 修正内容

- 将Work_A主入口、supply索引和TDD索引整理为单一当前状态，移除重复的“最新Stage200/201”等入口摘要；历史执行结果继续保留在本SUMMARY，不用当前版本覆盖原测试正文。
- 主交接、验收清单和两份正在使用的供应商/F12、A/B计划补当前勾选及边界，旧环境/旧提供方待交付描述明确为计划建立时快照。补充运行API未更新合并构建、main账户设置上传目录/真接口待验，避免源码可编译被当成实际部署。
- 更新F12、供应商契约和v36兼容读取前言、当前提供方/部署表；供应商授权/齐备及资质/F12结构不再等待他人补齐。客户F12权威映射、D07/权威审核、真实Inspector及供应商合并执行仍按原边界，Inspector继续用户暂缓。
- 升级说明链接真实43/44/45 up/down，增加团队正常43起点逐步`migrate up 1`示例、禁止force代替DDL、旧本地编号识别及回退限制。A产出仍在docs/Work_A，没有在migrations内复制报告或新增results.md；迁移SQL本轮未修改。

### 实际静态检查

- 16份当前交接/索引/契约/部署文件的204个项目相对文件链接可解析；外部网址和项目外绝对路径不属于该检查，不据此宣称原型/外部资料当前可访问。Markdown标题锚点未做全面验证。
- migrations共45份up、45份down，数字版本无重复、逐文件配对，旧000043_f12_records/000044_supplier_metadata冲突文件不存在。团队43的Git规范化内容哈希与origin/main一致，git diff为空。
- 原始字节哈希最初误报团队43不同，经git ls-files --eol确认仅Windows工作文件CRLF与索引LF差别；改用git hash-object --path规范化核对后通过，没有为消除换行差异改写迁移。
- git diff --check通过；.qa、.private、TDD/evidence仍被忽略，没有新增分散的*-results.md。该检查不表示所有未提交文件已纳入Git，A的v44/v45仍须随后续实际交付提交。

后续优先更新原日常API并核对MODEL_BSS_UPLOAD_DIR持久化，再验main账户设置真实认证读写/上传。其余待验按当前handoff-acceptance和主交接执行，不因文档整理重跑全部历史用例或新建环境。

## 2026-10-26：模型页面真实联调历史记录归并

以下为原独立验收记录，保持原执行日期/环境/限制，不表示新验收或当前运行状态。旧测试环境已经清理，复跑说明仅作历史追溯；后续按当前日常环境适配，不自动重建旧环境。

日期：2026-10-26。范围：本轮 F1/F3 配套后端及实际前端页面。

已通过 Playwright 驱动 Edge，完成真实登录 → 当前完整 Vue 应用 → Vite HTTP 代理 → 当前工作区 Go API → 专用 PostgreSQL 的联调。两个浏览器脚本均成功退出，未拦截或伪造 API 响应。公共布局在手机宽度溢出，单独保留为未解决项。

## 环境与数据

- 独立 API：`bss-a-models-live-20261026`，仅本机 `127.0.0.1:18083`，从工作区编译，后台任务关闭。
- 专用库：`model_bss_a_schema34_20261026_models_backend`，使用既有隔离 PG 容器和预备表结构。没有执行迁移或修改日常 `model_bss` / 8080 API。
- 初始数据：25 个带官方资料版本的模型、25 个供应商、49 条供应来源、3 份持久报告、103 条标准模型日志。主模型有25条来源，另24个模型各有一条。日志与模型ID超过2^53。维护脚本另建了测试模型。
- 隔离测试账号使用随机口令，本机忽略目录中的私有配置不进入测试源码、文档或输出。
- 资源、报告和控制日志是明确的隔离测试数据，不代表B已实际完成申报、测试平台调用或准入。本轮验证A读取和呈现持久化事实的能力。

## 验证结果

| 真实操作 / 检查 | 结果 |
|---|---|
| 页面新增待补齐模型→补齐核验→移除输出价格 | 三次写请求200；版本1/2/3为INCOMPLETE/VERIFIED/INCOMPLETE；核验人仅版本2有值；价格项0/2/1；当前报价重新阻断 |
| 历史版本和逐模型日志 | 三个版本、三条MANUAL_SAVE，数据库只读核对一致 |
| 全局日志跨100条分页、大ID | 浏览器实际请求第1/2页，无截断；日志/对象ID为字符串 |
| 当前模型已改名，查看首次日志 | 显示当时名称，before=null，after为当时资料，不用当前名称替代 |
| 原始日志中注入未知api_key/password标记 | SQL确认标记存在；详情响应和页面快照不包含标记；更新日志前后版本连续 |
| 25个模型grouped分页、聚合排序 | 页20+5；每次只取当前页，不请求模型列表拼接；主模型资源数/供应商去重数25，可报价数0 |
| 供应商/公司多选、区域、关键词 | 同维度OR、跨维度AND；区域不匹配返回0；接口模型ID可搜索；统计范围和系数区间同步变化 |
| 25条供应来源flat分页 | 页20+5；第2页应用区间回第1页 |
| 零值与组合区间 | 耗时上限0正确命中一条，缺指标不当0；系数/RPS/TPS组合区间先筛选再聚合，大数边界不丢精度 |
| 独立测试报告 | 真实列表/详情、连通耗时/RPS为0、TPS120000、缓存命中率0%，归属一致 |
| 上架时间 | 升降序均有值在前、空值末尾，缺时间显示未记录 |
| 错误与权限 | 匿名401、供应商403、跨模型日志404、非法ID/多选/区间/排序400；过期revision409且当前版本不变 |
| 第二名员工登录 | 维护表单只读，无保存按钮，直接写请求403 |
| 返回上下文与浏览器 | 筛选返回保留；最终主场景44条观测请求无5xx，两个员工页面均无pageerror |

维护脚本最终模型为 `9007199254741026`，真实SQL确认三个资料版本和三条操作日志。数据证据还确认49资源、25模型组、25供应商和一条耗时/RPS/缓存命中数均为0的报告。

## 发现并修复

真实筛选选项最初返回503：A运行与模块装配使用 `NewResourceReadStore`，没有包含 `ResourceFilterOptionsStore` 能力。两处改为已有 `NewResourceRepository`，保留原事实读取，补上选项能力。改动仅在 `cmd/server/supply_runtime.go` 与 `internal/api/supply_module.go` 的A装配位置；公共主路由、C通用日志和B办理/报告逻辑未改。

新增 `TestSupplyRouteModuleResourceOptionsUsesConfiguredStore`。修复后重启本轮独立API并完整重跑浏览器场景成功；A领域/SQL/HTTP/装配相关定向回归、服务与测试夹具编译通过。

## 复跑与证据

- `model-pricing-web/tests/standard-model.live.browser.mjs`：真实新增、维护、核验、版本、日志。
- `model-pricing-web/tests/model-pages.live.browser.mjs`：真实日志详情、广场/来源分页、筛选、报告、错误、只读权限。
- `model-pricing-backend/tests/modelpageslive/main.go`：专用空白库测试数据生成器。限定上述数据库名，要求测试口令，检查空白账号库，在一个事务中播种；不是迁移或生产种子。

在有同版结构的独立测试库准备环境变量后执行。账号口令只通过环境变量传入；生成器输出不含凭据的manifest。已有测试库不应重复播种。

```text
后端生成数据所需：PGHOST / PGUSER / PGDATABASE / PGSSLMODE / BSS_TEST_PASSWORD
go run ./tests/modelpageslive > manifest.json

前端所需：BSS_TEST_LOGIN_ID / BSS_TEST_PASSWORD / BSS_TEST_API_ORIGIN
BSS_MODEL_PAGES_MANIFEST（模型页面脚本需要，指向上一步manifest）
node tests/standard-model.live.browser.mjs
node tests/model-pages.live.browser.mjs
```

本机证据位于 `model-pricing-web/.qa/model-pages-live/`：`maintenance-results.json`、`results.json`（七组检查、请求参数/状态/request_id、限制）、`database-evidence.txt`及维护保存、历史日志、非维护人只读、筛选、分页、报告和手机截图。早期失败证据保留，最终结论以成功生成的results.json和退出码为准。

## 限制

390px完整应用中，公共固定侧栏仍占190px，顶部账号工具栏右边界540px，整页横向溢出。A表格158px容器内可横向滚动（内容893px）。按A边界保留此公共问题，未改 `AppLayout.vue` 或全局样式，不能宣称整站手机适配通过。

未验Inspector实际执行、B申报/准入办理、C报价写入及规模性能。定向回归通过不代表既有公共路由测试失败已修复或全量回归通过。没有迁移、提交、推送或部署日常API。

验收结束后先注销本轮测试会话并停止临时服务；随后按用户明确清理要求，核对容器ID、专用库测试数据、无其他容器引用及无活动连接，已删除本轮 `bss-a-models-live-20261026` 容器和 `model_bss_a_schema34_20261026_models_backend` 数据库，查询确认均不存在，并移除本机测试口令私有配置。可复跑源码、验收记录、结果JSON和截图保留。共享测试PG、18081/18082及日常8080/5433环境未删除或停止。


## Stage208：Work_A文档归并与一次性SQL本机归档（2026-10-27）

按用户要求暂停提交推送，supply的48份Markdown收敛12份，TDD的17份收敛6份。接口/字段/权限/版本/事务按F1/F3/F4/F12/F13归入模块契约；DEC保留编号和覆盖关系；当前状态只维护交接与验收清单，原型和需求各一份。早期计划提炼到regression-scenarios，独立模型页面真实联调原记录并入本SUMMARY并保留执行日期/环境/限制，没有重复跑测试或把历史结果改为当前通过。

原文和迁移43/44/45副本按SHA256核对后保存在本机后端忽略目录.qa/docs-consolidation-*，.qa/docs-consolidation-current.txt记录位置。supply/sql的两个脚本仅用于本机旧44升级/断言，已从交付目录移除并保留在该原文快照内，不参与应用启动和团队正常迁移；正式migrations未改动，不新增文档archive层或分散results.md。

最终静态核对：19份交付文档中的118个项目内文件链接、49个有效目标锚点通过；当前链接检查未发现遗留SUMMARY锚点缺失，但没有重新证明每个历史引用的语义。68个原始文件按SHA256逐一核对留档，45组迁移数量/编号及43～45原字节未变化，HEAD与真实暂存区保持；git diff --check通过。源码、数据库、运行API与Git分支均未改变；未提交、推送或改写历史。

## Stage209：A增量分支交付范围（2026-10-27）

用户重新授权本轮提交并推送work-A-fix。交付包括A的标准模型/资源广场/工作台接线、供应商及F12业务、门户前段、可复跑测试源码、正式000044_f12_records和000045_supplier_metadata，以及归并后的Work_A文档。保留团队000043_supplier_subject_link，基线main@1fe8907已合入；使用正常提交/推送，不改写历史或自动合并PR。

验收沿用实际记录：标准模型/工作台及原生办理、配置/接入/改价见Stage194～198；供应商F12/资质/批量/日志/附件见Stage200～202；门户与批准接管Stage204；数据库45升级Stage205；合并构建/定向Go、类型/Node与账户设置fixture Stage206。Stage207/208为文档静态核查，不升级为新的业务联调结论。

本轮提交前确认远端work-A-fix没有新增分叉、main仍1fe8907；归并后文档链接/锚点、备份及迁移库存检查通过，差异空白检查通过。排除本机.qa、.private、TDD/evidence、依赖/构建物、转储和真实附件，不交付一次性本机升级SQL。具体提交SHA与远端结果以Git记录和本轮回执为准，不将计划推送写成已部署。

仍待：运行API更新为合并构建及门户账户实际读写/上传；客户F12可信映射与写入；D07/权威审核、真实Inspector（用户暂缓）、供应商合并执行、F2增强和未覆盖的具体场景。PR描述须保留这些边界。
