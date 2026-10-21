# F1 读取与 HTTP 接口切片实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为已实现的 F1 领域服务补全内部读取、历史快照以及独立可注册的 HTTP 接口与契约测试。

**Architecture:** 新增 ModelReadStore/ModelReader 端口；A 自己的 `supply_model*.go` 和 `router_supply.go` 负责 DTO、参数、认证上下文和领域错误映射。复用现有认证、幂等和响应机制；不改公共 router.go、main.go、迁移或统一响应结构。

**Tech Stack:** Go1.26、Gin、httptest、现有公共中间件；Docker Go 镜像运行测试。

**Spec:** [F1 契约](../supply/supply-standard-models.draft.md)、[决策](../supply/decisions.md)、[公共接入需求](../supply/public-integration-request.md)。

## Global Constraints

- bigint ID 和 revision/version 字符串；全员读取针对已认证 INTERNAL/STAFF，不能开放给匿名或供应商身份。
- StaffID 来自现有 `middleware.OperatorFrom`，不得用 AccountID 或 HTTP actor_id 代替。
- 复用 `{code,message,data,requestId}`，失败 data=null；公共 errors[] 尚未交付，不私建另一套错误信封。
- 新增自动绑定创建者；PUT整体替换资料，缺结构字段、未知字段、数字型金额/版本或尾随 JSON 拒绝。
- 写接口必须经过现有 Idempotency；依赖缺失返回503，绝不提供无幂等写入后门。
- 真实 schema/SQL/认证装配未完成前，不声称真库和正式 HTTP 已上线；旧 API 和页面不切换。

## Task 1：读取端口与历史版本

Files: `internal/domain/supply/model_read.go`、`model_read_test.go`。

- [x] 先写测试：列表默认1/20、大小上限100、排序白名单、空值末尾和稳定ID的查询要求；大ID保持字符串；非当前历史价格不可用于新的报价；返回副本不改仓储对象；不存在和无依赖关闭。
- [x] 运行 `go test -count=1 ./internal/domain/supply` 观察缺失实现失败。
- [x] 实现 `ModelReadStore.ListModels/GetModel/ListModelVersions/GetModelVersion`、`ModelReader.List/Get/Versions/Snapshot`。查询不含人员过滤字段。
- [x] 重跑绿色；明确排序实际 SQL 与真库分页仍需仓储接入验证。

历史消费锚点：当前版本3、历史版本2价格分别为3和2，`Snapshot(id,2)` 返回价格2、CurrentVersionNo=3、IsCurrent=false 且新报价 readiness 为 false，不能把当前版本价格覆盖历史。

## Task 2：HTTP 与路由

Files: `internal/api/supply_model.go`、`supply_model_request.go`、`router_supply.go`、`supply_model_test.go`。

- [x] 先写真实 Gin/httptest：内部无旧权限点可读，匿名401、其他门户403；写请求拒绝自报维护人、未知字段、重复JSON键、尾随JSON、错误版本类型及缺失PUT字段；成功DTO扁平化并保持ID/金额字符串。
- [x] 写错误映射：领域403/404/409、资料400、缺依赖503、内部错误不回显；历史版本不可编辑。
- [x] 写路由集成：缺幂等键400，同键同body重放不重复调用写服务，同键异body沿用公共400；无存储503。
- [x] 观察红色，实施注册入口 `RegisterSupplyRoutes(group, SupplyDeps)`。组前缀由调用方提供 `/api/internal`，注册器附加内部身份保护，不加旧角色权限点。
- [x] 用严格 JSON 解码及字段存在性校验；重复键不能被 JSON 默认后值覆盖。响应复用现有 response，不在公共源码分配新数字错误码。
- [x] 重跑目标HTTP/领域测试，随后完整 internal/api 包、vet及后端build；更新准确契约和接入说明。

## 验收边界

测试替换缺失的读取仓储、写服务边界和幂等持久化，仅证明参数、DTO、真实路由及公共幂等状态机的接入。PostgreSQL约束、公共事务commit行为和新版页面仍不在本切片完成声明内。
