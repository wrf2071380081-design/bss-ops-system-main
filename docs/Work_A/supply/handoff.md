# A 业务交接与接入参数

<a id="handoff"></a>

更新：2026-10-27。代码已合入main@1fe8907，本次分支交付包含A增量；日常库45/false。运行API仍为Stage204二进制，此次合并构建尚未部署。 当前迁移与部署见[交接](./handoff.md)，执行结果只维护[TDD SUMMARY](../TDD/SUMMARY.md)。下文保存业务/接口/存储约束，不再逐阶段重复测试结论。
## 当前交接（2026-10-27，Stage205～207）

| 项目 | 交付与验收边界 |
|---|---|
| 代码和运行 | work-A-fix@1fe8907已合并main，A增量随本次分支提交交付；API仍运行Stage204二进制，合并构建已通过但尚未部署；前端为工作区源码 |
| 数据库与迁移 | 原日常model_bss=45/false；团队43主体关联、A的44 F12、45供应商元数据。正常库顺序升级，本机旧44补齐已执行，不能重跑或盲目force；见[升级路径](./database-v45-upgrade.md) |
| 标准模型库 | 手工新增/补齐、单一保存留痕、核验/修改失效、版本历史及日志已有真实联调；最近UI和写入见Stage198，当前45版仅补受影响能力检查；F2抓取/差异仍为后续增强 |
| 模型广场 | 来源、详情、安全指标、非空报告/下载、真实节点/申报历史、配置绑定/逐资源接入/报价资格、批准改价及批量部分成功/取消均有指定场景证据（Stage193、196/197、201）；测试报告包含明确QA产物，不代表Inspector已测 |
| 工作台A子能力 | 公告个人阅读、计划维护/执行、409重放、提醒/具体计划导航、领导待办与分派后A办理已有指定场景证据；跨人员/失败回滚见Stage194、ONLINE见Stage197，不重做B审批规则 |
| 供应商/F12 | 曾用名/资质元数据及详情业务Stage201；供应商合同授信首次办结Stage200、归档/跨页冲突/并发/日志及附件保护Stage202。结构已交付，不再等待他人补同批字段 |
| 门户前段 | 草稿保存/409刷新保留/离开保护/撤回、至少一行有效连通通过提交、草稿测试资源批准后同ID接管与防重已验Stage204；自身双测和正式配置仍是逐资源准入前提 |
| main新增账户设置 | 源码已合并、构建/类型/契约及fixture浏览器通过；运行API及门户企业证明上传目录仍需部署核对，真实账户写入/上传未在本轮验收 |

## 接入责任与当前参数

| 接点 | 实际提供方/边界 |
|---|---|
| 入口与HTTP | newSupplyRuntimeDeps组合A依赖，RegisterSupplyRoutes挂A路由，main/router保留main账户设置增量与A装配；不另建审批中心 |
| 流程/报告 | B原生批准/分派、源行版本、指定报告及CompleteRow事实已由A适配；明确QA报告不代表Inspector真实出站 |
| 资源办理 | WireSupplyResourceWorkflow后接ResourceStages；绑定/逐条接入/改价回写与日志/事件借同一事务，缺真实资格拒绝而非默认通过 |
| F12 | NativeSupplierF12接SUPPLIER流程授权/齐备；客户可信主体与流程模板由C事实接入。F12SubjectPanel接subjectType/subjectId及flowContext，changed后刷新宿主 |
| 计划/阅读 | PlanWriter/PlanExecutor/PlanReminderStore及NoticeWriter/Reader使用现有运行组合；GET不写状态，BLOCKED提交后保存409幂等响应 |
| 文件 | F12AttachmentStoreDir、SupplierQualificationStoreDir为A现有本地受控适配；D07与权威审核仍另接，main账户上传使用独立UploadDir配置 |



<a id="shared-transactions"></a>

## 事务、幂等和错误边界

### 3. 事务端口要求

`Within` 必须加入调用方已有事务；只在没有外层事务时开启顶层事务。不能在幂等中间件事务内独立提交一份业务事务。

事务适配提供：分配 ID、锁定模型、创建/CAS 更新、追加完整版本、追加操作记录。任一业务校验、唯一性、快照、日志或 commit 失败时整体回滚；原错误保留给 HTTP 错误映射。

新请求无资料变化时只追加 UPDATE_NOOP / VERIFY_NOOP 操作记录，保留原版本和资料更新时间；幂等重放不重复执行。日志失败不得将无变化写入视为成功。

当前 `db.WithDB/FromContext` 是可复用的上下文基础，不等于事务和日志适配已交付。A 的内存事务测试只验证服务将这些动作放进同一个端口回调，不能证明 SQL 实现的隔离和回滚。

### 5. 认证、错误与幂等接入

- `WriteMeta.ActorID` 来自认证上下文；RequestID 来自请求追踪；Reason 来自业务操作说明。
- 新增不接受任意 maintainer_id，自动绑定创建者；更换维护人由公共转交接口处理。
- 领域错误分为 FieldError、ErrForbidden、ErrRevisionConflict、ErrIncomplete、ErrInactive、ErrNotFound、ErrIdentityConflict、ErrDependencyUnavailable、ErrSourceRejected。公共 HTTP 层负责映射，不在 A 领域自行分配数字 code。
- 写 HTTP 入口继续要求 Idempotency-Key，领域服务不另外实现一个幂等存储。接口幂等重放与真实事务接入后还需验证。
- 统一 errors[]/fieldErrors 结构及错误原因机器码仍由公共契约固定。
- 当前公共幂等同键异body返回400，与附01的409待统一；FAILED状态后的重试策略也需与前端“保留原键重试”对齐。
- 计划前置冲突必须先保存BLOCKED和失败依据、提交事务，再输出409并保存幂等结果；不以已发送响应替代提交成功。其他写入同样核对事务与回执一致性。
- 读取适配器需真实实现白名单排序、双向 NULLS LAST 和稳定 id 次序，以及排序/过滤先于分页；端口查询标记本身不能作为 SQL 验收证据。


<a id="portal-integration"></a>

## 供应商门户协作

### 17. 供应商门户跨模块接口契约

供应商模型、申报草稿/记录及测试请求已有调用适配。`router_submission.go`通过`registerSupplierSubmissionRoutes`注册正式多行接口；草稿保存/重开/409恢复/撤回及提交/接管的实际证据见SUMMARY Stage204。以下仅列协作边界，不恢复旧单模型遗留流程：

- `GET /api/supplier/models`：只返回认证主体自己的模型安全投影，字段与 `model-pricing-web/src/api/supplierModels.types.ts` 一致；系数为比例字符串，连通/压力测试分别返回 `tested` 与 `passed`，报价资格返回原因数组。不得返回测试或正式 Key。
- `POST /api/supplier/models/:id/price-change`：按认证主体和资源版本校验，使用 `expected_revision`、新系数及原因创建独立价格变更申报；不直接修改资源系数。
- `GET /api/supplier/submissions/model-options`：返回供应商可选的安全标准模型目录（`id/name/official_id/company`），支持关键词查询；不可复用内部全量模型接口。
- `GET/POST /api/supplier/submissions`、`GET/PUT /api/supplier/submissions/:id`、`POST /:id/submit`、`POST /:id/withdraw`：草稿与申报记录均由认证主体隔离；模型新增允许多行，草稿可由B分配物理测试resource，批准进入办理后由A同ID正式接管并防重。
- `POST/GET /api/supplier/submissions/:id/rows/:rowId/test`：只针对已保存行创建/读取对应类别任务，返回服务端测试状态、报告摘要和指标。前端分开呈现“做过测试”和“测试通过”。

草稿请求中的 `test_api_key` 只供测试平台使用；服务端必须按测试凭据策略安全处理，普通 GET、列表、日志和报告响应不得回显。批次提交沿DEC-020：至少一行有效连通通过即可提交，压力非强制；每资源是否接入仍须其自身双测、配置、版本及办理资格。门户前段已有真实接口验收，Inspector真实出站/回收仍按用户要求暂缓。

<a id="remaining"></a>

## 剩余开发、部署与联调

1. 更新原日常API到合并构建，核对MODEL_BSS_UPLOAD_DIR持久目录，验main账户设置真实认证读写/上传及非空身份解析。
2. 客户F12权威主体/流程/普通维护事实接入，A完成共用面板/接口适配后的真实写入验收。
3. D07文件身份与权威资质审核、Inspector真实配置/调用/回收、供应商合并执行，按各自依赖推进；Inspector按用户要求暂缓。
4. 对尚未覆盖的页面竞态和具体跨模块新增场景做定向补验。F2抓取/差异确认仍是后续增强。

用户已授权本轮提交推送到work-A-fix，不自动合并GitHub PR。测试范围只看SUMMARY真实记录；数据库升级、源码存在、fixture、构建和已部署/真业务验收分开说明。
