# A / F3 资源与模型广场对接草案

状态：评审草案；不是已注册的运行接口。
日期：2026-10-20。
依据：A 详细设计 §6、§8.3、§9，以及[业务决策记录](./decisions.md) DEC-002/003。

本文先向 B/C 明确 A 的边界、资源事实及事务要求。目标 DDL和公共适配签名未交付，不能将草案视为已经冻结的数据库模型。

## 1. 已确认的业务规则

1. 草稿只保留申报行和测试记录；提交进入办理后才创建资源。
2. 每个资源自己的连通性和压力测试都通过，才可登记接入并供报价使用。批次至少一条通过的提交门槛不能替代逐资源准入。
3. 资源具有独立的 supplier_id、standard_model_id、region；身份三元组创建后不通过常规编辑替换。
4. coefficient 是 decimal 字符串，范围 `(0,1]`，最多四位小数；零、大于一及超精度输入拒绝。
5. 正式配置与申报测试凭据分开；不能将申报测试 URL/Key 自动复制成正式配置。
6. 正式接入还需满足有效正式配置、适用办理条件与绑定授权；测试通过不等于直接可用。

第 3—6 项来自任务及详细设计；人工直建资源、人工直改系数、复测失败处理和证据有效期仍需按 D02 后续对齐，不能据本草案自动开放额外写入口。

## 2. A 提供给 B/C 的方法

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

## 3. A 读取的 B 事实

B 的 AdmissionEvidence 至少需能表达：

- 所属 supplier、standard_model、region，以及关联的 submission_row 和不可变目标版本。
- 对应资源引用（资源创建前可不存在）、报告引用及任务顺序。
- 连通性与压力各自的结论，目标是否与当前待接入对象一致，以及证据有效性和阻断原因。
- 测试配置/目标指纹的安全标识，不返回测试原始密钥。

B 的 OnboardingReadiness 需表达资源是否属于该 flow/node、当前 actor 是否办理人以及适用节点是否完成。A 从服务端读取这些事实，不信任 HTTP body 自报 `flow_completed=true` 或 `test_passed=true`。

未接入事实提供方时，正式写流程返回依赖不可用；不能用默认“通过”的适配器启用资源。受控 fake 仅用于独立测试。

## 4. 给 C 的安全快照

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

## 5. HTTP 读取与模型广场

| 接口 | 契约 |
|---|---|
| GET `/api/internal/resources` | view=grouped（默认）或 flat；两种响应行 DTO 明确分开 |
| GET `/api/internal/resources/:id` | 安全详情、配置摘要、报告摘要、quote_readiness、revision、服务端操作资格 |
| GET `/api/internal/resources/:id/test-reports` | 通过 B 的报告读取接口提供历史摘要及受保护访问方式，不复制报告正文作为第二来源 |

筛选：company、model_type、supplier_id、region、state、keyword；分页默认 1/20，上限100。排序字段采用白名单；未知字段 400，空值始终末尾，并以稳定 id 次序打破同值。

grouped 按标准模型分组，同一过滤范围计算供应商去重数、资源数、可用数、系数最小/最大及状态分布。先对模型分组再分页，不能先截断资源页再聚合。区域筛选不能把另一地区的资源计入数量或区间。

flat 表示每条资源。其排序、分页及 total 不套用 grouped 的模型计数。精确排序字段名单和完整 HTTP 样例与前端列表一起评审后冻结。

## 6. 状态与事务接入

详细设计建议的实际状态为 PENDING_TEST → PENDING_ACCESS → ADMITTED → OFFLINE；计划标签是派生信息，不让未来计划立即改变当前可用性。状态枚举须与目标 DDL核对。

进入办理创建时，B 的行 resource_id 写回与 A 的资源、防重及日志共享事务。接入时，B 的办理/行状态与 A 的资源状态也共享事务；任何验证、日志或 outbox 写入失败全部回滚。

当前基础代码已有 `internal/infra/db.WithDB/FromContext`。它是可复用机制，不代表 UnitOfWork、operation_log 和 B/C 适配器已实现。领域代码不直接依赖 B/C 具体服务类型或在内部另开独立事务。

下架通过 outbox 通知 C，A 不直接修改 customer_link；C 按 event_id 幂等处理。是否及如何恢复客户关系仍由 C 的业务契约决定。

## 7. 交付门槛

- A06/A07：区域、身份唯一与来源防重，重试不重复创建。
- A08：非绑定写入 403，读取不错误套用旧角色数据域。
- A11：三行一行通过，只放行该资源接入；其他行保持阻断。
- A12：B 写状态后 A 失败时，同一事务全部回滚。
- A13：目标改变或报告迟到，旧测试不被误用于新对象。
- A14：分组、筛选、计数和排序在真库保持一致与稳定。
- A17/A21：下架、留痕、事件入队原子一致，写失败不产生成功副作用。
- 完整 A24 需接入真实 B/C 后联合验收；接口草案和 fake 测试均不能作为该验收完成证据。

## 8. v0.22 对齐补充

最新依据为原型 v0.22 及原路径 A 详细设计的 v1.1。前文涉及 `supplier_model_name` 的输入应改为明确的接口模型 ID `supplier_model_id`；不能由官方模型名自动猜测供应商接口标识。旧目标 DDL/申报行到资源的映射需与 B/公共方核对后冻结。

资源安全详情及 B 的报告摘要补充 `cache_stats`：report_id、status(PROVIDED/NOT_PROVIDED/NOT_APPLICABLE)、hit_tokens、input_tokens、hit_rate、scene。计数为非负整数字符串，PROVIDED 要求 input_tokens>0 且 hit_tokens<=input_tokens；比例派生，页面直接按原始计数计算百分数后一次舍入到两位。0/2000 是0.00%，不是缺失值。

列表只使用当前选定报告的统计；历史详情按所查 report_id 返回，不从其他报告借值。B 负责原始报告写入和校验，A 不新增可独立编辑的资源缓存统计。该指标不增加接入或报价的数值门槛。

模型广场和供应来源导航集中在“详情”列，真实状态操作独立保留；按过滤结果聚合并按模型分页。工作台的人员查看不会改变资源操作身份。

## 9. 读取切片的具体端口

GET resources 新增 `standard_model_id` 过滤用于展开单个模型的资源来源；展开时保留原区域/供应商等过滤条件。grouped默认按name升序，flat默认按coefficient升序；双向空值末尾，稳定ID打破同值。精确排序白名单见ResourceListQuery校验。

每条读取投影由resource、当前standard_model、供应商安全事实、正式配置安全事实、选定报告摘要构成。目标版本、来源申报行及版本必须与报告对应；旧目标报告不作为当前测试或缓存依据。B适配失败返回依赖错误，只有确知未测试才允许空报告。

缓存 hit_rate 保留6位比例，另返回直接由原始计数一次舍入的 hit_percent（两位百分数）。例如246899/2000000的比例摘要0.123450，百分数应为12.34而非对已舍入比例再乘100得到12.35。

grouped响应为模型id/name/official_id/company/model_type、supplier_count/resource_count/available_count、coefficient_min/max、regions及state_counts。flat响应为资源安全详情，不含正式base_url、密钥或网站账户。grouped的total表示模型数，flat的total表示资源数。资源写命令、计划及B报告历史读取后续切片接入，不因读代码完成而视作F3整体完成。

测试摘要增加 `test.metrics.connection_ms/rps/tps`，为可空十进制字符串；真实0保留原字符串，缺失为null。flat排序白名单包括id/supplier_name/region/coefficient/state/updated_at/connection_ms/rps/tps/cache_hit_rate；grouped包括id/name/company/model_type/supplier_count/resource_count/available_count/coefficient_min/coefficient_max。API统一包络data含view/list/total/page/size。

读取代码与HTTP注册器已新增，尚未装配真实仓储/主入口；实际交付及未完成项见[F3读取记录](../TDD/2026-10-20-f3-read-results.md)。

## 10. 广场远程筛选选项

已有独立SQL目录适配supplypg.FilterOptionsStore，按新表完整资源/模型/供应商关系筛选，在同一只读快照查询公司和供应商，使用EXISTS去重、Limit+1判断has_more。真库已覆盖区域、重复来源、55项截断和搜索，见[SQL验证记录](../TDD/2026-10-20-filter-options-pg-results.md)。尚需组合进完整ResourceReadStore，不能将该目录类单独冒充完整资源仓储。

GET `/api/internal/resources/filter-options?region=CN&keyword=名称片段`，仅内部人员读取。data为 `{companies: string[], suppliers: [{id,name,region}], has_more: boolean}`；ID字符串，不含配置或凭据。region可空，keyword按公司/供应商各自名称匹配。

每种选项最多50项，截断时has_more=true；应在完整新资源/模型/供应商关联上按区域和名称过滤后取有限匹配，不从资源当前页猜测全集。仓储实现独立的可选能力ResourceFilterOptionsStore；未提供时503，不回退旧supplier_profile。

前端按名称查找并提交已选择供应商ID，展开来源时固定继承已应用筛选并重置资源页码/排序。独立ModelPlaza及来源/详情组件已新增；真实装配和状态写流程仍待完成，见[F3前端记录](../TDD/2026-10-20-f3-frontend-results.md)。

## 11. 统一改系数命令独立实现

ResourceCoefficientService.Change接收认证WriteMeta、资源ID/revision、系数和CoefficientSource{Kind:SUBMISSION/PLAN,ID,Revision}。ResourceCoefficientTransaction在同事务读取并核对真实来源授权、目标资源/revision/金额；来源引用不是客户端授权凭证。PLAN按原发起人验证，不错误要求其仍是当前buyer；人工MANUAL入口尚未开放。

实际变化CAS升资源revision，保留测试目标版本与报告，写前后快照日志和系数变更outbox；无变化只追加NOOP日志，保留revision/updated_at。调用方来源完成、资源、日志和outbox原子提交，提交失败返回nil。事件ID由公共outbox分配，C不因该命令被A直接改写原报价。具体端口及当前证据边界见[阶段记录](../TDD/2026-10-20-resource-coefficient-results.md)。

## 12. 直接上下架领域命令

> DEC-010已确认严格计划流程：以下直接HTTP/页面入口现已撤下，领域代码仅供后续计划执行重构复用。当前实施以[纠正记录](../TDD/2026-10-20-plan-only-correction.md)为准。

ResourceStateService.Change仅供绑定采购人直接OFFLINE/ONLINE，参数包含external_confirmed和completion_note；未实际确认或无依据拒绝。计划来源不能用客户端plan_id绕过buyer校验，需后续独立的发起人验证执行服务。

ONLINE复用当前安全快照可报价判定，在候选ADMITTED副本重查模型、主体、配置及自身测试；OFFLINE不因上游资料失效而阻止采购人停止资源。两者保留ever_admitted、目标与报告，CAS升revision。操作记录、公告和outbox由同事务效果端口写入，公告影响PENDING。具体证据及未接入项见[上下架领域记录](../TDD/2026-10-20-resource-state-results.md)。

历史POST `/api/internal/resources/:id/state` 及直接登记页面已按DEC-010从A注册器撤下；[HTTP记录](../TDD/2026-10-20-resource-state-http-results.md)仅保留历史证据。当前采用创建计划→原发起人登记实际完成；已有计划创建/修改/取消独立能力，执行HTTP仍未开放，最新进度以[TDD索引](../TDD/README.md)为准。
