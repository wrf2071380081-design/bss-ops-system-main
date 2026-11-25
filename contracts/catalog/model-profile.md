# 官方模型资料到 BSS 的字段契约

适用：BSS 手动/定时、DSH 对话/定时，统一调用 `toBssModelData`；原始采集快照不改写。

## 规格、能力与来源

`capacity/api_access/lifecycle` 保留原有 `key/disclosure/value`，可选增加：
`label/unit/semantics/condition/scope/source_url/quote`。能力保留 `key/state/description`，可选增加
`label/condition/scope/source_url/quote`。BSS JSONB、Go DTO、前端保存与审核都保留这些字段。

- `semantics`：`SHARED_WINDOW / SEPARATE_INPUT_OUTPUT / INPUT_ONLY / UNSPECIFIED`。没有证据不推算最大输入。
- `scope`：`API / WEIGHTS / NATIVE / EXTENDED`。仅在来源明确区分时填写，不根据名字猜测。
- 多条件规格分别保存，条件范围形成稳定标识。同范围相互矛盾的数值均保留为待核实。
- 名称优先使用采集名称，版本单独保存。缺模态、类型不自动填成文本。
- 能力未知不等于支持；仅转换明确已知的方法。别名必须有原厂引用证据。
- `source_url` 必须是不带凭据的公开 HTTP(S) 地址；证据刷新不计入规格/价格变化。

## 条件价格

沿用 `price_items`；`dimensions` 增加 `region/deployment/valid_from/valid_to/period/account_tier`，
保留已有上下文阶梯、缓存、分辨率等维度。地区、阶梯、单位、币种、条件不同的行不得合并。
相同条件金额变化应保持标识；只有完全重复的行可以去重。缺币种不再默认人民币；原始行继续
留在待审采集快照，补齐依据后才能写入价格。相同范围不同金额不可同时作为正式报价采纳。

## 型号采集状态

BSS `GET /api/internal/agent-catalog/model-status?ids=1,2`：最多 100 个 BSS 型号 ID。
按档案中的厂商和官方 ID 关联历史；只读取平台共享及当前员工自己的记录。

返回每个型号的 `state / attemptedAt / lastCompleteAt`。
`state` 为 `COMPLETE / PARTIAL / UNAVAILABLE / NOT_OBSERVED / RUNNING / UNKNOWN`。
`COMPLETE` 表示目标字段已有数值或官方明确结论，不表示所有字段非空；冲突或待续采不能算完整。
本次部分成功保留最近完整成功时间。缺少历史信息时显示待核实，不用厂商成功状态替代。

存储仍为 BSS 业务库；Agent 保存执行/会话。BSS migration 82 仅增加历史 JSONB 查询索引，
不重写历史结果。Agent 无数据库结构变更，不新增空迁移。
