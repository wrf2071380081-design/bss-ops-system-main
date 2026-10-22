# A / F3 逐资源登记接入契约

日期：2026-10-21。依据：v0.22 vCompleteAdmission、详细设计§6.2/6.3、DEC-002/003及最新需求REQ-11/17。本文细化已有Admit草案，不新增审批中心或公共迁移。

## 操作与可信事实

调用命令包含resource_id、expected_revision、外部实际配置确认和完成依据；WriteMeta取认证人员、原因、请求追踪。审批资格不自动授权接入，普通买方归属也不能替代当前配置节点办理人。

办理中的资源由B在同一事务提供AdmissionProof：flow_id、node_id、flow_revision、supplier_id、resource_id、origin_row_id/version、target_version、region、assignee_id、流程进行中、节点进行中、准备与其他适用前置已完成。该结构来自服务端，不接受客户端自报布尔值作为授权证据。报价审批/分派尚未完成时，B不得提供进行中的已分派节点。

## A 注册器 HTTP 契约

`POST /api/internal/resources/:id/admit` 只在A的 `RegisterSupplyRoutes` 中注册，要求内部Staff身份及 `Idempotency-Key`。请求严格为 `{expected_revision:string, external_confirmed:true, completion_note:string, reason:string}`；资源ID和revision均为规范正十进制bigint字符串。认证人员和request_id由服务端上下文取得。不得接受flow/node/assignee、测试通过、审批资格或supplier_id等客户端自报事实。

成功返回不含配置/凭据的收据 `{resource_id,state:"ADMITTED",ever_admitted:true,revision:string,updated_at}`。仅在原资源、来源行和B节点办理事实的共享事务成功提交后返回。缺服务503，身份/文本错误400，非当前节点办理人403，状态、版本或业务前提冲突409。主路由尚未调用A注册器，不代表运行API已开放。

本切片只实现有办理流程的初次接入；无流程人工建资源的授权策略尚未确认，不开放后门。PENDING_TEST须先经测试事实同步到PENDING_ACCESS；该同步由后续AttachTestEvidence衔接，Admit不暗中改写报告或跳过准备状态。

## 业务前提

- 资源PENDING_ACCESS且ever_admitted=false，身份和expected_revision一致，revision可递增。
- 当前认证人员是配置节点办理人，流程属于该供应商、区域与资源，来源申报行及版本、测试目标版本一致；流程/节点进行中且适用前置满足。
- 正式配置为同供应商、同区域、有效且当前已确认版本，包含正式凭据；不能把测试凭据复制成正式配置。
- 选定报告为该资源或其自身来源申报行/版本，当前目标匹配、证据有效、连通和压力均通过。不使用批次通过代替逐资源结果，不增加缓存阈值。
- 模型资料核验是可报价条件，不在此新增“先核验才能登记接入”的额外门槛；接入后仍由已有安全快照独立计算quote_readiness，未核验模型不可报价。

## 事务协议

ResourceAdmissionTransaction按共同约定锁定资源及对应办理事实，读取当前模型/供应商/正式配置/选定报告。A验证后CAS更新资源为ADMITTED、ever_admitted=true并递增revision；不更换资源身份、来源、目标版本、报告或采购人。

事务效果端口同事务保存B的逐行接入事实、该次授权办理引用、操作日志、完成依据及资源接入事件；B按其完整节点规则判断是否所有行完成，A不因一行成功就完成整批流程或设supplier.established=true。事件ID和投递由公共outbox适配，不直接修改C的客户关系或报价。

业务、B状态、日志或事件失败须整笔回滚。UnitOfWork加入外层事务时返回仅代表候选结果，外层必须提交后响应；公共幂等重放发生在业务命令前，不能靠重复ADMIT返回假成功。缺证据适配返回依赖不可用。

## 交付边界与验收

已新增[原生事务适配](./resource-admission-storage.draft.md)，接既有领域服务，在同一事务使用真实A模型/配置/资源/报告SQL；保留当前B办理proof、完成依据并核对实际outbox行。真库覆盖完整A组合链路、同批失败资源、错误办理人/版本/前置、日志/B行/事件/外层回滚，以及接入与失败报告两种并发顺序。Stage 34 已在A注册器提供接入HTTP handler及严格契约测试。

B真实审批/节点资格/版本和公共outbox、真实SupplyDeps/main主装配及资源详情操作表单仍需联合验收；当前B提供方为受控事务夹具，不宣称真实生产办理或浏览器页面已完成。不要求为领域字段逐个新增同名列。
