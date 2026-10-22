# A / F3 初次接入的正式配置绑定

日期：2026-10-21。依据：BindFormalConfig既有草案、正式配置与测试凭据隔离、首次接入的配置节点办理授权。

## 范围与资格

本切片只覆盖尚未接入的PENDING_TEST/PENDING_ACCESS资源绑定已存在的正式配置。资源身份、测试目标及报告不因绑定而修改；不创建正式密钥，不从申报测试URL/Key生成正式配置，不把绑定成功当作外部操作或接入完成。

WriteMeta来自认证人员。命令含resource_id/expected_revision、formal_config_id/expected_config_revision；服务端共享事务提供当前资源、正式配置安全可用性事实及B的AdmissionProof。操作人员须为当前配置节点办理人，流程/节点进行中且准备及适用前置满足；资源、供应商、区域、来源行和目标版本与事实一致。审批资格、buyer身份不替代配置节点指派。

## A 注册器 HTTP 契约

`PUT /api/internal/resources/:id/formal-config` 只在A的 `RegisterSupplyRoutes` 中注册，要求内部Staff身份及 `Idempotency-Key`。请求严格为 `{expected_revision:string, formal_config_id:string, expected_config_revision:string, reason:string}`；ID及revision为规范正十进制bigint字符串。认证办理人和request_id从服务端上下文取得。客户端不能传入节点/流程、办理人、配置状态、密钥/URL或任何“已确认”布尔值。

成功返回无敏感内容的收据 `{resource_id,formal_config_id,formal_config_revision:string,resource_revision:string,changed:boolean,updated_at}`。同ID同版本的重复关联返回 `changed:false` 并保留资源revision；新的确认配置版本返回 `changed:true`。错误边界与逐资源接入一致：400请求错误、403当前节点办理权限不足、404资源/配置不存在、409版本或流程事实冲突、503依赖未装配。主路由尚未调用A注册器，不代表运行API已开放。

正式配置维护仍按其既有供应商采购人规则；绑定是资源关联操作，两者不混为一个授权入口。已接入/下架资源的配置变更继续走B已批准的变更流程，本命令不绕过该流程。

## 规则与事务

- 配置ID、供应商和区域必须匹配，配置ACTIVE、有正式密钥、revision为命令期望值且confirmed_revision=revision。
- 资源revision必须匹配；绑定新ID或新确认版本时revision+1，状态、ever_admitted、来源、测试目标、报告及任务序号不变。
- 同ID/同版本重复关联记录BIND_NOOP，保持资源revision/updated_at；新请求意图仍留痕，公共幂等重放由外层处理。
- 资源CAS、配置引用和操作日志在同事务；配置更新/停用、资源绑定/接入遵循公共统一锁顺序，绑定后的引用须使既有HasResourceReferences生效。
- 日志只保存资源及配置ID/版本安全引用、流程/节点证据，不含正式BaseURL、掩码以外凭据或测试数据。外层提交前返回是候选结果，提交失败不可返回成功。

已新增A原生SQL事务适配，见[正式配置存储与绑定](./formal-config-storage.draft.md)。配置与资源事实来自真实控制头，办理资格仍由可信B proofLoader提供；锁序为supplier→resource→config→B，绑定与停用的两个真实并发提交顺序均已验证。Stage 34 已在A注册器实现HTTP入口并通过契约测试；B真实提供方、公共生产装配和页面操作表单仍待联合验收。
