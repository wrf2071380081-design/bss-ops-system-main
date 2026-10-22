# A 模块 TDD 汇总

> 当前交接入口（2026-10-22）：[责任与接入参数](../supply/2026-10-22-handoff.md)、[交接验收清单](./2026-10-22-handoff-acceptance.md)。Stage59 已用 Go 1.26 编译当前后端、通过相关定向测试并更新本地 API 容器；真实计划执行/提醒、配置密钥写入和 B 流程联合验收仍待完成。以下各阶段保留当时结论，旧“未装配/运行库32”等描述不代表当前源码状态。交接文档中的待验场景不等于已执行通过。

> 职责更正：总览/A任务书/共享契约只有A/B/C三条线，没有独立公共负责人；共享文件统一维护未点名全部由谁负责。A负责F4/F13等业务闭环，C明确提供F14日志/绑定helper。历史阶段的“待公共负责人”只保留当时表述，不能据此移交A责任；最新交接清单已校正。

## 2026-10-21：F1 接入适配与 36 版结构验证

- 运行库 `schema_migrations` 只读核对为 `36 / dirty=false`。新组装验收连接专用的 schema-only 测试库，未对运行库执行写入或迁移。
- 按 TDD 补齐 `GORMModelTxFromContext`，将公共幂等中间件注入的 GORM 事务解析为同一 `*sql.Tx`；新增 `AppendModelOperationLog`，在同一事务写公共 `operation_log`。
- 新增 `NewStandardModelSupplyDeps` 作为 A 的 F1 依赖构造入口，检查幂等仓储与 A SQL 仓储使用同一连接池，并构造标准模型读写服务；不改公共 `main.go`、主路由或权限装配。
- `TestStandardModelModuleCommitsBusinessLogAndIdempotencyTogether` 在 Go 1.26 容器中针对隔离库通过：创建投影、不可变首版本、操作日志和 `DONE` 幂等结果同时落库；同键重放不重复写版本或日志。
- 前端本轮 `npm test` 99/99 通过，`npm run build` 通过（Vite 报告现有大包体积提示）。标准模型专用浏览器脚本因项目环境缺少 `playwright` 包而未启动，不能据此算作交互验收。
- 当前未完成公共 `registerSupplyRoutes`/`api.Deps` 装配及真实认证、页面路由和运行容器联合验收。公共负责人完成入口接线后，再以 Docker 实际 API 做联合验收；本节结果只证明 A 注册器和适配器的真库组合。

## 后续本地阶段：计划实际完成与BLOCKED提交（2026-10-21）

契约：[计划存储与执行](../supply/plan-storage.draft.md)。新增plan_execution.go、原生组合执行测试，并扩展计划/资源控制封套保存完成依据与来源事件；本节为未提交实现。

- 红阶段实际执行：执行服务接收NUL/非法UTF-8依据的反例失败，修复后统一拒绝，回执与动作时间统一UTC微秒。真实创建/接入/核验链上的计划测试在执行工作单元为桩时未拒绝他人、旧版本及缺失效果，且返回空执行结果；实现后同一反例通过。另加BLOCKED详情应显示RESOURCE_NOT_READY反例，先失败，再补PlanReadRecord.ExecutionFailureReason到安全BlockedReasons投影后通过。
- PlanExecutionWork借用PlanStore提供的外层事务/日志/outbox，锁供应商→资源→计划，ONLINE还锁模型并经真实ResourceReadStore/所选报告/正式配置组合读取准入事实。OFFLINE只复核资源和原始计划基准，不因当前不可报价而阻止下架。所有实际提案在写适配中再次按领域规则核对，不接受页面自报通过。
- 成功原子写资源状态/revision、原生DONE计划、不可变PLAN_REVISION与RESOURCE_REVISION、RESOURCE_ONLINED/OFFLINED事件及RESOURCE_CHANGE公告。计划和资源日志含plan ID/revision、完成依据和真实source_event_id；公告引用同一事件，影响摘要PENDING且数量未知，不修改客户关系。旧提醒由当前revision/status核对失效。
- 扩展TestResourceEvidenceAdmissionChainPostgres34的plan_execution子场景，复用真实F1/资源/报告/配置：未来计划实际下架→OFFLINE/revision6→新上架计划→ADMITTED/revision7。错误人员、旧计划版本、空日志、空事件回调、notice真实SQL CHECK失败均拒绝，外层回滚后仍为原ADMITTED/revision5，成功时只一条对应事件/公告；重复实际完成409。压力测试实际失败时ONLINE得到BLOCKED，资源仍OFFLINE，没有新资源公告；回滚该反例后恢复真实通过报告，上架成功。
- TestPlanConcurrentCreationPostgres34扩展真实外层提交：创建后的资源发生转交/revision变化，原发起人执行得到Conflict=true及nil error，外层COMMIT前另一连接仍见PENDING，提交后通过独立RR读取见BLOCKED/revision2并保留RESOURCE_CHANGED/完成依据；没有资源变更事件。此证明A事务结果可提交，不代替公共幂等HTTP提交后409响应验收。
- 新鲜验证：四包go test -count=1、同四包go vet及全后端go build ./...退出0；专用库显式运行14个顶层F1/F3/F4/计划原生SQL/并发测试通过。普通无专用库回归仍跳过原生SQL。测试独立schema清理，运行库未迁移。
- 未完成：执行HTTP的公共幂等提交/响应装配、生产B报告与公共配置提供方、实际页面办理入口和Docker联调；can_execute继续false，未对外注册执行路由。后续优先核对F1/F3主装配与原型页面接入，再联合验收工作台任务，不能把A受控组合称为全系统完成。

## 后续本地阶段：计划原生读取与维护（2026-10-21）

契约：[计划存储](../supply/plan-storage.draft.md)。新增plan_store.go及依赖/原生维护/双事务竞争测试，复用现有plan.before/after与operation_log，不添加公共迁移；本节为未提交业务实现。

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

契约：[证据/登记接入与事件](../supply/resource-admission-storage.draft.md)。新增 `resource_test_evidence.go`、`resource_admission.go`、共同资源锁 helper、完整组合/并发测试及依赖单元测试；本节为未提交的本地业务实现。

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

契约：[正式配置原生34存储](../supply/formal-config-storage.draft.md)。新增 `formal_config_store.go`、`resource_config_binding.go` 及单元/真库/并发测试，复用现有34表。本节为未提交的本地实现，不属于文档清理PR。

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

契约：[控制快照存储](../supply/resource-control-storage.draft.md)。新增 `resource_state_store.go`、`resource_creation.go` 及对应单元/真库/并发测试；本节为未提交的本地业务实现，不属于文档清理PR。

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

契约：[F3 PostgreSQL 读取](../supply/f3-postgres-read.draft.md)。新增 `resource_read.go`、`resource_repository.go` 及对应测试；本节为本地未提交实现，不属于文档清理 PR #5。

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

契约：[官方来源策略](../supply/official-source-policy.draft.md)。新增真实 `OfficialSourcePolicy` 实现，替换 F1 原生写测试中的来源校验替身。

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

本节为尚未提交的新实现，不属于文档清理PR #5。新增 `internal/infra/supplypg/selected_report.go`，契约见[选定报告读取](../supply/selected-report.draft.md)。复用34原有报告、任务和资源字段，不修改表结构，不新增生产路由。

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

公共幂等中间件当前把4xx标记为FAILED，幂等重放对DONE固定包装为HTTP 200。故本轮仅验证首次BLOCKED响应为409且携带计划，不宣称409响应已被共享幂等仓储保存或可重放。公共 `registerSupplyRoutes` 为空，主路由尚未注入 `PlanExecutor`；在公共支持409终态响应快照并完成主装配前，`can_execute` 和前端实际完成表单保持关闭。交接要求见[公共接入请求第15节](../supply/public-integration-request.md#15-计划实际完成-http-与幂等响应)。

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

逐字段检查时，`resource.revision`及来源/目标/报告控制元数据由保留的`operation_log.after`中`supply.resource/v1`快照承载；正式配置状态、版本、核验依据由`FORMAL_CONFIG_REVISION`安全快照承载，区域从同一供应商行推导。它们是应用投影字段，不要求物理列。此前只看DDL列名便判断为“缺字段”是错误结论，已在[结构基线](../supply/2026-10-21-schema34-baseline.md)更正。本轮只读查询实际运行库再次确认`36|false`，实际列与仓库迁移一致。

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

## Stage 47：供应商申报前端最小闭环

补齐 A 负责的供应商门户前端空页：模型新增现在支持一份多行申报、标准模型安全目录选择、显式保存草稿、保存后的逐行连通性/压力测试启动与刷新、批次测试覆盖提示和申报提交；草稿/申报记录页签读取列表并开放可编辑草稿。测试状态由 API 返回，前端区分“做过测试”和“测试通过”；每行连通性已测且至少一行压力测试已测才开放批次提交。价格变更仍从模型管理页按资源及原 revision 发起，不直接改资源。

验证：`npm.cmd run build` 首次暴露两个模板事件的隐式 `any`，修正事件处理后再次构建通过。Vite保留既有 500 kB chunk 提示。没有运行完整测试套件，没有创建或修改业务数据。`127.0.0.1:8080/healthz` 返回健康、`127.0.0.1:5173/` 返回 HTTP 200；本轮 Docker CLI 仍因 Docker Desktop pipe `Access is denied` 不可检查，所以未确认当前 Docker 容器是否包含本轮前端源码。

真实供应商申报 API 尚未可联调：`internal/api/router_submission.go` 的 B 路由注册仍为空；`/supplier/models`、安全标准模型选项、草稿/逐行测试/提交与价格变更的接口尚未由真实提供方开放。详细字段和隔离要求已补入[公共集成请求](../supply/public-integration-request.md#17-供应商门户前端-api-接入)。旧单模型 `/supplier/model-applications` 未复用。源码与前端构建通过，不等于供应商门户页面/API真实可用。未提交或推送。

## Stage 48：F4 正式配置维护入口

模型广场资源详情的“接入与配置”页新增正式配置新建、修改、停用及资源绑定入口；Key 使用密码输入框，更新留空表示保留，页面只显示“已设置”状态，不回显已保存密钥。新增/修改要求明确确认外部操作，停用要求填写原因。修正列表状态契约，使前端接受后端 `ACTIVE/INACTIVE`，避免有效配置列表因 `DISABLED/INACTIVE` 不一致而报契约错误。

验证：`npm.cmd run build` 通过 Vue 类型检查与 Vite 生产构建，仍有既有大 chunk 提示；没有运行全量测试、没有写入数据库。当前运行装配仍未设置 `FormalConfigs` writer、生产 `SecretSealer` 与 `FormalURLPolicy`，所以新增/修改/停用请求仍应返回503。具体公共装配交接已写入[集成请求](../supply/public-integration-request.md#18-f4-正式配置页面装配)。本阶段证明界面与请求层可构建，不证明正式配置真库写入可用。未提交或推送。

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

新增[supply三模块协作交接清单](../supply/2026-10-22-handoff.md)与[TDD交接验收清单](./2026-10-22-handoff-acceptance.md)。按公共/B/C明确真实提供方、A接续实现、代码入口、参数和最小验收条件；将Stage54/56/57已通过范围与配置/准入/报告/执行/提醒等待项分开。同步修正入口、规划和核查清单中的运行库32、主路由未挂载、前端审批守卫未实现、运行PlanExecutor已注入等过时状态。

本次仅核对源码及现有阶段记录、补交接说明，不执行新业务测试、不增加“通过”结论，不修改功能代码/数据库/容器，也不提交或推送。交接后的真实结果继续追加本文件，不新增分散results或上传evidence。

### 同日职责核对更正

重新核读总览§〇.1/〇.2及F1–F17、A任务书、附01§2.2/3.2/6.1/10：项目是A/B/C三人；“公共统一维护”是共享文件协作，未指定独立第四负责人；C明确提供F14日志/绑定工具。已修正交接清单和当前规划：正式配置安全实现/接线、资源状态、F13执行/提醒由A继续推进，B交付真实流程/测试，C完成客户/报价及F14。热点文件修改仍按统一维护规则。F8按总览与A任务书由A做前端/B做后端，附01页面表及总览文件表的冲突已注明。仅更正文档，不改变业务规则、实现和验证结果。

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

先新增 [F12 v36 只读契约](../supply/f12-v36-read-projection.draft.md)，再实现供应商合同记录与授信历史查询：`GET /api/internal/contracts`、`GET /api/internal/credits`。读模型只投影 v36 原生列；合同不返回 `file_ref`/`note`，只显示附件引用是否存在。当前授信按主体、方向、币种和最高原生版本确定；同一最高版本有多条时标记 `AMBIGUOUS_VERSION`，金额非正时标记 `INVALID_LEGACY_AMOUNT`，都不猜测当前值。供应商主体页新增只读合同/授信面板，并明确显示旧表无法提供资料名、编号、附件下载或不可变版本历史。

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
