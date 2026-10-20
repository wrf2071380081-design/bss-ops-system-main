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
