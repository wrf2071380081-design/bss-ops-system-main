# A / F1 标准模型接口评审草案

状态：评审草案，尚未冻结或接入运行服务。
日期：2026-10-20。
依据：`01-A主数据与供应侧-详细设计-v1.0.md` §3、§4、§8.1、§10、§15、§16。

本文属于新 supply 业务，不修改或重新解释旧 `04-models.md` 的 SKU 接口。新业务仅使用 `standard_model`、`standard_model_version` 及明确允许复用的公共能力。

## 1. 范围

本切片交付手工维护八类资料、价格项、完整性检查、人工核验、不可变版本、列表与详情契约。F2 自动拉取和差异接口在阶段 3 实现，不能用其未交付阻断手工模型维护，也不能把现有旧价格同步视为 F2 完成。

需求中的既定规则：金额用 decimal 字符串；资料不全不能核验和报价；写操作幂等及同事务留痕；新表与旧业务表隔离。下列字段和状态沿用详细设计建议，需与公共目标 DDL、绑定和 C 消费契约核对后冻结。

## 2. 传输与通用约束

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

共享说明中的 `errors[]` 与现有公共实现的字段错误结构尚不一致。A 提供字段路径和安全说明需求，由公共负责人固定编码和序列化格式；不在本草案自行分配数字业务码。

## 3. 官方资料与安全 DTO

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

## 4. 价格项

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

## 5. 接口行为

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

## 6. 写入、版本与可报价规则

1. 用户已确认允许保存不完整资料，返回缺失路径；核验和报价仍被阻断（见[业务决策记录](./decisions.md) DEC-004）。已提供但非法的字段仍拒绝。
2. 更新锁定当前对象，校验身份和 revision；无实际资料变化返回 changed=false，不增加业务版本。
3. 实际资料变化产生完整不可变快照，再更新当前投影和 revision。历史报价引用的模型版本不随当前资料变化。
4. 人工核验校验八类资料和来源，记录核验人及日期；价格/关键身份/来源变化后重新核验。
5. quote_readiness 至少返回 ready、reasons、standard_model_version_no；ACTIVE、VERIFIED 且所需场景价格资料满足时才可用。
6. C 在实际报价写入时重新读取并校验当前版本；页面缓存的 ready 只用于展示。
7. 业务变更、模型快照、operation_log 同事务提交。日志失败整体回滚；HTTP、抓取和 LLM 调用不进入事务。
8. 所有内部读取遵循本轮内部全员可见口径，写入按绑定维护人。用户已确认新建自动绑定认证上下文中的当前创建者；新增请求不接受自报 maintainer_id，常规 PUT 不承担转交。后续更换维护人走公共转交能力（DEC-005）。

## 7. 验收锚点

| 需求 | 必须证明的行为 |
|---|---|
| A01 | 不完整模型可定位缺失项；核验失败且不可报价 |
| A02 | 补齐并人工核验，当前状态、不可变快照和日志一致 |
| A03 | 修改价格产生新版本，旧价格保持，关键资料重新核验 |
| A04 | 多模态、零价格、缺失价格的币种/单位/条件和语义不丢失 |
| A05 | 同 revision 的并发写仅一个成功，另一个 409，无历史覆盖 |
| A08 | 非绑定人写入 403，业务状态和成功日志不改变 |
| A10 | DTO、日志、错误不出现调用密钥和其他敏感原文 |
| A21 | 日志失败时业务写和快照整体回滚 |

测试分层：领域反例 → 契约 → PostgreSQL 版本/并发/事务 → 原型页面及真实 HTTP 流程。受控仓储/公共 adapter 的通过不能代替真库验收。

## 8. 冻结前需补齐的接口事实

- 公共负责人交付 standard_model/version 的真实 DDL，包括 revision、维护人、唯一性和版本约束。
- 新建维护人策略已按 DEC-005 固定；公共方继续提供认证人员身份和后续转交接口，节点授权单独核对。
- 与 C 核对模型/价格快照、价格任务和单位字典，以及完整性对具体报价场景的要求。
- 固定来源白名单及来源过期判定；当前不自行设定抓取频率或来源有效天数。
- 公共方固定错误结构、幂等冲突码、事务日志调用及认证 staff ID 类型。
- 用户完成 F1 设计评审后，补齐嵌套 schema、请求/响应样例和排序字段白名单，再进入实现。

## 9. 独立领域切片的实现映射

用户已授权依据原型复核合理后按切片契约先行＋TDD推进；复核和范围记录见 [F1 实施计划](../TDD/2026-10-20-f1-domain-plan.md)。此授权推进独立领域代码，不等于公共 HTTP 契约和目标 DDL 已冻结。

当前聚合逻辑结构：`ModelData` 包含 company/name/official_id/model_type、输入输出模态、capacity、abilities、price_items、api_access、lifecycle、sources、checked_at。

- capacity/api_access/lifecycle 采用 `{key,disclosure,value}` 的结构化事实；disclosure 为 DISCLOSED/UNDISCLOSED/UNVERIFIED，已披露必须有值，未核验阻断完整性。
- abilities 采用 `{key,state,description}`；state 为 SUPPORTED/UNSUPPORTED/UNDISCLOSED/UNVERIFIED。
- 支持本原型的 TEXT/IMAGE/VIDEO/AUDIO 模型及对应任务价格覆盖；价格任务、单位及维度词表在 `price.go` 明确列举，扩展须有契约和测试，不接收任意 JSON 作为计费规则。
- 金额规范为八位小数字符串，以相同数值不同尾零格式重提不会制造价格变更版本；不同稳定键/条件保留为独立价格项。
- 来源策略同时检查模型级与价格项级来源；构造/保存不代表来源已核验。

准确公共适配需求见[接入请求](./public-integration-request.md)。HTTP 层仍须固定嵌套 schema、分页排序和错误包络，并验证未知输入拒绝、幂等和真库事务；独立领域测试不代替这些验收。
