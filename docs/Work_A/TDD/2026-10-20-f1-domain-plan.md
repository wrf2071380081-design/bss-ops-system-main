# F1 标准模型独立领域切片实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 交付 F1 价格项、八类资料完整性、人工核验、不可变快照、维护人和 revision 冲突的独立领域代码及测试，为公共 schema 和 HTTP/页面接入提供可验证基础。

**Architecture:** 新代码位于 `internal/domain/supply`，不依赖旧业务表、Gin、GORM 或 B/C 具体服务。聚合返回独立副本；领域服务通过事务仓储接口写当前投影、版本和操作记录，通过来源策略接口验证官方来源。没有适配器时不假定正式接入已完成。

**Tech Stack:** Go 1.26.0、shopspring/decimal、标准库 testing；在已有 golang:1.26-alpine Docker 镜像中运行。

**Spec:** [F1 契约草案](../supply/supply-standard-models.draft.md)、[业务决策](../supply/decisions.md)、原始 A 详细设计 §3/4/8.1/16。

## Global Constraints

- 全部 A 产出文档放 `docs/Work_A/`；本切片继续完整目标中的阶段 1，不代替后续 F4/F3/F12/F8/F13/F2。
- 金额 `numeric(20,8)` 对应精度、decimal 字符串；禁止 float64 运算、指数形式和静默舍入。
- bigint 标识字符串；版本与 revision 不混用；未知/未披露不冒充不支持，零价格不冒充缺失。
- 默认创建者为维护人；可保存不完整资料，但不得核验/报价；内部读取不按维护人过滤。
- 实际资料变更重新核验；无变化不增版；旧快照、输入切片与返回副本相互隔离。
- 原型 `opValidateModel` 决定模态对应价格覆盖：文本输入/输出、图片/视频生成或输出、音频对应任务；不把不同条件同类价格合并。
- 本次不改公共 migrations、router.go、main.go、oplog/binding 和公共前端，也不修改运行数据库。
- 未确认的来源有效期不设默认天数；通过注入来源策略校验。HTTP 嵌套 schema 与公共 DDL 在实际接入前继续冻结。

## 文件和任务

### Task 1：价格项与官方资料结构

Files: `internal/domain/supply/price.go`、`price_test.go`；随后在 `model.go` 定义资料结构。

Interface: `ValidatePriceItems([]PriceItem) error`；`PriceItem` 保留 item_key/task_type/amount/currency/unit_code/unit_quantity/condition/dimensions/source_url。

- [x] 写表驱动反例：负数、超过八位小数、超过十二位整数、非三位大写币种、单位数量非正、重复稳定键、指数金额；合法零价格及同任务不同条件保留。
- [x] 运行 `go test -count=1 ./internal/domain/supply`，记录缺失实现/行为失败。
- [x] 用 decimal 和输入格式/精度校验实现，不转换浮点。
- [x] 重跑并记录绿色；格式化。预期合法零价通过，非法价格返回可定位字段错误。

锚点示例：
```go
items := []PriceItem{{ItemKey: "input", TaskType: "TEXT_INPUT", Amount: "0.00000000", Currency: "USD", UnitCode: "TOKEN", UnitQuantity: "1000000", Condition: "standard"}}
// 必须保留零价；把 Amount 改为 -0.1 / 0.000000001 时必须失败。
err := ValidatePriceItems(items)
```

### Task 2：模型聚合与版本

Files: `model.go`、`model_test.go`。

Interface: `NewStandardModel(id, actorID string, data ModelData, now time.Time)`；模型公开只读副本、MissingFields、QuoteReadiness。官方资料分为身份、模态、容量、能力、价格、API、生命周期、来源与日期；事实用结构化 key/state/value 表达，不用任意 JSON。

- [x] 写不完整保存/不可报价、完整仍未核验、模态价格缺项、未披露与未核验、无效日期、大 ID 不损失、输入/输出副本隔离测试。
- [x] 观察红色，补聚合及完整性实现。
- [x] 写维护人、同 revision 冲突、无变化不增版、资料变更撤销核验及快照独立的测试，分别观察红色后实现。
- [x] 重跑；版本历史以事务接口追加，聚合不在内存里伪装数据库历史。

关键断言：新建不完整模型为 INCOMPLETE；完整未人工核验为 UNVERIFIED；人工核验后才 VERIFIED；价格从 `2.00000000` 改为 `3.00000000` 后旧快照仍是 `2.00000000`。

### Task 3：事务领域服务与公共接入端口

Files: `ports.go`、`service.go`、`service_test.go`。

Interfaces: `ModelUnitOfWork.Within(ctx, func(ModelTransaction) error) error`；事务提供 ID 分配、锁定读取、创建/CAS 更新、追加版本和操作记录。`OfficialSourcePolicy.Validate(ctx, company, sources) error` 由公共/应用适配，缺失时核验失败关闭。`ModelService.Create/Update/Verify` 从参数认证 actor 获取维护人，不接收任意维护人指定。

- [x] 用事务内存替身写持久化结果测试：创建者绑定、非维护人403对应错误、资料更新新快照、同 revision 竞争仅一人成功、日志失败全部回滚、来源策略拒绝和未配置拒绝。
- [x] 先观察红色；只替换缺失的持久化与来源边界，领域规则执行真实代码。
- [x] 实现同一个 Within 中写投影/版本/日志；更新失败返回原错误，不吞依赖错误。
- [x] 绿色后检查失败路径没有新增快照/成功记录。明确内存替身不证明 PostgreSQL 隔离或公共日志集成。

### Task 4：复核与交付记录

- [x] `gofmt` 新包，`go test -count=1 ./internal/domain/supply`。
- [x] `go vet ./internal/domain/supply`，`go build ./...` 验证仓库可编译。
- [x] 对金额边界和维护人/版本关键分支做可恢复变异，确认相关测试变红后还原并重跑。
- [x] 在本目录记录真实 RED/GREEN、命令、范围和公共接入缺口；更新索引，不把独立测试称为真库/页面完成。

执行时使用 Docker 挂载仓库，缓存使用独立 Go module/build volumes；容器内 HTTP(S) 代理为 `http://host.docker.internal:12000`。不连接开发数据库来执行本切片测试。

## 原型与既有决策复核

- 原型 1009 行批次门槛与1012行进入办理生成资源相符；974行整批接入按用户已确认的逐资源准入修正。
- `opValidateModel` 的齐全校验保留为核验/可报价门槛，待补齐保存是用户明确批准的补充行为。
- 正式配置不复制测试凭据及维护人绑定策略保持已确认边界。
- 用户授权：复核合理后按切片契约先行＋TDD推进；以上复核完成，选择在当前指定工作目录的 `feature/supply` 分支执行，不另建工作区。
