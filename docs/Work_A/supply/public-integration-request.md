# A 公共及跨模块接入需求

## 当前交接状态（2026-10-22）

**接手优先读[三模块交接清单](./2026-10-22-handoff.md)和[TDD验收清单](../TDD/2026-10-22-handoff-acceptance.md)。** 本文保留各阶段的接口细节，历史状态不代表当前进度；最新真实页面/API/隔离PG证据为SUMMARY Stage54、56、57，Go编译及当前API部署见Stage59–60。

- 已接入：F1正式页面/读写、F3资源读取、配置读取、公告个人已阅、计划读取/维护及模型级日志/具体计划导航。主运行库36/dirty=false，不再等待“32升级”或首次挂载基础路由。
- A继续完成：正式配置密文真实写入验收；绑定/准入的B事实接入与真实验收，报告/文件、批准改价适配；F13执行/提醒待Docker业务数据联合验收；F12已交付v36兼容只读投影，完整合同授信写入与附件还需按第19节推进。共享main/config/幂等/调度/工作台框架按统一维护规则协作，不另设公共负责人。
- 待B：来源行/流程节点/办理人/选定报告/已批准未应用变更的可信事实，报告历史/文件接口及批准改价来源。A需继续适配和补齐实际改价办理入口。
- 待C：F14日志/绑定helper、报价前重查A资格与资源变化事件幂等消费。供应商门户为A前端/B后端，F12业务归A，结构/附件协作另见H-X01。
- 当前源码运行组合已注入 `PlanExecutor`；只向原发起人的PENDING上下架计划开放执行。BLOCKED以提交后的409终态快照保存并同键重放；到期提醒worker按当前revision/status去重写公告；工作台仓储已投影具体plan_id。Go定向测试与当前API部署见SUMMARY Stage59；Stage60已设置本机正式配置密钥。密文创建、计划执行/重放和提醒投递仍需真实业务数据验收。

> 当前以[33–36结构映射](./2026-10-21-schema34-baseline.md)实施。F1无需先新增独立维护人/revision/核验列，原“缺字段”清单不再作为主装配前提；仍需真实认证、公共日志及幂等提交接入。本文下面的早期结构建议以当前基线修订。

初稿日期：2026-10-20；当前交接更新：2026-10-22。
范围：开发者A的标准模型、资源、正式配置、计划与公告及跨模块接入。依据总览/A任务书/共享契约，只有A/B/C三条线：A完成供应侧与F13业务，B提供办理/测试，C提供F14日志/绑定工具并消费报价资格和资源事件。共享文件要求统一维护但未点名独立负责人，不能由此转移A实现责任。本文不是迁移脚本，也不代表未验收依赖已经交付。

2026-10-21接入补充：当前跨模块规则见[需求结论与规划](./2026-10-21-requirements-and-plan.md)。优先接通标准模型库和模型广场，之后集成工作台。公共工作台默认本人；普通待办单人绑定，供应商报价共享审批候选事项单独聚合。报价模块需在一次审批与分派完成后提供后续任务，防重复决策/分派；A消费服务端可信办理事实。人员筛选、审批资格和业务记录操作权不得互相替代。

详情导航需同时保留资源/主体与具体任务上下文：计划包含plan_id，办理节点包含flow_id/node_key或稳定节点ID；业务目标页重新校验当前认证人员。公共`v_work_item`的PLAN以resource_id作为biz_id且没有plan_id。A仓储现从此视图读取非计划事项，并直接从原生`plan`表投影计划行及其稳定ID，避免以创建时间反查；该修复不改共享视图/迁移。办理节点的具体flow/node上下文仍由B提供，列表可见本身不能授权办理。

## 1. 当前可接入的 A 代码

F1 基础构造为 `api.NewStandardModelSupplyDeps(StandardModelSupplyOptions{DB, Schema, IdemStore, SourcePolicy, Clock})`。现已提供覆盖 F1/F3/F4/F13 的 `api.NewSupplyRouteModule(SupplyRouteModuleOptions{...})`：它构造全部A路由依赖，并向可信B接入方返回 `ResourceCreation` 与 `TestEvidence` 服务。两种构造都要求A SQL写入与 `IdemStore` 共用同一连接池；完整入口对身份、日志、密钥封装、URL策略、B proof/report/申报行/outbox及计划待办配置查询均无成功默认值，缺少提供方时返回明确依赖错误。公共主装配应调用完整入口，将 `module.Deps` 传给 `RegisterSupplyRoutes`，并把两个B调用服务接入真实流程。完整入口的构造测试及API包回归通过，见[TDD Stage 38](../TDD/SUMMARY.md#stage-38v36结构下的a模块组合入口)。此代码尚未接入公共 `main/Deps` 或运行容器；不能以替身参数代替真实身份、加密或B业务事实。

Stage39在隔离库model_bss_a_schema34_334c39验证完整构造器的模型/资源/公告/计划GET及F1业务/日志/幂等同事务与重放，临时数据已清理。该阶段证据不代替真实B提供方联调；当前基础运行装配已完成，完整剩余依赖由A交构造与业务适配、B交事实、共享main按约定维护，见当前交接状态。

2026-10-21最新补充：`supplypg.NewResourceRepository(schema, readRunner, factsLoader)` 组合完整资源读取与选项，注入 `SupplyDeps.Resources = supply.NewResourceReader(repository)`。readRunner可复用 `supplypg.ModelReadTransactions(db)`，事实提供方必须使用传入的同一sql.Tx。A控制快照已提供真实 `NewResourceStateStore(schema, logAppender).FactsLoader(reportLoader, configLoader)`；configLoader可注入 `NewFormalConfigStore(schema, configLogAppender).ResourceView`，B提供可信报告来源与目标版本。不能在装配中用当前原生行临时拼造已保存revision投影。见[F3读取契约](./f3-postgres-read.draft.md)及[控制存储与创建适配](./resource-control-storage.draft.md)。本地真实SQL/HTTP验证不代表主路由已启用。

公共日志适配需原样保存A的RESOURCE_REVISION安全快照，保持追加/保留，业务动作在JSON operation字段。操作记录展示应按业务动作显示中文文案，不能向用户展示存储保留标记。A创建工作单元加入外层事务并注入B LockSource/AttachRow/PrepareReport，锁序、来源版本、真实审批分派和草稿报告转存须由B真实实现后联合验收；A未实现另一套审批系统。

正式配置接入使用 `NewFormalConfigWork(configStore, resolveTx)` 注入既有FormalConfigService，同时提供真实SecretSealer与FormalURLPolicy；缺失不得默认成功。新内部供应商创建须按DEC-008在同事务保存明确初始归属记录，公共转交继续该记录链并共用supplier锁，不能让配置命令补猜归属。configLogAppender保留FORMAL_CONFIG_REVISION的安全View/指纹，不能写入密文或密钥版本。资源初次绑定可注入 `NewResourceBindingWork(state, configs, resolveTx, proofLoader)`；B提供当前节点可信资格，绑定与配置维护必须使用同一supplier锁。详见[配置存储及锁序](./formal-config-storage.draft.md)。

证据关联使用 `NewTestEvidenceWork(state, resolveTx, authority)`，authority同事务确认可信B调用及报告引用；A仍重读真实报告/任务。登记接入使用 `NewResourceAdmissionWork(state, configs, resolveTx, ResourceAdmissionHooks)`，必须注入B当前proof、选定报告来源、逐行完成CAS及公共outbox写入。outbox writer返回真实主键，A核对PENDING事件的类型/安全payload/请求号/人员，不能用空回调占位。事件采用supply.resource_admitted/v1，schema与字段见[事务/事件契约](./resource-admission-storage.draft.md)。以上服务仍无新浏览器写入口；外层提交后响应、真实幂等与B后端仍须装配。

F1人工核验可注入 `supplypg.NewOfficialSourcePolicy(map[string][]string)`，可信配置按公司给出精确HTTPS origins；不接受页面新增允许域名，不默认允许全部来源。没有真实配置前保留明确依赖故障，见[来源策略](./official-source-policy.draft.md)。

目录：`model-pricing-backend/internal/domain/supply/`。

- `ModelService.Create(ctx, WriteMeta, ModelData)`。
- `ModelService.Update(ctx, WriteMeta, id, expectedRevision, ModelData)`。
- `ModelService.Verify(ctx, WriteMeta, id, expectedRevision, checkedAt)`。
- `StandardModel.MissingFields()`、`Readiness()` 和独立副本 `Clone()`。
- `ModelUnitOfWork`、`ModelTransaction`、`OfficialSourcePolicy` 的准确 Go 签名见 `ports.go`。
- `ModelReadStore`、`ModelReader.List/Get/Versions/Snapshot` 见 `model_read.go`；读取查询不携带旧角色/维护人过滤，历史快照不用于新报价。
- HTTP 接入入口为 A 的 `internal/api/router_supply.go` 中 `RegisterSupplyRoutes(group, SupplyDeps)`；组前缀 `/api/internal` 并已有 AuthN/RequestID。当前依赖包括 F1 Reader/Writer、F3 Resources/ResourceAdmitter/ResourceConfigBinder、F4 FormalConfigs、F13 Notices/NoticeWriter/Plans/PlanWriter/PlanExecutor，以及 IdemStore。注册器会额外检查 INTERNAL/STAFF、使用StaffID；采购人、模型维护人、计划发起人、B当前节点办理人等资格仍须由各自可信服务事实校验。A注册器的端点不等于公共主路由已装配。

领域不引用旧 model_sku、supplier_profile 或旧审批/成本重算。A已提供HTTP注册入口、Swagger源注释及F1/F3 SQL适配，当前工作区已有公共router/main的基础接入改动；后续公共热点文件仍按共享归属维护。本交接不新增业务迁移或公共能力替代实现。

## 2. 目标 schema 需核对

| 对象 | A 需要的事实/约束 |
|---|---|
| standard_model | bigint ID、维护人、当前 typed ModelData 的字段映射、ACTIVE/INACTIVE、核验状态、核验人、版本/revision、UTC 审计时间 |
| standard_model_version | 完整资料和核验快照；唯一 `(standard_model_id,version_no)`，只追加 |
| 官方身份 | 有效 company/official_id 的唯一性；允许待补齐模型缺少身份资料时，应采用明确的 null/部分唯一策略，避免多个空草稿互相冲突 |
| 数值 | 官方 amount numeric(20,8)；单位数量为正，当前领域切片也限定同等输入精度；最终 DDL/API 单位数量精度需一致 |
| 乐观锁 | 数据库更新须包含 `WHERE id=? AND revision=?`；未命中正确区分404/409 |
| 操作记录 | 与当前投影/版本相同事务，保留实体、动作、认证人员、原因、request_id、前后快照和时间 |

领域内 `ModelData` 使用 OfficialFact/Ability/Source/PriceItem 的类型结构，不代表应把聚合的 `data` 包裹直接用作 HTTP 响应或新增同义临时表。请先固定目标 DDL 与 API 映射。

## 3. 事务端口要求

`Within` 必须加入调用方已有事务；只在没有外层事务时开启顶层事务。不能在幂等中间件事务内独立提交一份业务事务。

事务适配提供：分配 ID、锁定模型、创建/CAS 更新、追加完整版本、追加操作记录。任一业务校验、唯一性、快照、日志或 commit 失败时整体回滚；原错误保留给 HTTP 错误映射。

新请求无资料变化时只追加 UPDATE_NOOP / VERIFY_NOOP 操作记录，保留原版本和资料更新时间；幂等重放不重复执行。日志失败不得将无变化写入视为成功。

当前 `db.WithDB/FromContext` 是可复用的上下文基础，不等于事务和日志适配已交付。A 的内存事务测试只验证服务将这些动作放进同一个端口回调，不能证明 SQL 实现的隔离和回滚。

## 4. 官方来源策略

`OfficialSourcePolicy.Validate` 接收公司及所有模型级、价格项级来源。必须基于配置白名单和已有有效资料做本地检查；缺少策略时核验拒绝。

- 不能用“域名包含厂商字符串”判断官方。
- 不在持锁事务中抓网页、调用 LLM 或发远程 HTTP。
- 来源有效天数尚未确认，不内置任意过期阈值。
- URL 拒绝用户密码段及常见密钥查询参数；完整来源白名单仍由适配策略负责。

## 5. 认证、错误与幂等接入

- `WriteMeta.ActorID` 来自认证上下文；RequestID 来自请求追踪；Reason 来自业务操作说明。
- 新增不接受任意 maintainer_id，自动绑定创建者；更换维护人由公共转交接口处理。
- 领域错误分为 FieldError、ErrForbidden、ErrRevisionConflict、ErrIncomplete、ErrInactive、ErrNotFound、ErrIdentityConflict、ErrDependencyUnavailable、ErrSourceRejected。公共 HTTP 层负责映射，不在 A 领域自行分配数字 code。
- 写 HTTP 入口继续要求 Idempotency-Key，领域服务不另外实现一个幂等存储。接口幂等重放与真实事务接入后还需验证。
- 统一 errors[]/fieldErrors 结构及错误原因机器码仍由公共契约固定。
- 当前公共幂等同键异body返回400，与附01的409待统一；FAILED状态后的重试策略也需与前端“保留原键重试”对齐。
- 当前共享中间件在handler写响应后执行MarkResult/Commit；A应交付执行接入方案，与共享幂等层约定维护人共同验证/处理响应和提交状态一致性，必要时缓冲响应后输出。三份依据未点名该中间件维护人；A无库测试不能证明SQL提交失败时仍正确响应。
- 读取适配器需真实实现白名单排序、双向 NULLS LAST 和稳定 id 次序，以及排序/过滤先于分页；端口查询标记本身不能作为 SQL 验收证据。

## 6. 接入验收

1. 目标迁移可用、字段映射核对后，在独立测试库验证身份唯一性、版本唯一性和 CAS 冲突。
2. 使用真实日志/事务适配注入日志失败和 commit 失败，确认当前投影及版本不部分提交。
3. 验证内部全员读取、绑定人写入和非绑定403，HTTP 层不得沿用旧角色矩阵阻断新读取。
4. 对真实路由执行完整的新增待补齐→补齐→人工核验→改价→历史快照流程。
5. 原型前端在上述冻结API之上接入；公共路由/布局按统一维护规则协作（未点名独立负责人），A页面与业务闭环仍由A负责。

F4/F3 及后续阶段所需 schema/接口在对应切片继续补充，不因 F1 独立代码可测试而视作全部 A 功能完成。

## 7. F4 正式配置接入补充

独立服务 `NewFormalConfigService(work, sealer, urls, clock)` 已提供 Create/Update/Disable；具体 Go 端口位于 `internal/domain/supply/formal_config.go`，业务依据见[F4 契约](./supply-suppliers.draft.md)。

- `FormalConfigUnitOfWork.Within` 加入调用方事务；端口包括 SupplierForUpdate、ConfigForUpdate、ID分配、创建/CAS更新、资源引用查询及安全日志追加。
- `SecretSealer.Seal(ctx, plaintext, SecretBinding)` 必须采用版本化认证加密，并以 purpose/supplier/config 绑定认证附加数据。主密钥由环境/密钥管理能力提供，不能从普通数据库配置读取或硬编码。测试替身不可部署。
- 密文及密钥版本仅用于持久化，安全投影 `FormalConfigView` 用于返回和操作日志；普通查询不得直接序列化持久化对象。加密器及基础设施也不得自行记录明文输入。
- `FormalURLPolicy` 负责本地环境策略，可要求HTTPS；不在事务锁内执行网络探测。
- schema需核对区域来源、状态、revision、确认人员/时间/对应版本及密文版本字段。绑定配置、修改供应商区域和停用配置必须采用一致锁顺序与资源引用约束；单靠先查引用数量不足以证明并发正确性。
- Stage 35 已在 A 注册器实现正式配置 POST 新建、PUT 更新、DELETE 逻辑停用写端点，并有严格 HTTP 契约测试。GET 配置列表、供应商主体/网站账户/资质附件页面、公共主路由及公共密钥/URL策略接线仍未完成；HTTP接口替身测试不证明生产供应商归属、密钥或真实事务已经接入。

## 8. v0.22 F1 前端装配

A 页面组件为 `model-pricing-web/src/views/internal/models/StandardModelList.vue`，请求封装 `src/api/standardModels.ts`；建议公共菜单新增“标准模型库”，路径 `/internal/standard-models`，component 使用该文件。不要把旧 `Models.vue` 的SKU权限点和API语义复制到新入口。

页面已对接新F1列表/详情/版本/新增/整体修改/核验契约；后端 `RegisterSupplyRoutes` 及真实仓储仍需公共装配。当前没有修改生产公共路由、布局、入口和菜单，浏览器测试通过测试专用入口挂载组件。

操作记录复用现有 `GET /api/internal/workbench/operations`，以 `entity_type=standard_model`、十进制字符串 `entity_id`、`page=1`、`size=100` 查询。公共路由位于内部 AuthN 保护组下，并在 `WorkbenchInboxSvc` 已装配时开放；响应字段为 action、entity_type、entity_id、operator_name、reason、created_at。A适配已改为显示 `operator_name/created_at`，不再读取该接口没有提供的 `operator_id/request_id`。MSW契约场景和前端类型检查通过；真实登录的页面调用仍待公共主装配与浏览器联调，不能据此宣称运行日志查询已上线。

多值公司/类型及供应商/部署节点关联筛选、模型库级记录侧栏需继续冻结查询契约；不从当前分页结果推断全部可选值，也不将标准模型硬设单一区域。阶段截图与未完成项见[F1前端执行记录](../TDD/SUMMARY.md#stage-04)。

## 9. F3 资源读取装配

`SupplyDeps.Resources = supply.NewResourceReader(store)`；已有A注册器提供GET resources及/:id。缺读取依赖503，匿名401，其他门户403；内部人员全员读取，返回的can_edit仅依据当前StaffID与资源buyer_id，不套旧角色权限点或owner读取过滤。

`ResourceReadStore` 提供ListResourceGroups/ListResourceRows/GetResource；Get/Group需返回当前模型、供应商安全事实、配置安全事实和所选B报告的同一一致性快照。在报价/开通调用中加入现有事务，不另开独立提交。目标字段映射见Resource/ResourceRecord类型，不读取旧SKU/供应商profile表。

`ListResourceGroups`先对符合过滤条件的模型ID分页，再返回该页模型完整成员；领域会再过滤成员以保护区域聚合，但无法修复漏成员、错误SQL总数和排序。flat按资源分页。两者都必须实现双向NULLS LAST与稳定ID；指标按数值排序，缓存按原始计数比例比较，过期目标报告的指标为空。

B报告适配须提供report_id、可空resource_id、supplier/model/region、来源申报行及版本、不可变目标版本、自身连通/压力结论、有效性、可选毫秒/RPS/TPS字符串和cache_stats原始计数/场景。选定报告缺失/依赖失败必须区分，不借其他报告或原始测试凭据填充。

新字段需求包括resource的supplier_model_id、origin_row/version、target_version、latest_report_id及正式配置引用版本；B的报告需保留资源创建前的行关联。报告选择的任务顺序与迟到回调保护后续写切片继续冻结。样例及验证边界见[F3读取记录](../TDD/SUMMARY.md#stage-10)。

## 2026-10-21 main同步：审批资格接入核对

基线已更新到7f9886e。公共迁移000036采用internal_staff.can_approve_supplier，登录快照提供同名字段，A不增加独立资格表。执行迁移、部署公共认证与刷新已有会话需在生产装配时安排，本轮没有变更运行库。

REQ-25/W14 前端直接路由缺口已由 A 补齐：`src/router/guards.ts` 对 `requiresApproval` 路由检查认证快照的 `canApproveSupplier`，无资格访问 `/internal/approvals` 返回403。有资格/无资格的定向认证场景均通过；本机无审批资格账号直接访问也落到403。审批页仍是占位，前端检查不能代替API鉴权。

服务端资格需在供应商报价审批链路落实，并与共享router.go约定维护人协调：当前审批API使用M4Approve权限点，没有同时检查当前人员的internal_staff.can_approve_supplier。直接调用必须校验真实资格；不能只信菜单/路由或客户端。审批为后续确认需求，旧文档“公共文件统一维护”不等于已指定一个公共审批负责人。

A模型与资源普通阅读不使用该审批字段缩小范围；普通办理继续校验具体资源/节点绑定人员，审批资格不产生代办权。

## 10. 模型广场页面与选项装配

页面组件 `model-pricing-web/src/views/internal/resources/ModelPlaza.vue`，当前公共菜单“模型广场”已接到 `/internal/model-plaza`，资源深链为 `/internal/model-plaza/:resourceId`；不采用早期建议路径 `/internal/resources`。供应来源及资源详情在A页面内部，不需要公共方重建详情页。

现有ResourceReader新增Options能力，注册器增加GET `/resources/filter-options`；实际store实现可选接口ResourceFilterOptionsStore，查询region/keyword，Limit=50，每类至多50项，更多匹配返回HasMore。A真实仓储接新表；公共方提供目标DDL、人员/区域与主装配能力。选项缺失返回503，不能从当前资源页或旧profile猜测身份。

当前只读页面已按新GET契约验证；公共区域联动、采购人名称、日志/计划/历史报告面板及资源状态写入仍需继续接入。完整证据见[F3前端记录](../TDD/SUMMARY.md#stage-09)。

## 11. 工作台公告与计划摘要装配

`SupplyDeps.Notices = supply.NewNoticeReader(store)`、`NoticeWriter = supply.NewNoticeService(work, clock)`、`Plans = supply.NewPlanReader(store)`；具体构造签名以源码为准。A注册器提供公告读取、跨人员阅读状态、本人标记已读及计划摘要路由，接口见[工作台契约](./supply-workbench.draft.md)。

- 公共方提供新表、内部人员目录和主路由；A仓储映射 Notice/NoticeRead/ResourcePlan 及查询端口。Notice的影响状态/数量映射已有 affected_summary JSON，不增加平行数据库列。
- 阅读状态以公告/人员组合分页，过滤先于分页，published_at DESC、notice_id DESC、staff_id ASC稳定排序；all_staff以真实内部人员目录为准。查看其他人不更换认证上下文、不创建阅读记录。
- 阅读表要求 notice_id/staff_id 唯一约束。写入先锁公告、确认内部人员，再读取/创建个人状态；加入外层幂等事务，阅读记录和操作日志原子提交。重复阅读保留首次时间，新操作意图追加READ_NOOP，幂等重放不追加。
- POST已读使用认证StaffID，原因由服务端固定为“阅读公告”。禁止调用方通过body或query指定阅读者。
- 计划仓储返回计划与当前资源的同一一致性快照；发起人取created_by，不从当前buyer_id推断。所有摘要只读，revision比较仅为提示，不是执行授权或完整执行前置校验。
- 真实约束、并发、日志失败、提交失败及公共幂等响应提交顺序仍需联调。测试替身通过不代表现有运行容器已接入，见[阶段证据](../TDD/SUMMARY.md#stage-31)。

## 12. 工作台 A 详情组件装配

`model-pricing-web/src/views/internal/resources/NoticeDetailDialog.vue` 接收 id、可选 selectedStaffID、可选 readOnly；监听 close 关闭、read 刷新公共工作台计数。公共人员选择必须传真实StaffID，查看他人时同时传 readOnly=true。组件仅显式点击写入，GET不自动标已读，服务端仍承担身份与权限判断。

`PlanSummaryDialog.vue` 接收id、可选resourceID/region及close/changed事件，展示已知计划详情，按服务端能力提供修改/取消/实际完成；指定上下文不匹配时拒绝展示。Stage58已在源码开放符合权限的执行能力，工作台plan_id投影亦已接入；Docker实际验收尚未完成。

资源详情已内部挂载 `ResourceNoticesPanel.vue`，按 resourceID/region 查询当前人员公告，不需新增公共菜单。组件、截图和实际验证边界见[前端记录](../TDD/SUMMARY.md#stage-30)。

## 13. 标准模型多值筛选

ModelListQuery新增Companies/ModelTypes/SupplierIDs/Regions；规范化后复数条件为准，旧Company/ModelType被转入并清空。仓储不能继续只读取旧单值字段。供应商/区域必须通过同一资源EXISTS匹配；未指定关联条件保留无资源模型，过滤和模型去重先于分页。

ModelReadStore实现可选ModelFilterOptionsStore后可使用标准模型filter-options路由，缺该能力503。公司取全部标准模型，供应商取关联资源身份；不要用资源选项接口代替，避免丢失无资源公司的模型。具体字段与边界见[F1契约](./supply-standard-models.draft.md)和[后端记录](../TDD/SUMMARY.md#stage-03)。

## 14. 上下架计划装配与历史直接入口

当前计划列表/维护及页面已接入并真实验证（Stage54/57）；执行器、409终态重放和提醒worker已接入运行组合，Go 1.26编译并部署到本地8080 API（TDD Stage59/65）。真实计划执行、BLOCKED HTTP/数据库重放和到期提醒投递仍缺非空业务计划的联合验收。以下保留方案演进说明，直接state接口和ResourceStateDialog不得重新挂载；当前交接以第15节及H-P02–04为准。

PlanMaintenanceTransaction必须实现HasPendingConfigurationChange：B在同事务提供该资源“已通过尚未应用”的变更事实，true阻断创建，依赖失败关闭。与B审批/应用统一资源锁或等价约束，不在锁内远程调用，不从前端列表推断。此项补齐原型539行此前遗漏的门槛，见[修正记录](../TDD/SUMMARY.md#stage-15)。

资源详情内部已挂载ResourcePlansPanel，通过GET plans查询resource_id/region/status并打开PlanSummaryDialog，无需新增公共菜单；实际完成表单已接入，只有服务端授予 `can_execute` 时开放。真实接口不能将依赖失败伪装成空列表，见[前端记录](../TDD/SUMMARY.md#stage-21)。

PlanReadStore增加可选PlanListStore能力后，现有SupplyDeps.Plans即可使用GET plans列表；未实现列表能力503。SQL按resource_id/region/kind/status过滤后稳定分页，当前资源事实与计划同快照读取，total为计划数。正式前端和SQL已接入并在Stage54真实验收，见[TDD汇总](../TDD/SUMMARY.md)。

计划维护HTTP通过SupplyDeps.PlanWriter注入NewPlanMaintenanceService(work,clock)，提供创建/修改/取消三个入口；执行HTTP通过SupplyDeps.PlanExecutor接入，详见第15节。必须保留requireSupplyInternalStaff、认证StaffID、公共幂等和RequestID；真实work加入外层事务。早期[维护HTTP记录](../TDD/SUMMARY.md#stage-18)仅记录当时切片状态。

当前计划创建/维护服务为PlanMaintenanceService，A已提供[原生PlanStore](./plan-storage.draft.md)，实现读取/列表及PlanMaintenanceUnitOfWork。构造NewPlanStore(schema, readRunner, resourceStateStore, hooks)，只读runner建议ModelReadTransactions(db)；写hooks提供Resolve（借用当前外层事务）、Log、Outbox以及B PendingConfiguration。同资源锁保护状态维度唯一、CAS、PLAN_REVISION不可变日志及提醒outbox。A提醒worker已接入源码，按revision防陈旧，due_at不触发实际状态变化；Go编译并部署到本地API已完成，真实到期计划投递仍待验收。维护/读取及真实并发证据见[TDD汇总](../TDD/SUMMARY.md)。

> 本节原装配方案已被DEC-010取代，当前不得按此装配直接HTTP入口。上下架必须先创建计划，再由原发起人登记完成，见[纠正记录](../TDD/2026-10-20-plan-only-correction.md)。以下保留历史接口说明。

当前执行方案为PlanExecutionService及PlanExecutionUnitOfWork，不注册直接state HTTP。执行结果Conflict=true在外层幂等事务提交后返回409，并由终态快照保存/重放；日志或提交失败则返回技术错误且没有部分写入。计划/资源加锁及同维度唯一规则见[计划执行记录](../TDD/SUMMARY.md#stage-17)。该协议已编译并部署，真实业务执行与PG幂等重放仍待有计划数据时联合验收。

A现已提供NewPlanExecutionWork(plans, configs, reportRefLoader)的[原生执行适配](./plan-storage.draft.md)，复用PlanStore的Resolve/Log/Outbox；执行成功写资源、DONE计划、不可变日志、真实事件与公告，失败提案BLOCKED带原因。执行HTTP与原发起人 `can_execute` 已装入运行组合；Go/服务构建、领域PostgreSQL执行事务和HTTP替身重放用例通过。请保留“结果→提交→409/200及幂等响应”的顺序；Outbox必须持久化传入OccurredAt/actor/request_id。ONLINE提供真实FormalConfigStore与B当前报告引用；OFFLINE不要求可报价资料前置。真实资源上的HTTP成功/BLOCKED数据库提交及同键重放仍待联调。

SupplyDeps.ResourceStates注入NewResourceStateService(work,clock)，A注册器提供POST resources/:id/state。work实现ResourceStateUnitOfWork，加入现有幂等事务；资源锁定和当前模型/配置/B证据同快照读取，CAS更新与日志、公告、outbox同事务写入。公共方提供工具，A提供真实资源映射，B提供当前报告事实。

效果端口分配唯一event_id，并将其用于自动公告source_event_id防重；未知客户影响写PENDING，C消费后计算。不直接修改客户关系，上架不自动恢复。并发锁顺序及commit失败响应需共同真库验收。接口字段与当前测试边界见[HTTP记录](../TDD/SUMMARY.md#stage-25)。

资源详情已新增ResourceStateDialog，POST成功后重读详情并沿changed事件刷新来源与广场。权限展示使用安全DTO的can_edit及实际状态，服务端仍必须强制校验；403/409后输入保留且不自动更新revision重试。完整页面证据见[登记页面记录](../TDD/SUMMARY.md#stage-27)。

## 15. 计划实际完成 HTTP 与幂等响应

A 的 `RegisterSupplyRoutes` 已提供 `POST /api/internal/plans/:id/execute`，依赖为 `SupplyDeps.PlanExecutor`（`supply.PlanExecutionService`）。请求仅接受 `expected_revision`（规范正整数字符串）、`confirmed`、`completion_note` 和 `reason`；认证 StaffID 是执行人，不能从请求选择或替换计划发起人。成功返回 `EXECUTED` 计划，领域服务以 `Conflict=true, nil` 表示已在同一事务保存的 `BLOCKED`，HTTP 映射为409并携带计划结果。

Stage58已在当前源码运行组合注入 `PlanExecutor`，并扩展共享幂等快照协议：BLOCKED的409终态在外层事务内落库，提交后返回；相同键/相同请求重放原HTTP状态、业务码、消息和计划结果，不再执行；普通验证及授权4xx仍标记FAILED。Go 1.26编译、定向用例及本地API部署见TDD Stage59/65。还需使用真实计划执行成功与BLOCKED，并通过持久化幂等记录验证原409重放和无重复事件/日志。

`PlanSummary.permissions.can_execute` 仅对原发起人的PENDING上下架计划开放；执行表单要求确认实际外部操作、完成依据、操作原因及预期版本。BLOCKED结果在事务提交后以409终态快照保存并可同键重放。源码已Go编译且部署到本地API；真实HTTP/PG业务链仍需验证原发起人、成功/阻断、事务提交及同键重放不重复写入。当前主库资源和计划均为空，不能据空列表宣称这些业务路径通过。

## 16. v36 Docker 联调补充

Stage 46 已用 Go 1.26 容器构建当前工作区 API，并更新本地 `model_bss_api_latest` 挂载的二进制。当前运行库为 `36 / dirty=false`；健康、内部认证保护、标准模型列表和模型广场列表均已在 8080 冒烟通过，模型与资源数据目前为空。未运行迁移或业务写入。实际结果见[TDD Stage 46](../TDD/SUMMARY.md#stage-46当前工作区-docker-api-冒烟)。

真实 v36 表包含 `submission`、`submission_row`、`onboarding_flow`、`onboarding_node`、`onboarding_batch`、`test_job` 和 `test_report`。但申报与办理之间没有明确的行级流程关联字段：申报行可关联资源/测试任务，办理节点只关联 flow，flow 只暴露供应商 owner；不能仅按供应商猜测当前资源对应的办理节点。B/公共接入需提供同事务可信解析器，返回当前资源对应的 flow、node、origin row/revision、target version、assignee 及前置状态，再接通资源创建、报告绑定与逐资源准入。

当前 `model_bss_api_latest` 已由TDD Stage65更新为工作区源码构建的API，监听8080并健康连接v36主库；Stage60配置的本机正式配置密钥保持有效。标准模型、模型广场、供应商主体和工作台的认证读取入口已在本地前端验证。运行库业务数据为空；绑定/准入仍缺B的真实办理和报告事实，不能据空数据读取宣称资源报价链路通过。

## 17. 供应商门户前端 API 接入

A 已把供应商“模型管理”“模型新增/价格变更”和“草稿/申报记录”页面接入调用层。后端 `internal/api/router_submission.go` 当前仍是空注册函数，旧 `/supplier/model-applications` 是单模型遗留流程，不能承载多模型批次及逐行测试。B 接入前端时需要提供：

- `GET /api/supplier/models`：只返回认证主体自己的模型安全投影，字段与 `model-pricing-web/src/api/supplierModels.types.ts` 一致；系数为比例字符串，连通/压力测试分别返回 `tested` 与 `passed`，报价资格返回原因数组。不得返回测试或正式 Key。
- `POST /api/supplier/models/:id/price-change`：按认证主体和资源版本校验，使用 `expected_revision`、新系数及原因创建独立价格变更申报；不直接修改资源系数。
- `GET /api/supplier/submissions/model-options`：返回供应商可选的安全标准模型目录（`id/name/official_id/company`），支持关键词查询；不可复用内部全量模型接口。
- `GET/POST /api/supplier/submissions`、`GET/PUT /api/supplier/submissions/:id`、`POST /:id/submit`、`POST /:id/withdraw`：草稿与申报记录均由认证主体隔离；模型新增允许多行，草稿只保存申报行/测试信息，提交进入办理后才创建正式资源。
- `POST/GET /api/supplier/submissions/:id/rows/:rowId/test`：只针对已保存行创建/读取对应类别任务，返回服务端测试状态、报告摘要和指标。前端分开呈现“做过测试”和“测试通过”。

草稿请求中的 `test_api_key` 只供测试平台使用；服务端必须按测试凭据策略安全处理，普通 GET、列表、日志和报告响应不得回显。批次提交校验为每行做过连通性测试且至少一行做过压力测试；每资源是否可接入仍由其自身连通及压力测试结论决定。A 页面已据此接线并展示状态，当前仍不能声称门户接口真实联调通过。

## 18. F4 正式配置页面装配

模型广场资源详情的“接入与配置”页现已提供正式配置新建、修改、停用和资源绑定入口，调用现有 A 接口 `GET/POST/PUT/DELETE /api/internal/suppliers/:id/formal-configs`。前端只显示是否已设置 Key，不回显密钥；新建/修改要求确认已完成外部平台操作，停用要求填写原因。

F4属于A。当前源码已在 `cmd/server/supply_runtime.go` 接入版本化AES-GCM与URL策略，并按密钥配置启用FormalConfigs；环境变量缺失时写入503，缺一项或密钥格式错误时启动失败。部署密钥未配置，源码未部署至Docker，尚未真实联调。

## 19. F12 合同与授信存储边界

000033 当前 `contract` 表只有 `subject_type/id/status/signed_at/version/file_ref/note` 等基础列，`credit_line` 只有主体、方向、金额、币种、版本、有效时间和 note；没有详细设计所需的合同编号/名称、`contract_key`、附件 `file_id`、来源流程与节点授权引用、授信文档编号及业务版本唯一约束。现有结构可以供后续安全只读展示，但不足以实现首次接入上传、可追溯的不可变合同/授信版本和 `GetBusinessReadiness`。

A不把JSON塞入note或把file_ref猜成附件ID，也不自行执行DDL。总览/A任务书将供应商与客户合同/授信F12均分给A；A需评估任务书基础字段与后续详细设计差异、提出必要结构/附件方案并继续业务实现，迁移文件由约定维护人统一落地，B/C提供办理/主体上下文。详细设计新增字段不自动构成必须等别人先补齐的前提。

Stage66 已补齐当前 v36 可安全支持的供应商只读投影：内部合同列表与授信历史/当前值查询、供应商主体页展示。兼容字段与异常版本处理见[只读契约](./f12-v36-read-projection.draft.md)。该投影不暴露 `file_ref`/`note`，不声称合同版本历史或附件可下载；当前没有供应商数据，尚未做非空真实行的页面/API联调。A仍需继续完成完整 F12 的写入/不可变版本服务、共享附件调用和首次接入授权/readiness；数据库迁移按共享文件维护规则推进，不能把只读切片当作完整业务交付。

## 20. A 资源报告读取 API 与 B 提供方契约（2026-10-22）

A已实现历史列表/详情/文件下载页面调用，并新增正式 API 适配路由。`SupplyRouteModuleOptions.ResourceReports` 接收可信报告/附件读取提供方，方法收到当前员工ID、资源ID及报告/文件ID；运行组合暂未注入B提供方，所以认证请求返回503，不能视为真实数据已接入。当前资源摘要仍来自ResourceReader，不用摘要拼造历史。

- 历史 GET `/api/internal/resources/:id/test-reports?page=1&size=10`（已有详细设计补充路径）：返回 `{list,total,page,size}`。
- 详情 GET `/api/internal/resources/:id/test-reports/:reportId`：返回摘要字段及 `files:[{id,name,can_download}]`。
- 文件 GET `/api/internal/resources/:id/test-reports/:reportId/files/:fileId`：使用认证的二进制下载，不返回任意外部URL。A先核对同一报告详情中的文件ID和`can_download`，B/附件提供方在`OpenFile`中必须再次校验操作者、资源、报告、文件归属与权限；前端 can_download 只控制按钮，不能替代服务端授权。下载文件名必须是文件名而非路径；响应强制attachment、nosniff、private/no-store，并限制安全Content-Type。

列表/详情摘要字段：`id/resource_id` 为bigint字符串；`status=RUNNING|COMPLETED|FAILED`；`created_at` 与可空 `finished_at`；`target_matches` 标记当前目标是否匹配，不单独表示准入有效；可空布尔 `connectivity_passed/pressure_passed`（COMPLETED时均有结论）；`metrics:{connection_ms,rps,tps}` 为可空非负十进制字符串；`cache_stats:{status,report_id,hit_tokens?,input_tokens?,scene?}` 沿用已有缓存口径，真实零与缺失分开。详情 files 只包含文件标识/名称/下载能力，不带凭据、存储路径、平台原始响应。前端显式投影这些字段，不保存未知字段。

响应类型位于`internal/domain/supply/resource_report_read.go`，路由和安全投影位于`internal/api/supply_resource_reports.go`。未接入时返回503；异常资源归属或响应字段映射拒绝返回，不泄露`report_file`路径、凭据或平台原始响应。B若已有不同DTO，由A适配真实契约，不要求B另造报告存储。按当前设计，内部补测只展示办理指引和刷新入口，没有新增直接启动平台测试权限。真实报告事实、附件下载、目标版本判定与授权联调仍待B/附件提供方。

供应来源区间筛选当前通过已有 flat API 完整读取所选模型/外层筛选范围，再做前端筛选和分页；不向现有API发送不支持的区间参数，也不只筛当前页。页间总数变化、重复ID或范围错配会要求刷新。后续数据量较大时可由公共/B/A约定服务端区间筛选契约后替换，不能静默截取前几页。
## 21. A运行联调修正与剩余计划前置（2026-10-22）

Stage54已在v36隔离库用真实认证/页面/SQL跑通标准模型主写流程、公告本人阅读和计划创建/修改/取消；随后将A修复更新到当前API。日志新增真实姓名快照，资源读取兼容省略价格维度；计划配置检查改从资源控制快照读取，消除查询不存在列的500。

计划维护的运行回调仅校验绑定配置的状态/引用版本，不代表B“已批准但未应用变更”已接入。B需交该事实，A在同资源锁/事务组合检查。F13执行器、受限执行资格、409终态重放、提醒worker和工作台plan_id投影已完成Go编译并部署到本地API；真实计划执行/提醒事件仍待非空业务数据联合验收。

本轮没有实现公共加密服务、B报告/文件/办理接口，也未新增迁移。临时服务和数据库均已移除，运行库继续v36。
## 22. A 模型级资源日志与具体计划导航已提供

A 已提供 GET `/api/internal/standard-models/:id/resource-operations`，汇总当前模型资源及关联计划留痕，安全投影与分页见 supply-resources.draft.md 第13节。供应来源页已使用该接口，不修改公共 workbench/operations 的通用查询。

A深链现接受 `/internal/model-plaza/{resource_id}?region=CN&tab=plans&plan_id={plan_id}`；详情核对资源/区域后打开指定计划。工作台仓储从原生`plan`行直接投影稳定plan_id并传入deeplink，避免相同资源/发起人/时间戳的多条计划误跳到同一条。代码和定向回归见TDD Stage67；有计划时的工作台真实跳转、提醒投递仍待运行数据验收。计划创建还需B提供待应用变更事实。
