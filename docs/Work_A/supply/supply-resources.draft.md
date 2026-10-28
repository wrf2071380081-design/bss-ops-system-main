# F3 资源与模型广场契约

<a id="resources"></a>

更新：2026-10-27。真实来源/办理资格、指定报告、配置绑定/逐资源接入/改价回写及批量办理已有指定场景验收；Inspector真实出站/回收仍暂缓。 当前迁移与部署见[交接](./handoff.md)，执行结果只维护[TDD SUMMARY](../TDD/SUMMARY.md)。下文保存业务/接口/存储约束，不再逐阶段重复测试结论。

## 最新业务口径

DEC-020：至少一行有效连通通过即可提交，压力不作为批次提交前置。DEC-021：B可为草稿测试分配物理resource；A正式读取排除无RESOURCE_REVISION的测试专用分配，批准后同ID接管并防重。每条资源登记接入仍须自身双测通过、匹配目标/来源版本、正式配置及当前节点人员资格；模型不完整不直接阻止登记接入，但阻止报价。

多模型逐条校验并返回部分成功/失败。流程为登记配置完成→确认已开通；取消只针对未接入行，保留资源与历史，已接入资源走下架计划。当前采购人、领导审批资格或人员筛选不产生节点代办权。

<a id="resource-http"></a>

## 业务方法、快照及HTTP

### 1. 已确认的业务规则

1. 草稿只保留申报行和测试记录；提交进入办理后才创建资源。
2. 每个资源自己的连通性和压力测试都通过，才可登记接入并供报价使用。批次至少一条通过的提交门槛不能替代逐资源准入。
3. 资源具有独立的 supplier_id、standard_model_id、region；身份三元组创建后不通过常规编辑替换。
4. coefficient 是 decimal 字符串，范围 `(0,1]`，最多四位小数；零、大于一及超精度输入拒绝。
5. 正式配置与申报测试凭据分开；不能将申报测试 URL/Key 自动复制成正式配置。
6. 正式接入还需满足有效正式配置、适用办理条件与绑定授权；测试通过不等于直接可用。

第 3—6 项来自任务及详细设计；人工直建资源、人工直改系数、复测失败处理和证据有效期仍需按 D02 后续对齐，不能据本草案自动开放额外写入口。

### 2. A 提供给 B/C 的方法

| 方法 | 输入 | 结果与规则 |
|---|---|---|
| GetStandardModelSnapshot | model_id、可选 version_no | 官方身份、核验状态、价格项及版本；不混用旧 SKU ID |
| GetResourceSnapshot | resource_id、region | 下述安全资源快照及当前可用性 |
| ListSupplierResources | supplier_id、region、分页 | 指定供应商资源安全列表，门户主体归属由 B 鉴权 |
| CreateFromSubmission | 共享事务上下文、认证 actor、申报行及版本、主体/模型/区域、供方模型名、系数、buyer_id、reason | 防重创建，按自身当前测试事实选择 PENDING_TEST/PENDING_ACCESS；不自动 ADMITTED |
| AttachTestEvidence | 共享事务上下文、受信任 B 调用上下文、resource_id、报告引用、测试目标版本与任务顺序 | 迟到报告不替换新目标的当前展示依据；不能只按回调时间排序 |
| Admit | 共享事务上下文、actor、resource_id、expected_revision、flow/node 引用、reason | 核对行、资源、流程、配置及自身测试事实后推进接入；失败全部回滚 |
| BindFormalConfig | 共享事务上下文、actor、resource_id/revision、配置 ID/revision、reason | 同供应商且区域匹配；只建立正式关联 |
| ChangeCoefficient | 共享事务上下文、actor、resource_id/revision、coefficient、来源引用、reason | B 调价申报与 A 计划共用领域命令；人工入口政策另定 |

actor 必须区分内部人员、供应商和系统来源，不能仅用可能撞号的裸 operator_id。具体 Go 签名与公共认证/事务接口核对后冻结。

### 3. A 读取的 B 事实

B 的 AdmissionEvidence 至少需能表达：

- 所属 supplier、standard_model、region，以及关联的 submission_row 和不可变目标版本。
- 对应资源引用（资源创建前可不存在）、报告引用及任务顺序。
- 连通性与压力各自的结论，目标是否与当前待接入对象一致，以及证据有效性和阻断原因。
- 测试配置/目标指纹的安全标识，不返回测试原始密钥。

B 的 OnboardingReadiness 需表达资源是否属于该 flow/node、当前 actor 是否办理人以及适用节点是否完成。A 从服务端读取这些事实，不信任 HTTP body 自报 `flow_completed=true` 或 `test_passed=true`。

未接入事实提供方时，正式写流程返回依赖不可用；不能用默认“通过”的适配器启用资源。受控 fake 仅用于独立测试。

### 4. 给 C 的安全快照

建议字段如下；标识和版本号保留字符串，不含 ORM 对象或供应商秘密：

```json
{
  "resource_id": "12001",
  "supplier_id": "2001",
  "standard_model_id": "1001",
  "region": "OVERSEAS",
  "coefficient": "0.6800",
  "resource_revision": "4",
  "model_version_no": "2",
  "formal_config_revision": "3",
  "state": "PENDING_ACCESS",
  "ever_admitted": false,
  "eligible": false,
  "blocked_reasons": ["ONBOARDING_NOT_FINISHED"],
  "official_price_items": [],
  "test_evidence_ref": null
}
```

样例表示不可报价资源，空价格及空证据不表示其满足准入。原因标识为草案，不在此自行分配公共业务错误码。

可报价必须同时核对资源接入状态、主体和区域、模型核验/价格、有效正式配置及自身测试证据。C 在新的开通或报价写入时重查当前快照，不能仅依赖旧投影和浏览器缓存。

正式 URL、API Key、网站账户密码和完整测试凭据不进入上述快照。价格项不得丢弃币种、单位数量、适用条件或稳定 item_key；A 不替 C 计算跨币种最低价。

### 5. HTTP 读取与模型广场

| 接口 | 契约 |
|---|---|
| GET `/api/internal/resources` | view=grouped（默认）或 flat；两种响应行 DTO 明确分开 |
| GET `/api/internal/resources/:id` | 安全详情、配置摘要、报告摘要、quote_readiness、revision、服务端操作资格 |
| GET `/api/internal/resources/:id/test-reports` | 通过 B 的报告读取接口提供历史摘要及受保护访问方式，不复制报告正文作为第二来源 |

筛选：company、model_type、supplier_id、region、state、keyword；分页默认 1/20，上限100。排序字段采用白名单；未知字段 400，空值始终末尾，并以稳定 id 次序打破同值。

grouped 按标准模型分组，同一过滤范围计算供应商去重数、资源数、可用数、系数最小/最大及状态分布。先对模型分组再分页，不能先截断资源页再聚合。区域筛选不能把另一地区的资源计入数量或区间。

flat 表示每条资源。其排序、分页及 total 不套用 grouped 的模型计数。精确排序字段名单和完整 HTTP 样例与前端列表一起评审后冻结。

### 6. 状态与事务接入

详细设计建议的实际状态为 PENDING_TEST → PENDING_ACCESS → ADMITTED → OFFLINE；计划标签是派生信息，不让未来计划立即改变当前可用性。状态枚举须与目标 DDL核对。

进入办理创建时，B 的行 resource_id 写回与 A 的资源、防重及日志共享事务。接入时，B 的办理/行状态与 A 的资源状态也共享事务；任何验证、日志或 outbox 写入失败全部回滚。

当前基础代码已有 `internal/infra/db.WithDB/FromContext`。它是可复用机制，不代表 UnitOfWork、operation_log 和 B/C 适配器已实现。领域代码不直接依赖 B/C 具体服务类型或在内部另开独立事务。

下架通过 outbox 通知 C，A 不直接修改 customer_link；C 按 event_id 幂等处理。是否及如何恢复客户关系仍由 C 的业务契约决定。

### 8. v0.22 对齐补充

最新依据为原型 v0.22 及原路径 A 详细设计的 v1.1。前文涉及 `supplier_model_name` 的输入应改为明确的接口模型 ID `supplier_model_id`；不能由官方模型名自动猜测供应商接口标识。旧目标 DDL/申报行到资源的映射需与 B/公共方核对后冻结。

资源安全详情及 B 的报告摘要补充 `cache_stats`：report_id、status(PROVIDED/NOT_PROVIDED/NOT_APPLICABLE)、hit_tokens、input_tokens、hit_rate、scene。计数为非负整数字符串，PROVIDED 要求 input_tokens>0 且 hit_tokens<=input_tokens；比例派生，页面直接按原始计数计算百分数后一次舍入到两位。0/2000 是0.00%，不是缺失值。

列表只使用当前选定报告的统计；历史详情按所查 report_id 返回，不从其他报告借值。B 负责原始报告写入和校验，A 不新增可独立编辑的资源缓存统计。该指标不增加接入或报价的数值门槛。

模型广场和供应来源导航集中在“详情”列，真实状态操作独立保留；按过滤结果聚合并按模型分页。工作台的人员查看不会改变资源操作身份。

### 9. 读取切片的具体端口

GET resources 新增 `standard_model_id` 过滤用于展开单个模型的资源来源；展开时保留原区域/供应商等过滤条件。grouped默认按name升序，flat默认按coefficient升序；双向空值末尾，稳定ID打破同值。精确排序白名单见ResourceListQuery校验。

每条读取投影由resource、当前standard_model、供应商安全事实、正式配置安全事实、选定报告摘要构成。目标版本、来源申报行及版本必须与报告对应；旧目标报告不作为当前测试或缓存依据。B适配失败返回依赖错误，只有确知未测试才允许空报告。

缓存 hit_rate 保留6位比例，另返回直接由原始计数一次舍入的 hit_percent（两位百分数）。例如246899/2000000的比例摘要0.123450，百分数应为12.34而非对已舍入比例再乘100得到12.35。

grouped响应为模型id/name/official_id/company/model_type、supplier_count/resource_count/available_count、coefficient_min/max、regions及state_counts。flat响应为资源安全详情，不含正式base_url、密钥或网站账户。grouped的total表示模型数，flat的total表示资源数。资源写命令、计划及B报告历史读取后续切片接入，不因读代码完成而视作F3整体完成。

测试摘要增加 `test.metrics.connection_ms/rps/tps`，为可空十进制字符串；真实0保留原字符串，缺失为null。flat排序白名单包括id/supplier_name/region/coefficient/state/updated_at/connection_ms/rps/tps/cache_hit_rate；grouped包括id/name/company/model_type/supplier_count/resource_count/available_count/coefficient_min/coefficient_max。API统一包络data含view/list/total/page/size。

读取与HTTP/仓储/主入口已装配；实际交付及未完成项见[F3读取记录](../TDD/SUMMARY.md#stage-10)。

### 10. 广场远程筛选选项

已有独立SQL目录适配supplypg.FilterOptionsStore，按新表完整资源/模型/供应商关系筛选，在同一只读快照查询公司和供应商，使用EXISTS去重、Limit+1判断has_more。真库已覆盖区域、重复来源、55项截断和搜索，见[SQL验证记录](../TDD/SUMMARY.md#stage-12)。尚需组合进完整ResourceReadStore，不能将该目录类单独冒充完整资源仓储。

GET `/api/internal/resources/filter-options?region=CN&keyword=名称片段`，仅内部人员读取。data为 `{companies: string[], suppliers: [{id,name,region}], has_more: boolean}`；ID字符串，不含配置或凭据。region可空，keyword按公司/供应商各自名称匹配。

每种选项最多50项，截断时has_more=true；应在完整新资源/模型/供应商关联上按区域和名称过滤后取有限匹配，不从资源当前页猜测全集。仓储实现独立的可选能力ResourceFilterOptionsStore；未提供时503，不回退旧supplier_profile。

前端按名称查找并提交已选择供应商ID，展开来源时固定继承已应用筛选并重置资源页码/排序。

### 11. 统一改系数命令独立实现

ResourceCoefficientService.Change接收认证WriteMeta、资源ID/revision、系数和CoefficientSource{Kind:SUBMISSION/PLAN,ID,Revision}。ResourceCoefficientTransaction在同事务读取并核对真实来源授权、目标资源/revision/金额；来源引用不是客户端授权凭证。PLAN按原发起人验证，不错误要求其仍是当前buyer；人工MANUAL入口尚未开放。

实际变化CAS升资源revision，保留测试目标版本与报告，写前后快照日志和系数变更outbox；无变化只追加NOOP日志，保留revision/updated_at。调用方来源完成、资源、日志和outbox原子提交，提交失败返回nil。事件ID由公共outbox分配，C不因该命令被A直接改写原报价。具体端口及当前证据边界见[阶段记录](../TDD/SUMMARY.md#stage-24)。

### 12. 直接上下架领域命令

> DEC-010已确认严格计划流程：以下直接HTTP/页面入口现已撤下，领域代码仅供后续计划执行重构复用。当前实施以[纠正记录](../TDD/regression-scenarios.md#2026-10-20-plan-only-correction)为准。

ResourceStateService.Change仅供绑定采购人直接OFFLINE/ONLINE，参数包含external_confirmed和completion_note；未实际确认或无依据拒绝。计划来源不能用客户端plan_id绕过buyer校验，需后续独立的发起人验证执行服务。

ONLINE复用当前安全快照可报价判定，在候选ADMITTED副本重查模型、主体、配置及自身测试；OFFLINE不因上游资料失效而阻止采购人停止资源。两者保留ever_admitted、目标与报告，CAS升revision。操作记录、公告和outbox由同事务效果端口写入，公告影响PENDING。具体证据及未接入项见[上下架领域记录](../TDD/SUMMARY.md#stage-26)。

历史POST `/api/internal/resources/:id/state` 及直接登记页面已按DEC-010从A注册器撤下；[HTTP记录](../TDD/SUMMARY.md#stage-25)仅保留历史证据。当前采用创建计划→原发起人登记实际完成；已有计划创建/修改/取消独立能力，执行HTTP仍未开放，最新进度以[TDD索引](../TDD/README.md)为准。

### 13. 模型供应资源操作记录与计划定位

GET `/api/internal/standard-models/:id/resource-operations?page=1&size=20`：内部人员读取该标准模型下全部供应资源的操作记录；可选 `resource_id` 缩小到一条资源。范围独立于供应来源表的性能/状态筛选，页面明确说明。查询在数据库按模型归属过滤后分页，按 created_at DESC、id DESC 稳定排序；模型不存在404，指定资源不属于模型404。返回 `{list,total,page,size}`；每行仅含字符串 id/entity_id/resource_id/supplier_id、entity_type(resource/plan)、supplier_name、supplier_model_id、region、action、operator_name、reason、created_at，不返回 before/after、凭据或测试原始数据。此入口汇总 resource 实体日志及通过 plan.resource_id 关联的计划日志，不包含模型资料或供应商级配置日志；模型资料日志仍走标准模型库。

A 资源深链支持 `?tab=plans&plan_id=字符串ID`，直接读取并打开该计划，不依赖计划位于列表当前页；计划详情必须核对其 resource_id/region 与资源上下文。未知或跨资源计划显示错误，不改开其他计划，也不改变认证操作者。公共待办暂未提供 plan_id 时仍打开资源计划列表，不猜测计划ID，不开放实际执行。

验收：多供应商资源日志一次汇总、模型间隔离、分页顺序/安全字段；指定计划可直接打开，跨资源计划不展示或操作。

<a id="resource-read"></a>

## 资源读取与聚合

### 数据与事实来源

- SQL 只投影资源的非敏感业务列及供应商身份；不读取测试 URL、测试密钥或供应商账号密码。`PENDING_ADMIT` 映射为领域 `PENDING_ACCESS`；未知及旧 PLAN 状态不猜测实际上下架结果。
- 标准模型使用 F1 当前列＋不可变版本快照的真实读取，同一事务内校验；缺历史初始化不能伪造已核验。
- `ResourceFactsLoader` 在同一 SQL 事务内读取已经持久化的 A 控制元数据、B 来源与选定报告引用，以及正式配置安全摘要。资源 revision、来源/目标版本、正式配置绑定不能从 updated_at、报告数量或页面输入推算。
- A控制元数据已提供真实 `ResourceStateStore.FactsLoader`，从现有operation_log的保留版本化快照读取，协议及创建服务装配见[控制快照存储](./supply-resources.draft.md#resource-control)。正式配置与B报告来源仍须注入各自真实提供方。
- 提供方返回资源 ID、ProjectionUpdatedAt 及随 revision 持久化的完整非敏感资源投影。逐列与原生行比较（十进制数值和时刻规范化），不一致拒绝读取；即使有人改原生系数却不更新 updated_at，也不能搭配旧 revision。不能在读取时拿当前行填造历史投影。供应商当前状态独立读取，不要求每次冻结供应商都改资源 revision。时间仅用于一致性检查，不能作为 revision。来源或版本缺失返回错误，不包装为无测试或可用。
- 已选报告必须与原生 latest_test_report_id 一致，且 ref 的资源/供应商/模型/区域和任务顺序一致，再调用真实 ReadSelectedReport。报告目标较旧可以展示 STALE，不能引用其他报告指标。
- 无选定报告与缺事实提供方不同：前者正常显示未测试，后者依赖不可用。正式配置缺失/失效保留领域阻断原因，不自动授予报价资格。

### 列表语义

1. SQL 绑定全部筛选值，在完整匹配范围读取资源；关键词为字面子串，百分号和下划线不作为通配符。关键词最多200字符、公司最多128字符，非法UTF-8/NUL或超长由领域边界返回400，不等到SQL适配层报500；选项关键词使用相同限制。
2. 在同一个只读 REPEATABLE READ 快照组合模型、控制元数据、配置与报告，构建并验证安全快照。
3. flat 按全部匹配资源排序后分页；grouped 按标准模型分组，按同一范围计算去重供应商数、资源数、真正可报价数与系数范围后排序分页。每组返回完整成员，不能先分页资源再拼组。
4. 排序白名单、NULLS LAST，主键数值升序作为稳定补序；十进制和缓存原始计数精确比较，不转 float。字符串采用 Go 字符序。
5. 当前适配器为保证外部事实参与可用数量与排序，物化本次完整筛选结果；不以隐藏上限截断总数。不宣称大数据量优化已完成；后续可在可信事实形成可查询投影后将同等语义下推 SQL。

提供方不在事务内调用远程服务。公共装配必须注入真实事实实现；测试提供方仅证明组合契约，不代表 B、正式配置或生产主路由已装配。读取不写日志、不改变状态，不承接审批或分派。

<a id="resource-control"></a>

## 控制快照与锁序

### 载体与协议

- `resource` 保存已有业务投影；revision、来源行/目标版本、正式配置绑定版本、选定报告任务序号随完整非敏感资源快照保存到现有 `operation_log.after`。
- 控制记录固定 `entity_type=resource`、`action=RESOURCE_REVISION`，JSON 为 `contract=supply.resource`、`schema_version=1`、`operation=具体业务动作`、`resource=完整安全资源状态`。所有ID和版本用字符串表达；时间统一UTC及数据库微秒精度。
- 节点办理动作增加可选proof，保存flow_id、node_id、assignee_id和字符串flow_revision；资源来源行/版本和目标版本已在resource中。proof人员必须与操作日志actor一致；普通创建/其他不适用操作不凭空填造节点依据。
- 测试关联可选test_evidence保存本次安全报告引用、任务序号、来源/目标版本及处理结果；ADMIT保留completion_note与节点proof。更新控制头后会核对这些必要依据未被日志适配丢弃，字段不包含URL、凭据或报告正文。旧目标/旧任务/NOOP只留普通日志，不覆盖当前控制头。
- 该保留动作只承载真实资源变更，不用于普通浏览、NOOP或其他业务日志。真实业务动作保留在operation字段；日志展示接入需读该字段。before同样是旧控制快照，创建时为空。
- 同一资源先锁原生行，再以新语句读取最新控制记录（按日志ID倒序），revision直接取已保存JSON，不从时间、行数或最大版本临时推算。日志ID只确定同资源串行提交的记录顺序。
- 最新记录协议/字段损坏或原生投影不符时拒绝，不能退回较旧有效快照。没有控制快照的历史资源也拒绝，不自动认领或补初始revision。
- 这些记录属于业务状态依据，必须追加且保留，不能按普通临时日志清理；若公共日志未来归档/裁剪，需先显式迁移权威状态并保持同等读写协议。本实现不改公共日志管理策略，也不偷偷回填旧资源。

### 写入与并发

`ResourceStateStore`提供预分配ID、原生插入＋控制日志、锁定读取、expected revision更新＋控制日志等底层能力；不提供HTTP写入口，不代替已有领域准入和绑定校验。

创建只能由已校验的申报办理服务调用；领域初态仍为PENDING_TEST/PENDING_ACCESS。原生SQL的PENDING_ADMIT与领域PENDING_ACCESS互相映射。供应商/模型/区域唯一约束继续生效，不绕过已有identity。

更新锁资源，核对当前完整快照与原生列，比较expected revision；新revision必须恰好+1，资源ID、供应商、模型、区域及创建时间不变。原生写与控制日志使用调用方同一sql.Tx。日志适配只接收无密钥的结构；其成功返回后重读控制头，确认完整快照确已保存，不能把空回调当成功。

每个方法不自行Begin/Commit/Rollback。调用方发生任何错误必须整体回滚，且在外层提交成功后才发布HTTP/幂等响应。持有锁期间不发网络请求。真实B行回写、节点结果和outbox仍要加入同一外层事务，不能因A存储完成而省略。

### 申报创建事务适配

`NewResourceCreationWork(store, resolve, ResourceCreationHooks)` 接入既有ResourceCreationService。A重新从SQL读取供应商/模型及现有资源，不把B响应中的旧Supplier/Existing当作权威A状态；重复创建返回当前控制快照，不重建状态或重置revision，追加普通CREATE_NOOP日志，不写新的控制头。

LockSource、AttachRow及必要的PrepareReport由B提供，在传入的同一sql.Tx内完成真实身份/不可变版本及分派验证、申报行CAS和草稿报告转存。LockSource须实现跨模块统一的锁序与不存在资源时的身份键保护：已有资源的办理、创建重读与接入必须统一为资源锁在B行/节点锁之前；不能先单独锁B行，再与持有资源锁的办理事务互相等待。可以先非锁读取不可变来源身份，按统一顺序取得资源/身份键及B锁后重读并核对版本。当前测试的B回调是夹具，不证明该真实锁协议已交付；上线前须共同验证创建与办理/接入的竞争。

原生report要求resource_id非空且有FK，因此草稿报告不能提前伪造资源。创建先插入latest_test_report_id为空的真实资源行，然后PrepareReport将已选草稿证据转存为该资源的原生报告，再以真实SQL核对报告/任务归属及连通/压力结论与原可信证据一致，最后关联报告并保存控制头。没有转存提供方、报告结论被替换或B行回写失败，均返回错误并要求外层整体回滚。既有报告ID须由B按共享ID空间保持引用，不静默替换选定报告。

资源进入PENDING_ACCESS仍不是ADMITTED，正式配置另行绑定，报价仍受模型核验、逐资源测试和正式配置等条件约束。此适配不新增浏览器直建资源入口，也不实现供应商报价审批。

### 读取组合

`ResourceStateStore.FactsLoader(reportLoader,configLoader)` 从真实控制记录组装前一切片的ResourceReadFacts。无报告/无配置绑定不调用对应提供方；有引用而没有对应真实提供方返回依赖不可用。B报告引用和正式配置安全摘要仍由各自可信适配提供，不存凭据、不假定通过。

登记接入组合读取将原生行的带时区时间规范为UTC/微秒后再逐字段比较，不以Go time.Location标记不同误判同一时刻为不同版本；保留身份、业务字段、revision与控制头的严格一致性检查。真实A组合链路与事件验证见[证据/接入适配](./supply-resources.draft.md#resource-transactions)。

<a id="resource-creation"></a>

## 资源创建与正式接管

### 输入与可信来源

命令仅指定申报行ID及不可变行版本。认证WriteMeta提供人员、原因、追踪号；不接受页面直接指定supplier/model/region/buyer或测试结论。

B在共享事务内读取并锁定申报行及办理事实，提供SubmissionResourceProof：row_id/version、target_version、supplier_id、standard_model_id、region、接口模型ID、采购系数、已分派采购人、flow_id/revision、当前调用的授权人员，以及审批/分派已落实且进入办理的事实。ApprovedAndAssigned与EnteredHandling是B服务端事实，不是HTTP入参；B负责按真实审批和一次性分派生成。

资料草稿、未完成审批分派、尚未进入办理均不能创建正式资源。模型与供应商身份须有效，供应商区域须匹配；已有模型未核验不阻止建立待接入来源，但后续报价仍阻断。

### 初态

资源默认PENDING_TEST、ever_admitted=false、revision=1，不自动ADMITTED。B提供当前行自身报告时，报告的供应商、标准模型、区域、行/版本及目标版本必须相符；B草稿测试可以分配真实物理resource用于外键；A批准后接管同一ID，不将测试分配视作正式接入（DEC-021）。仅有效且连通与压力均通过时初态PENDING_ACCESS，失败或无报告仍PENDING_TEST。缓存数据不设准入阈值。

保留自身选定报告引用便于追溯，不复制报告正文/凭据。正式配置另行绑定，申报测试配置不复制为正式配置。系数为(0,1]的最多四位十进制，保存规范化四位字符串。

后续证据关联需要可比较的B任务序号：初建带报告时同步保存Report.Sequence与LatestReportSequence；无真实序号拒绝写入，不用报告ID或时间猜测。序号体系须从草稿行进入正式资源后连续可比较，见[测试证据关联契约](./supply-resources.draft.md#test-evidence)。

### 防重与事务

共享事务以来源申报行和(supplier_id,standard_model_id,region)共同防重；不存在资源时也须有来源/身份键锁及数据库唯一约束防并发双建。已有资源必须同时匹配同一来源行/版本和身份，才作为同一创建意图的重读，不能把另一条申报静默接管现有资源。

重复创建不重置资源状态、版本、采购人、报告或修改时间；返回当前资源及created=false，新的非幂等重放请求意图仍记CREATE_NOOP留痕。真正幂等重放由公共中间件返回原结果。

新资源、B行resource_id回写与操作日志在同一事务。CREATE_NOOP只允许补回相同资源引用，禁止覆盖B已有不同绑定。日志、回写、唯一冲突或提交失败须整笔回滚，不留下单边记录。资源ID由公共数据库分配，需支持GENERATED ALWAYS身份列，不在A自行使用时间戳ID。

<a id="config-binding"></a>

## 首次正式配置绑定

### 范围与资格

本切片只覆盖尚未接入的PENDING_TEST/PENDING_ACCESS资源绑定已存在的正式配置。资源身份、测试目标及报告不因绑定而修改；不创建正式密钥，不从申报测试URL/Key生成正式配置，不把绑定成功当作外部操作或接入完成。

WriteMeta来自认证人员。命令含resource_id/expected_revision、formal_config_id/expected_config_revision；服务端共享事务提供当前资源、正式配置安全可用性事实及B的AdmissionProof。操作人员须为当前配置节点办理人，流程/节点进行中且准备及适用前置满足；资源、供应商、区域、来源行和目标版本与事实一致。审批资格、buyer身份不替代配置节点指派。

### A 注册器 HTTP 契约

`PUT /api/internal/resources/:id/formal-config` 只在A的 `RegisterSupplyRoutes` 中注册，要求内部Staff身份及 `Idempotency-Key`。请求严格为 `{expected_revision:string, formal_config_id:string, expected_config_revision:string, reason:string}`；ID及revision为规范正十进制bigint字符串。认证办理人和request_id从服务端上下文取得。客户端不能传入节点/流程、办理人、配置状态、密钥/URL或任何“已确认”布尔值。

成功返回无敏感内容的收据 `{resource_id,formal_config_id,formal_config_revision:string,resource_revision:string,changed:boolean,updated_at}`。同ID同版本的重复关联返回 `changed:false` 并保留资源revision；新的确认配置版本返回 `changed:true`。错误边界与逐资源接入一致：400请求错误、403当前节点办理权限不足、404资源/配置不存在、409版本或流程事实冲突、503依赖未装配。路由已接入，实际动作仍逐次验证运行依赖和服务端资格。

正式配置维护仍按其既有供应商采购人规则；绑定是资源关联操作，两者不混为一个授权入口。已接入/下架资源的配置变更继续走B已批准的变更流程，本命令不绕过该流程。

### 规则与事务

- 配置ID、供应商和区域必须匹配，配置ACTIVE、有正式密钥、revision为命令期望值且confirmed_revision=revision。
- 资源revision必须匹配；绑定新ID或新确认版本时revision+1，状态、ever_admitted、来源、测试目标、报告及任务序号不变。
- 同ID/同版本重复关联记录BIND_NOOP，保持资源revision/updated_at；新请求意图仍留痕，公共幂等重放由外层处理。
- 资源CAS、配置引用和操作日志在同事务；配置更新/停用、资源绑定/接入遵循公共统一锁顺序，绑定后的引用须使既有HasResourceReferences生效。
- 日志只保存资源及配置ID/版本安全引用、流程/节点证据，不含正式BaseURL、掩码以外凭据或测试数据。外层提交前返回是候选结果，提交失败不可返回成功。

已新增A原生SQL事务适配，见[正式配置存储与绑定](./supply-suppliers.draft.md#formal-config)。配置与资源事实来自真实控制头，办理资格仍由可信B proofLoader提供；锁序为supplier→resource→config→B，绑定与停用的两个真实并发提交顺序均已验证。

<a id="test-evidence"></a>

## 测试证据关联与晚到保护

### 可信输入与排序

B保留报告和任务权威来源。A命令只指定resource_id/report_id；事务适配器核对受信任B调用上下文，锁定资源并读取指定报告。授权失败返回403，缺实现返回依赖不可用；不提供浏览器自报通过结论的HTTP。

报告摘要增加Sequence，资源保存LatestReportSequence。该序号是B在提交测试任务时分配、在同一资源测试链内严格单调的正bigint，不是报告ID、回调到达时间或完成时间；草稿行测试转入资源后必须保持可比较的序号体系，目标变更不重置序号。实际列/来源映射由公共与B落实，不改已发布迁移。

资源初建若带报告，同时保存报告ID与Sequence；已有关联ID却缺序号时拒绝自动排序，必须先由真实任务关系补齐，不能猜测。

### 关联规则

- 指定报告须属于资源本身或资源的来源行，供应商、标准模型、区域、来源行身份必须一致；不借其他资源的报告。
- 报告行版本或目标版本比资源当前版本旧：记录IGNORED_STALE_TARGET，不改当前报告/资源；来自未来版本的报告拒绝，先落实真实目标变更。
- 当前目标下，序号小于当前选定序号：IGNORED_OLDER；相同ID和序号：NOOP；相同序号却不同报告ID或相同ID却不同序号：冲突，不能覆盖。
- 当前目标且更高序号：SELECTED，替换当前报告指针及序号，资源revision+1，更新时间变化。资源PENDING_TEST/PENDING_ACCESS按有效连通/压力结果切换准备状态，不自动ADMITTED。
- ADMITTED/OFFLINE资源补测只更换证据和revision，保留实际状态与ever_admitted，不凭失败报告自动下架或恢复；可报价性由已有安全快照重新判定。复测失败的进一步处理仍待产品策略，不额外增加自动动作。

每次非幂等重放调用记录选定/忽略/NOOP结果及安全报告引用，忽略和NOOP不递增资源revision。不得改写不可变报告、借用缓存数据或新增数值阈值。

### 原子性与验证范围

资源CAS与证据关联日志在B共享事务内，B自身报告保存、关联及留痕失败须整体回滚；A不另开提交。外层提交成功前返回仅为候选结果。

已新增[原生证据/接入事务适配](./supply-resources.draft.md#resource-transactions)：指定报告结论、指标与缓存来自真实SQL；B authority提供真实调用资格及不可变来源/任务序号。真库覆盖选定、NOOP、旧任务、旧/未来目标、拒绝不可信调用、日志失败以及B报告插入与A关联同事务回滚；已接入失败复测不自动下架。

与接入竞争的两个真实事务顺序均验证：失败报告先提交阻断接入，接入先提交则后续失败复测保留ADMITTED并阻断报价。真实Inspector回调及结果回收仍按平台配置另验；原生办理与A运行组合的已验范围见交接。

<a id="selected-report"></a>

## 选定报告安全读取

`ReadSelectedReport(ctx, tx, schema, ref)`加入调用方一致快照事务，不提交、不写数据。ref必须由服务端B事实读取器在同一快照提供，包含报告/任务/资源ID、供应商、标准模型、区域、来源行/版本、目标版本、任务顺序及证据有效性；不接收HTTP自报事实。

SQL严格按指定report_id读取，并检查报告resource_id、test_job关联资源、当前资源供应商/标准模型/区域均与ref相同。报告不存在返回NotFound；绑定不一致返回ReadContract；缺依赖或非法来源不得猜测通过。不能选最新时间/最大报告ID或借另一报告的指标填缺失。

connection_ms/rps/tps以nullable十进制字符串读取，真实0保留，NULL保持缺失。缓存复用34计数字段映射：缺失/非法为NOT_PROVIDED，不改写历史数据，不设命中率门槛。

有效性= B证据有效且连通性和压力结论均已提供；已提供false表示有效报告中的失败结论，不等同缺失结论。逐资源准入仍检查两项通过。B否定的有效性不能因为数据库有通过值而恢复。

可信 B 报告首次随资源创建时，以及之后由 B 关联到资源时，A 将报告 ID、任务序号、来源版本、目标版本和有效性写入资源不可变控制留痕。读取时只有与当前资源报告指针及版本完全匹配的 `SELECTED/NOOP` 留痕才能恢复有效性；旧记录缺少此留痕时按无效处理。不能仅凭 `test_report` 行存在推定有效。

本适配不实现B首次批次、自测提交判定或审批；不把此SQL读取当完整ResourceReadStore或生产装配。测试使用原生34报告表的会话临时副本，真实B事实来源尚需接入。

<a id="resource-admission"></a>

## 逐资源登记接入

### 操作与可信事实

调用命令包含resource_id、expected_revision、外部实际配置确认和完成依据；WriteMeta取认证人员、原因、请求追踪。审批资格不自动授权接入，普通买方归属也不能替代当前配置节点办理人。

办理中的资源由B在同一事务提供AdmissionProof：flow_id、node_id、flow_revision、supplier_id、resource_id、origin_row_id/version、target_version、region、assignee_id、流程进行中、节点进行中、准备与其他适用前置已完成。该结构来自服务端，不接受客户端自报布尔值作为授权证据。报价审批/分派尚未完成时，B不得提供进行中的已分派节点。

### A 注册器 HTTP 契约

`POST /api/internal/resources/:id/admit` 只在A的 `RegisterSupplyRoutes` 中注册，要求内部Staff身份及 `Idempotency-Key`。请求严格为 `{expected_revision:string, external_confirmed:true, completion_note:string, reason:string}`；资源ID和revision均为规范正十进制bigint字符串。认证人员和request_id由服务端上下文取得。不得接受flow/node/assignee、测试通过、审批资格或supplier_id等客户端自报事实。

成功返回不含配置/凭据的收据 `{resource_id,state:"ADMITTED",ever_admitted:true,revision:string,updated_at}`。仅在原资源、来源行和B节点办理事实的共享事务成功提交后返回。缺服务503，身份/文本错误400，非当前节点办理人403，状态、版本或业务前提冲突409。路由已接入，实际动作仍逐次验证运行依赖和服务端资格。

本切片只实现有办理流程的初次接入；无流程人工建资源的授权策略尚未确认，不开放后门。PENDING_TEST须先经测试事实同步到PENDING_ACCESS；该同步由后续AttachTestEvidence衔接，Admit不暗中改写报告或跳过准备状态。

### 业务前提

- 资源PENDING_ACCESS且ever_admitted=false，身份和expected_revision一致，revision可递增。
- 当前认证人员是配置节点办理人，流程属于该供应商、区域与资源，来源申报行及版本、测试目标版本一致；流程/节点进行中且适用前置满足。
- 正式配置为同供应商、同区域、有效且当前已确认版本，包含正式凭据；不能把测试凭据复制成正式配置。
- 选定报告为该资源或其自身来源申报行/版本，当前目标匹配、证据有效、连通和压力均通过。不使用批次通过代替逐资源结果，不增加缓存阈值。
- 模型资料核验是可报价条件，不在此新增“先核验才能登记接入”的额外门槛；接入后仍由已有安全快照独立计算quote_readiness，未核验模型不可报价。

### 事务协议

ResourceAdmissionTransaction按共同约定锁定资源及对应办理事实，读取当前模型/供应商/正式配置/选定报告。A验证后CAS更新资源为ADMITTED、ever_admitted=true并递增revision；不更换资源身份、来源、目标版本、报告或采购人。

事务效果端口同事务保存B的逐行接入事实、该次授权办理引用、操作日志、完成依据及资源接入事件；B按其完整节点规则判断是否所有行完成，A不因一行成功就完成整批流程或设supplier.established=true。事件ID和投递由公共outbox适配，不直接修改C的客户关系或报价。

业务、B状态、日志或事件失败须整笔回滚。UnitOfWork加入外层事务时返回仅代表候选结果，外层必须提交后响应；公共幂等重放发生在业务命令前，不能靠重复ADMIT返回假成功。缺证据适配返回依赖不可用。

<a id="resource-transactions"></a>

## 事务与事件

### 测试证据

TestEvidenceWork加入调用方sql.Tx，按supplier→resource→B来源的共同顺序取得事实。B授权适配返回明确可信结论和指定报告引用；不可信调用403，缺实现503。A再从真实test_report/test_job读取该份报告的结论、指标与缓存并核对业务身份，不相信页面自报通过。

选定当前证据时CAS更新资源和保留控制快照；旧目标、旧任务及重复报告仅留普通日志，不改变revision。日志保留本次report_id、任务序号、来源/目标版本和SELECTED/IGNORED/NOOP结果，均无凭据。ADMITTED/OFFLINE资源失败复测不自动改实际上下架状态，quote_readiness按新证据阻断。

### 登记接入

AdmissionWork按supplier→resource→formal config→B来源/节点的顺序锁定；同一事务读取A原生模型/配置/控制头与所选真实报告，B proof在该事务读取当前指派及适用前置。不把采购人、审批资格、查询筛选人当当前办理人。

资源PENDING_ACCESS且自身测试、配置、来源及版本均满足后，由领域服务提出ADMITTED/ever_admitted=true/revision+1。模型核验仍单独决定报价，不额外阻止合法接入。资源更新、安全控制日志、完成说明、B逐行完成与outbox同事务；任何一步失败由外层整体回滚。A不自动完成整批节点、流程或设置supplier.established。

完成说明要求非空且合法UTF-8、无NUL；保留原办理节点引用与完成依据。控制快照使用已有supply.resource/v1协议的可选completion_note和test_evidence字段，不在日志复制正式URL、密钥或报告正文。

### Outbox 接入

AdmissionOutboxWriter接收A准备的安全事件并返回真实outbox主键；A在同事务核对event_outbox中新行的类型、payload、请求号、创建人和PENDING状态，空/错误回调不能当成功。事件ID及异步投递归公共适配，不在锁内网络发送。

事件类型RESOURCE_ADMITTED，payload为supply.resource_admitted/v1：resource_id、supplier_id、standard_model_id、region、字符串resource_revision、来源行/目标版本、formal config引用与版本、选定report引用/任务序号、办理flow/node/revision、actor、reason、request_id、completion_note及occurred_at。C消费安全引用并按需要重读A事实；A不改C客户关系或现有报价。

外层必须提交成功后才发布HTTP/幂等响应。重复ADMIT由状态/revision冲突拒绝，不通过重复生成事件“补成功”；真正幂等重放在公共事务入口处理。尚无新浏览器直建/直接接入/自报测试的HTTP入口。

<a id="resource-filters"></a>

## 供应来源筛选与页面请求

### 模型广场与供应来源

继续使用 GET `/api/internal/resources`，`view=grouped` 返回模型组，`view=flat` 返回资源行。原有单选参数仍可使用；新增多选与闭区间如下：

| 参数 | 规则 |
|---|---|
| `companies/model_types/supplier_ids/regions/states` | URL 编码的 JSON 字符串数组，每项最多 50 个、不重复；同一维度内 OR，不同维度之间 AND |
| `coefficient_min/coefficient_max` | 非负普通十进制字符串，最大 1；这是筛选边界，不放宽资源系数 `(0,1]` 写入规则 |
| `connection_min/connection_max` | 连通耗时 ms 区间，0 有效 |
| `rps_min/rps_max/tps_min/tps_max` | 精确十进制字符串区间，大于 2^53 的值不转浮点数 |
| flat `sort_by=admitted_at` | 真实上架时间排序，空值在升降序中均末尾，ID 打破同值 |

示例（编码前）：`view=grouped&supplier_ids=["21","22"]&regions=["CN"]&coefficient_max=0.7&page=1&size=20`。公司名中的逗号是名称本身，不拆分；数组参数不能与同维度单选同时使用。非法数组、未知枚举、负数、科学记数法、NaN、最小值超过最大值等返回 400。

公司、类型、供应商、区域和状态先在 SQL 收窄候选；指标以现有可信报告/目标版本投影为依据，只有 COMPLETED 的非空指标参加区间比较，过期或缺失不能当 0。服务端在同一读取事务中完成筛选、统计、排序及分页：grouped 的 total 是筛选后的模型数，去重供应商数、资源数、系数范围和可报价数都来自完整匹配集合；flat 的 total 为匹配资源数。分页前不会截断资源成员。

本轮复用现有后端读取及聚合机制。指标判断和聚合仍需在服务端装配匹配资源的可信事实，没有新增物化聚合、缓存或索引；本轮完成计算与网络分页从浏览器移到服务端，不等同于完成大数据量数据库性能压测。

前端广场每次只请求一个 grouped 结果页；来源详情每次请求一个 flat 结果页，不再 allRows 拉取全部分页。筛选选项复用 `/api/internal/resources/filter-options` 远程搜索，供应商详情往返保留筛选、排序和来源页码。
