# F1 公共接入需求

日期：2026-10-20。
范围：开发者 A 的独立标准模型领域切片；公共负责人负责目标迁移、认证、事务/日志适配及应用装配。本文不是迁移脚本，也不代表依赖已经交付。

## 1. 当前可接入的 A 代码

目录：`model-pricing-backend/internal/domain/supply/`。

- `ModelService.Create(ctx, WriteMeta, ModelData)`。
- `ModelService.Update(ctx, WriteMeta, id, expectedRevision, ModelData)`。
- `ModelService.Verify(ctx, WriteMeta, id, expectedRevision, checkedAt)`。
- `StandardModel.MissingFields()`、`Readiness()` 和独立副本 `Clone()`。
- `ModelUnitOfWork`、`ModelTransaction`、`OfficialSourcePolicy` 的准确 Go 签名见 `ports.go`。
- `ModelReadStore`、`ModelReader.List/Get/Versions/Snapshot` 见 `model_read.go`；读取查询不携带旧角色/维护人过滤，历史快照不用于新报价。
- HTTP 接入入口为 A 的 `internal/api/router_supply.go` 中 `RegisterSupplyRoutes(group, SupplyDeps)`；提供 Reader/Writer/IdemStore，组前缀 `/api/internal` 并已有 AuthN/RequestID。注册器会额外检查 INTERNAL/STAFF、使用 StaffID，读取不附加旧角色权限点。

领域不引用旧 model_sku、supplier_profile、审批或成本重算。A 已提供独立 HTTP 注册入口和 Swagger 源注释，但未修改公共 router.go/main.go，也没有创建业务迁移、SQL 仓储或公共能力的替代实现。

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
- 当前公共中间件在 handler 写响应后执行 MarkResult/Commit；真实失败时的响应状态/提交状态一致性须由公共负责人验证并处理，必要时先缓冲响应再在提交后发出。A 的无库幂等测试不能证明 SQL commit 失败时仍正确响应。
- 读取适配器需真实实现白名单排序、双向 NULLS LAST 和稳定 id 次序，以及排序/过滤先于分页；端口查询标记本身不能作为 SQL 验收证据。

## 6. 接入验收

1. 目标迁移可用、字段映射核对后，在独立测试库验证身份唯一性、版本唯一性和 CAS 冲突。
2. 使用真实日志/事务适配注入日志失败和 commit 失败，确认当前投影及版本不部分提交。
3. 验证内部全员读取、绑定人写入和非绑定403，HTTP 层不得沿用旧角色矩阵阻断新读取。
4. 对真实路由执行完整的新增待补齐→补齐→人工核验→改价→历史快照流程。
5. 原型前端在上述冻结 API 之上接入；公共路由和布局归公共负责人，A 页面归 A。

F4/F3 及后续阶段所需 schema/接口在对应切片继续补充，不因 F1 独立代码可测试而视作全部 A 功能完成。
