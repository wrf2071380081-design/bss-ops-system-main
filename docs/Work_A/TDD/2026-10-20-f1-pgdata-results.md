# F1 PostgreSQL 资料读取适配记录

日期：2026-10-20。依据：000033 现有资料字段、A 的 ModelData 契约及 F1 增量结构交接清单。

## 范围

新增 `internal/infra/supplypg/ReadModelData(ctx, tx, id)`，在调用方提供的 `*sql.Tx` 中参数化查询 `public.standard_model` 的官方资料列，返回 `supply.ModelData`。本函数不创建事务、不提交、不写表，不返回虚构的维护人、核验状态或 revision，也没有接入公开 HTTP。

通过 SQL `to_json(text[])` 读取模态数组，再显式解码一维字符串数组，避免手工拼接 PostgreSQL 数组转义以及依赖特定驱动的数组 Scan 行为。NULL 元素和非一维结构拒绝；空数组保持非 nil。JSONB 使用已有严格编解码规则，价格不经过浮点数。无模型返回 ErrNotFound；取消上下文和查询错误向上传递，不能解释为“无资料”。

它是完整 F1 仓储内部可复用的资料读取部分，仍不能独立实现 ModelReadStore/ModelUnitOfWork 或核验、不可变版本、CAS、事务日志闭环。

## TDD 与真实库验证

先编写事务/ID校验测试及 PostgreSQL 集成测试，读取函数仅返回依赖缺失时观察红阶段：非法 ID 校验失败，集成测试的默认资料、完整资料和不存在记录读取失败。随后实现查询与解码，最终通过。

复用已确认的独立库 `model_bss_a_schema34_20261020`。集成测试要求显式环境变量和专用数据库前缀，并查询 current_database() 核对。测试在单一事务插入、更新资料夹具，结束后显式回滚并核对夹具记录数为0；不修改表结构。序列消耗不因回滚恢复。

| 场景 | 结果 |
|---|---|
| 000033 默认空数组、空对象、NULL 日期 | 空资料可读，保持待补齐，不编造日期/价格 |
| text[] 中引号、逗号、反斜杠、中文 | 精确保留；特殊值用于验证存储转义，不表示业务允许任意模态名称 |
| JSONB typed facts、能力未知、来源 | 结构及字符串精确保留 |
| 零价格及 123456789.12345678 | 字符串原样保留，无 float 转换 |
| 大整数字符串 9007199254740993 | 原样保留 |
| checked_at 日期 2026-10-20 | 正确读取为日期字符串，不推断人工核验已完成 |
| 非空旧 JSON 对象 | 明确返回 capacity 不兼容 |
| 数组中 NULL 元素 | 明确返回 input_modalities 不兼容，不转换为空字符串 |
| 不存在 ID、取消上下文 | 分别返回 ErrNotFound、context.Canceled |
| 测试结束 | 夹具回滚，运行库 model_bss 仍 migration32、dirty=false |

## 原始证据

- [单测红阶段](./evidence/2026-10-20-f1-pgdata-red.txt)
- [真实库红阶段](./evidence/2026-10-20-f1-pgdata-integration-red.txt)
- [真实库最终结果](./evidence/2026-10-20-f1-pgdata-integration.txt)：四个子场景通过
- [领域/API/适配包回归](./evidence/2026-10-20-f1-pgdata-tests.txt)：三个包通过；默认回归跳过未显式配置的真实库测试，真实执行证据是上一项
- [go vet](./evidence/2026-10-20-f1-pgdata-vet.txt)：通过
- [go build ./...](./evidence/2026-10-20-f1-pgdata-build.txt)：通过
- [运行库复查](./evidence/2026-10-20-f1-pgdata-runtime.txt)：32、dirty=false

## 剩余接入工作

公共字段/约束及迁移仍由公共负责人统一处理，见 [F1 交接需求](../supply/2026-10-20-f1-schema-handoff.md)。A 后续在完整聚合读取和事务仓储中复用本函数，实现人员归属、资料核验、历史快照和 revision 校验，完成真库主流程后再装配页面/API。此次没有部署、提交或推送 Git。
