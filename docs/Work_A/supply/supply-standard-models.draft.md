# F1 标准模型库契约

<a id="standard-models"></a>

更新：2026-10-27。手工维护/单一保存留痕、核验、版本和日志已有指定真实场景验收；F2抓取/差异确认后续增强。 当前迁移与部署见[交接](./handoff.md)，执行结果只维护[TDD SUMMARY](../TDD/SUMMARY.md)。下文保存业务/接口/存储约束，不再逐阶段重复测试结论。

## 当前规则与阅读顺序

模型维护人来自认证创建者；普通维护不转交。资料不全允许保存并返回缺失路径，报价仍阻断。资料完整时单一“保存并留痕”执行人工核对声明，后端检查来源、核验日期和revision；保存、核验、快照和日志同事务。修改后的新版本只有本次核验成功才能报价。维护人/revision/核验元数据沿既有版本快照，不要求新增同名列。

主表单在核验日期结束，补充来源/说明/计费维度及历史日志从列表“更多”进入；见[原型约定](./prototype-alignment.md)。

<a id="model-http"></a>

## 资料、价格与HTTP

### 1. 范围

本切片交付手工维护八类资料、价格项、完整性检查、人工核验、不可变版本、列表与详情契约。F2 自动拉取和差异接口在阶段 3 实现，不能用其未交付阻断手工模型维护，也不能把现有旧价格同步视为 F2 完成。

需求中的既定规则：金额用 decimal 字符串；资料不全不能核验和报价；写操作幂等及同事务留痕；新表与旧业务表隔离。下列字段和状态沿用详细设计建议，需与公共目标 DDL、绑定和 C 消费契约核对后冻结。

### 2. 传输与通用约束

| 项目 | 契约 |
|---|---|
| 前缀 | `/api/internal/standard-models` |
| 鉴权 | 复用内部登录上下文；客户端不提交操作者身份 |
| ID | bigint ID 通过十进制字符串传输；前端不转换为 Number |
| 数值 | 价格、计费单位数量及涉及精度的值均为 decimal 字符串，不经过 float64 |
| 时间 | 时间戳 ISO 8601 带时区；核验日期 `YYYY-MM-DD`；展示 Asia/Shanghai |
| 成功 | HTTP 200、`{code:0,message:"ok",data,requestId}` |
| 分页 | `data={list,total,page,size}`；默认 page=1、size=20，上限 100 |
| 写请求 | `Idempotency-Key`；更新已有实体再带 `expected_revision`；reason 保留变更原因 |
| 并发 | 当前 revision 不匹配返回 409，不能覆盖新数据或创建额外快照 |

共享说明中的 `errors[]` 与现有公共实现的字段错误结构尚不一致。A 提供字段路径和安全说明需求，由共享文件约定维护人固定编码和序列化格式；不在本草案自行分配数字业务码。

### 3. 官方资料与安全 DTO

| 原型区域 | 逻辑字段 | 规则 |
|---|---|---|
| 官方身份 | company、name、official_id | 官方身份唯一范围按目标 schema 核对；不同官方版本 ID 不合并 |
| 模态 | model_type、input_modalities、output_modalities | 区分输入与输出；枚举需对齐目标字典 |
| 容量 | capacity | 有结构的资料和必要说明，未知与零值分开 |
| 核心能力 | abilities | 区分支持、不支持、未披露和未核验，不把缺失转换为 false |
| 价格 | price_items | 每项独立单位、币种、适用条件和来源 |
| API | api_access | 访问条件、接口及限流说明，不保存调用方秘密 |
| 生命周期 | lifecycle | 官方发布/版本/生命周期资料；不伪造未披露日期 |
| 来源与核验 | sources、checked_at | 来源可核验，资料齐全不自动代表已人工核验 |

详情同时提供 id、status、verify_status、current_version_no、revision、missing_fields、quote_readiness 及服务端计算的操作资格。不能把前端传入的 can_edit 作为授权依据。

本草案建议 `status=ACTIVE/INACTIVE`、`verify_status=INCOMPLETE/UNVERIFIED/VERIFIED`，沿用详细设计 §4.1；它们不是已交付的数据库枚举。capacity、abilities、api_access、lifecycle 的完整嵌套 schema 需与原型资料及公共 DDL一并评审，不能以任意 JSON 原样写入替代结构校验。

### 4. 价格项

价格项样例仅说明数据形状，不表示真实厂商资料：

```json
{
  "item_key": "text_input_standard",
  "label": "输入",
  "task_type": "TEXT_INPUT",
  "amount": "0.00000000",
  "currency": "USD",
  "unit_code": "TOKEN",
  "unit_quantity": "1000000",
  "condition": "标准 API 计费",
  "dimensions": {},
  "source_url": "https://vendor.example/pricing"
}
```

- item_key 在同模型内唯一、稳定；版本快照及报价消费通过它关联，不通过可变 label。
- amount 非负；零是有效价格，缺失价格为缺项/null，不能默认补零。
- unit_quantity 严格为正；currency 为三个大写字母。
- task_type、unit_code、dimensions 由受控字典/schema 校验；不凭此样例推断只支持 TOKEN 或文本。
- 文本、图片、视频、音频按对应任务检查必要价格项；不强制所有模态具有“输入价/输出价”同一形状。
- 不同币种、单位和条件不得直接比价；A 不自动换汇或推导跨币种最低成本。
- 超出目标 numeric 精度的输入拒绝，不能以数据库舍入隐藏精度变化。具体约束与目标 DDL 一起冻结。

### 5. 接口行为

| 接口 | 输入 | 成功结果 | 失败反例 |
|---|---|---|---|
| GET `/` | keyword、company、model_type、status、verify_status、sort_by、order、page、size | 模型分页及完整性/可报价摘要 | 未知排序字段 400 |
| GET `/:id` | 字符串 id | 当前完整资料、版本/revision 和操作资格 | 不存在 404 |
| POST `/` | 可编辑八类资料、维护人策略、reason | id、版本/revision、verify_status、missing_fields | 官方身份重复 409；字段结构非法 400 |
| PUT `/:id` | expected_revision、完整可编辑资料、reason | changed、新版本/revision 或无变化结果 | 过期 revision/身份冲突 409；非法结构 400 |
| POST `/:id/verify` | expected_revision、checked_at、reason | 核验状态和新快照信息 | 资料不全 400；revision 过期 409 |
| GET `/:id/versions` | page、size | 不可变完整快照分页 | 不存在 404 |

列表不添加没有业务定义的模型 region 过滤。排序白名单及空值末尾、稳定 id 次序需要在冻结契约中明确；不能把 SQL 片段直接接收为 sort_by。

PUT 为可编辑资料整体替换；省略请求结构要求的字段返回 400。“允许保存内容不完整的草稿”与“允许省略请求结构字段”不同：草稿可以显式提供空资料，并由 missing_fields 标明业务缺口。

### 6. 写入、版本与可报价规则

1. 用户已确认允许保存不完整资料，返回缺失路径；核验和报价仍被阻断（见[业务决策记录](./decisions.md) DEC-004）。已提供但非法的字段仍拒绝。
2. 更新锁定当前对象，校验身份和 revision；无实际资料变化返回 changed=false，不增加业务版本。
3. 实际资料变化产生完整不可变快照，再更新当前投影和 revision。历史报价引用的模型版本不随当前资料变化。
4. 页面使用一个“保存并留痕”入口。资料不完整时只保存待补齐/未核验状态；资料完整时，当前维护人明确执行该保存即为人工核对声明，后端仍须校验八类资料、官方来源、核对日期和 revision。
5. 完整资料的保存、核验人/日期、不可变快照和操作记录在同一事务内提交；资料变化生成一个新版本，新版本只有本次校验成功后可报价。仅字段齐全、填写日期或普通创建/更新请求不能将模型置为已核验。
6. quote_readiness 至少返回 ready、reasons、standard_model_version_no；ACTIVE、VERIFIED 且所需场景价格资料满足时才可用。
7. C 在实际报价写入时重新读取并校验当前版本；页面缓存的 ready 只用于展示。
8. 业务变更、模型快照、operation_log 同事务提交。日志失败整体回滚；HTTP、抓取和 LLM 调用不进入事务。
9. 所有内部读取遵循本轮内部全员可见口径，写入按绑定维护人。用户已确认新建自动绑定认证上下文中的当前创建者；新增请求不接受自报 maintainer_id，常规 PUT 不承担转交。后续更换维护人走公共转交能力（DEC-005）。

### 9. 独立领域切片的实现映射

用户已授权依据原型复核合理后按切片契约先行＋TDD推进；复核和范围记录见 [F1 实施计划](../TDD/regression-scenarios.md#2026-10-20-f1-domain-plan)。此授权推进独立领域代码，不等于公共 HTTP 契约和目标 DDL 已冻结。

当前聚合逻辑结构：`ModelData` 包含 company/name/official_id/model_type、输入输出模态、capacity、abilities、price_items、api_access、lifecycle、sources、checked_at。

- capacity/api_access/lifecycle 采用 `{key,disclosure,value}` 的结构化事实；disclosure 为 DISCLOSED/UNDISCLOSED/UNVERIFIED，已披露必须有值，未核验阻断完整性。
- abilities 采用 `{key,state,description}`；state 为 SUPPORTED/UNSUPPORTED/UNDISCLOSED/UNVERIFIED。
- 支持本原型的 TEXT/IMAGE/VIDEO/AUDIO 模型及对应任务价格覆盖；价格任务、单位及维度词表在 `price.go` 明确列举，扩展须有契约和测试，不接收任意 JSON 作为计费规则。
- 金额规范为八位小数字符串，以相同数值不同尾零格式重提不会制造价格变更版本；不同稳定键/条件保留为独立价格项。
- 来源策略同时检查模型级与价格项级来源；构造/保存不代表来源已核验。

准确公共适配需求见[接入请求](./handoff.md#handoff)。HTTP 层已有类型化资料、查询及未知输入/幂等路由测试；真实仓储分页排序、统一 errors[] 和真库事务仍需公共接入验证。

### 10. 已实现的 HTTP 契约

注册入口：`api.RegisterSupplyRoutes(internalGroup, api.SupplyDeps{Reader, Writer, IdemStore})`。调用方提供前缀为 `/api/internal`、带 AuthN/RequestID 的组；主应用已接入；当前合并源码与运行二进制的部署边界见交接文档。

已注册方法：GET 列表、GET `/:id`、GET `/:id/versions`、POST 新建、PUT `/:id`、POST `/:id/verify`，以及与原型单一保存入口对应的 POST `/manual-completion`、PUT `/:id/manual-completion`。页面使用 manual-completion 路由；旧创建/更新/核验路由保留兼容，但不用于页面的一次性补全流程。仅接受已认证 INTERNAL/STAFF，使用 StaffID 而非 AccountID/OperatorID 作为模型维护人。读取不检查旧角色权限点。

#### 10.1 请求结构

模型资料字段为 company/name/official_id/model_type/input_modalities/output_modalities/capacity/abilities/price_items/api_access/lifecycle/sources/checked_at，详细结构见 §9 和领域类型。

- POST 允许只传待补齐资料及 reason；不能传 id、maintainer_id、status、verify_status 等服务端元数据。
- PUT 需显式包含全部13个资料字段（空资料可以用空字符串、空数组/null表达），再带字符串 expected_revision 和 reason，不能用部分 PUT 静默清空其余字段。
- verify 请求只含字符串 expected_revision、checked_at 和 reason。
- manual-completion 新增请求允许显式空字段并携带 reason；更新请求还须携带字符串 expected_revision。后端根据八类资料完整性决定保存为待补齐还是执行人工核验，不接受客户端自报状态或核验人。
- revision 必须在正 bigint 范围内；金额字段按 string 解码。
- 未知字段、非规范大小写、重复键（含嵌套对象）、尾随 JSON、根 null/数组拒绝；body 上限1MiB，嵌套深度上限64。

新增草稿示例：

```json
{"name":"待补齐模型","reason":"登记待完善的官方模型资料"}
```

原型单一保存入口的补全示例：

```json
{"expected_revision":"1","company":"Example","name":"Example Model","official_id":"example-v1","model_type":"TEXT","input_modalities":["TEXT"],"output_modalities":["TEXT"],"capacity":[{"key":"context_window","disclosure":"DISCLOSED","value":"128000 tokens"}],"abilities":[{"key":"tool_call","state":"UNDISCLOSED","description":""}],"price_items":[{"item_key":"text_input_standard","label":"输入","task_type":"TEXT_INPUT","amount":"2.00000000","currency":"USD","unit_code":"TOKEN","unit_quantity":"1000000","condition":"标准请求","dimensions":{},"source_url":"https://vendor.example/pricing"},{"item_key":"text_output_standard","label":"输出","task_type":"TEXT_OUTPUT","amount":"8.00000000","currency":"USD","unit_code":"TOKEN","unit_quantity":"1000000","condition":"标准请求","dimensions":{},"source_url":"https://vendor.example/pricing"}],"api_access":[{"key":"protocol","disclosure":"DISCLOSED","value":"HTTPS API"}],"lifecycle":[{"key":"release","disclosure":"UNDISCLOSED","value":""}],"sources":[{"label":"官方价格","url":"https://vendor.example/pricing","source_kind":"OFFICIAL_PRICING"}],"checked_at":"2026-10-20","reason":"对照官方资料完成手动补全"}
```

#### 10.2 查询、DTO 与历史版本

- 列表只接受 page/size/keyword/company/model_type/status/verify_status/sort_by/order；重复或未知参数400。page默认1，size默认20，上限100。
- sort_by 白名单：id/name/company/model_type/verify_status/checked_at/updated_at；默认updated_at desc。读取端口要求 NULLS LAST 和稳定 id 次序，实际 SQL 由仓储验证。
- 标准模型没有单一区域字段；原型基于供应资源关联的地区筛选需与 F3 联动，不把 region 写成全球模型固有属性。基础 F1 查询暂不接收 region。
- DTO 将 ModelData 字段扁平展开，不暴露内部 data 包裹；返回字符串 id/maintainer_id/revision/version_no，当前资料还带 current_version_no，以及 missing_fields/quote_readiness/permissions。
- 历史版本响应不提供 can_edit/can_verify 操作资格，不能用于新报价。B/C 的 `ModelReader.Snapshot(id, version)` 返回不可变资料、IsCurrent 和 CurrentVersionNo，显式区分历史消费与当前可报价判断。
- PUT/verify 返回 changed，修订号未改变表示无实际版本变化。
- 无实际变化的新保存/核验请求仍写 operation_log（UPDATE_NOOP / VERIFY_NOOP），不增版、不改变资料更新时间；日志失败整体失败。同幂等键重放不再次执行业务或追加日志。

#### 10.3 错误与公共幂等边界

继续使用统一 response.Body：错误 data=null，requestId 来自本次请求，内部异常不回显原错误。字段/不完整400、绑定403、不存在404、身份/revision/来源冲突409、依赖缺失503。

写路由使用现有公共 Idempotency；缺键400，同键同body重放保持本次requestId，同键异body沿用当前公共实现400。附01期望的异体409、FAILED重试及真实commit失败的响应一致性，仍需公共方统一；A 不另写一套中间件。缺少幂等存储时写入口503，不开放无保护的替代路径。

Swagger注释已写在 A 自己的 handler 源码中，公共 openapi 产物未生成或替换。验证范围见[F1 读取与 HTTP 执行记录](../TDD/SUMMARY.md#stage-06)。

<a id="model-read"></a>

## 读取与版本投影

- ModelReadStore 提供列表、当前详情、版本列表和单版本读取。读取不取行锁，不写阅读/操作状态；全体内部人员的认证在 HTTP/公共装配校验。
- 每个仓储方法的计数、分页和资料读取必须处于同一 SQL 快照。提供 REPEATABLE READ / READ ONLY 事务运行器；借用外层事务时调用方须保证同等快照与不发布未提交写入。
- keyword 对模型名称、公司和官方ID做不区分大小写的字面子串搜索，%/_不作通配符。公司/类型多选为维度内OR、维度间AND，供应商和区域在同一资源EXISTS中同时满足；关联供应商区域必须与资源一致。无关联筛选时保留无资源模型。
- 所有筛选先于总数及分页，不按资源重复模型。排序列和方向白名单；NULLS LAST，ID升序稳定打破平局。版本按version_no降序；空页仍返回完整total。
- 版本JSON严格识别contract=supply.standard_model、schema_version=1及完整model，校验行model_id/version_no与快照一致、版本不超当前版本。不把旧对象或不兼容格式静默当有效历史。
- 当前详情和历史读取均执行资料及元数据完整性检查，不填假维护人或核验人。历史是否可用于报价由现有 ModelReader 标记为不可供当前报价。

本契约不等于生产装配完成。公共增量、来源策略、认证及公共幂等仍需落实；候选结构验证使用专用库临时表，不改 public 结构。

<a id="model-write"></a>

## 写入与不可变快照

ModelUnitOfWork 通过注入 resolver 加入调用方已有 sql.Tx，公共日志通过同一个 tx 的回调写入。不自行 Begin/Commit/Rollback；返回模型只是外层提交前的候选，外层错误必须回滚。

- 显式选择 schema，生产应为 public；仅接受 SQL 标识符，不接受 SQL 片段。测试使用 PostgreSQL 会话临时表 pg_temp，不补齐 public 业务表。
- 标准模型依赖 maintainer_id/revision/version_no/verify_status/checked_by 增量列；不存在即 SQL 失败，不填默认归属或版本。
- NextID 使用目标 identity 序列；Create 用 OVERRIDING SYSTEM VALUE，兼容 GENERATED ALWAYS。新建归属由现有领域服务取认证创建者。
- GetForUpdate 以行锁读取全部当前资料和授权/CAS/核验元数据，不从版本 MAX 推断当前状态。
- Update 保留创建时间和维护人，WHERE id/revision 做 CAS；0 行冲突。数据库官方身份唯一冲突映射 ErrIdentityConflict，版本唯一冲突映射 ErrRevisionConflict，不截断字段。
- AppendVersion 只 INSERT，不修改已有快照。snapshot 为版本化对象：contract=supply.standard_model，schema_version=1，model=完整 StandardModel；其中 ID/revision/version_no 为字符串，价格仍为十进制字符串。旧快照不得据此直接当新格式读取。
- 当前投影、版本和公共日志必须在同一外层事务；NOOP 只写日志。SQL 失败后调用方不得继续使用失败事务或发布候选结果。

本切片不装配生产主路由，不实现公共转交，不替代来源核验策略，不交付公共迁移。临时目标表测试证明 SQL 适配与事务行为，不证明运行库已具备这些列、外键回填或公共幂等提交已达标。

<a id="model-assembly"></a>

## 仓储组合

### 组合入口

公共增量结构和数据检查满足后，使用：

```go
repository, err := supplypg.NewModelRepository("public", supplypg.ModelReadTransactions(db))
if err != nil { return err }
reader := supply.NewModelReader(repository)
// 由公共装配把 reader 注入 SupplyDeps.Reader。
```

ModelRepository 同时满足 ModelReadStore 和 ModelFilterOptionsStore，避免列表可用而筛选选项缺失。二者使用相同 schema 及事务运行器，每次仓储调用在独立一致快照内完成；借用公共事务时仍须保证同等隔离，不独立提交写事务。

筛选选项的旧 `NewFilterOptionsStore(db)` 入口保留 public 语义及只读事务，以兼容资源读取能力；组合入口复用同一查询构造与解析逻辑，不复制一套筛选业务规则。任何SQL失败均返回错误，不转换为空选项成功。

<a id="official-source"></a>

## 官方来源配置

构造函数接受company→HTTPS origins数组，origin为协议/精确域名/端口；不隐式放行子域名，不按字符串后缀匹配。默认443与省略端口等价。不硬编码厂商、域名或自动爬取成功；主装配须提供经审阅的配置。

Validate逐条核对资料来源（含ModelService加入的价格项source_url），公司无配置、来源为空或任一URL不在允许范围时返回ErrSourceRejected。未构造策略的依赖故障保持ErrDependencyUnavailable；取消上下文原样传播。错误不包含原URL/查询参数。

只允许HTTPS/DNS域名，拒绝嵌入凭据、控制字符和常见密钥查询参数；配置中的路径/查询/fragment、通配符、IP地址拒绝，避免把页面路径误当来源范围。配置内部深拷贝后不可被调用方修改。

该策略仅在人工核验命令中确认来源归属，不抓取、不验证网页内容真实性/实时性、不设置未经确认的有效期。人工核验仍由维护人执行、留痕并产生版本；资料补齐或域名匹配本身不自动核验。

<a id="model-operation-logs"></a>

## 标准模型操作记录

### 标准模型日志

内部人员可读，未登录 401、供应商门户身份 403。三个接口已在 A 的 `router_supply.go` 注册：

| 方法及路径 | 返回 |
|---|---|
| GET `/api/internal/standard-models/operations` | 全部标准模型操作摘要分页 |
| GET `/api/internal/standard-models/:id/operations` | 指定模型操作摘要分页 |
| GET `/api/internal/standard-models/:id/operations/:operationId` | 指定模型的一条操作及安全修改前后快照 |

列表参数为 `page`、`size`（默认 1/20，上限 100）、`sort_by`（`id/action/operator_name/created_at`）、`order`（默认 `desc`）。排序空值末尾，同值按日志 ID 降序。未知/重复参数、非法 ID 或排序返回 400。详情必须同时匹配日志 ID、模型 ID 与 `entity_type=standard_model`，其他归属返回 404。

摘要字段为 `id/entity_id/model_name/action/operator_name/reason/created_at`。日志 ID、对象 ID 始终为十进制字符串，不经 JavaScript Number。`model_name` 取日志 after 的 `data.name`，缺失时取 before；不会用模型当前名称替代历史，也不依赖当前模型仍存在。旧日志没有历史名称时返回空字符串，页面回退显示模型编号。

详情在摘要上增加 `before/after`，每项为 null 或 `{id,data,version_no,revision,verify_status}`；ID、版本、revision 仍为字符串。只解码、验证既有标准模型结构并输出已知资料字段，不透传原始日志 JSON、未知字段或配置凭据。旧格式无效、缺失或模型归属不匹配的快照返回 null，页面明确提示未保存可用快照，不伪造历史。

标准模型列表/维护页已切换到此 A 查询；公共 `/internal/workbench/operations` 的其他业务和旧数字 ID 响应不在本轮修改范围。

<a id="model-filters"></a>

## 多值筛选

## v0.22 多值筛选补充（2026-10-20）

后续已实现supplypg.FilterOptionsStore的真实目录查询：公司来自全体标准模型，供应商通过资源与所选区域关联，先去重再截断；真实库已验证无资源公司、多区域、单类截断和默认前50项外搜索，见[SQL验证记录](../TDD/SUMMARY.md#stage-12)。当前只实现ModelFilterOptionsStore能力，

GET列表增加 companies/model_types/supplier_ids/regions，每个query值为JSON字符串数组，例如 `companies=["OpenAI","Company, LLC"]`、`regions=["CN","OVERSEAS"]`。不用逗号拆公司名称。每维最多50个非空、不重复值，单个公司最多200字符；ID仍为canonical bigint字符串，类型与区域使用既有枚举。空数组表示该维度不限；同维度OR、跨维度AND。

旧company/model_type单值参数可继续使用，但不允许与对应复数参数同时指定非空条件。复数条件规范化后传给仓储；公司名保留大小写进行精确选择，keyword另作文本搜索。

供应商和区域筛选必须通过同一资源的EXISTS关联实现，不给standard_model添加region列，也不以两个独立EXISTS分别命中不同资源。过滤和模型去重先于排序/分页，total为符合条件的模型数。未指定供应商/区域时，没有资源的标准模型仍可见。

GET `/api/internal/standard-models/filter-options` 接收keyword、regions（相同JSON数组编码）。返回 companies、suppliers[{id,name,region}]、has_more，每类最多50项。公司来自标准模型全集（包括无资源模型），供应商来自关联资源及有效供应商身份，regions仅限定供应商选项。keyword按名称匹配。达到上限仍有匹配时has_more=true，不从当前列表页构造选项。身份和普通读取相同，全体内部人员可读；缺选项仓储能力503。
