# F4 正式配置独立切片执行记录

日期：2026-10-20；分支：`feature/supply`。

## 1. 范围与依据

按 [F4 契约](../supply/supply-suppliers.draft.md) 实现正式配置新增、修改、逻辑停用的独立领域服务。原型968—971行要求实际外部操作确认、URL/Key/说明及引用保护；最终供应商页面1053行展示办理、资源和主体资料分区。本切片不改页面，不以原型中的内存明文存储作为生产实现。

交付代码：`internal/domain/supply/formal_config.go`、`formal_config_service.go`、`formal_config_test.go`。

- 事务内锁供应商，校验绑定采购人；区域来自供应商事实，跨供应商配置不可修改。
- 更新核对 revision；省略 Key 保留，传空拒绝，传非空 Key 轮换并递增版本；配置变化记录新的显式确认依据，旧 revision 不能继续作为办理前提。
- 使用认证加密端口并绑定 supplier/config/purpose 上下文；缺加密器时新建/轮换失败，不提供明文降级。
- 返回及日志使用独立安全视图，仅含 has_api_key 和固定掩码；密文及密钥版本也不进入返回/日志。加密器内部错误不原样透出。
- 有资源引用时禁止停用；成功无变化保存和重复停用仍留痕；日志失败不返回成功。
- DEC-007 已确认税率使用比例字符串，但本切片尚未实现供应商主体字段。

## 2. 红色与反例验证

| 证据 | 观察 |
|---|---|
| [初始红色](./evidence/2026-10-20-f4-formal-config-red.txt) | 新领域类型/接口缺失导致编译失败，退出码1；这是缺实现基线，不是业务断言失败 |
| [采购人授权变异](./evidence/2026-10-20-f4-mutation-buyer.txt) | 暂时取消绑定采购校验后，其他采购人的写入错误地成功，业务测试失败；随后恢复 |
| [资源引用变异](./evidence/2026-10-20-f4-mutation-reference.txt) | 暂时取消引用阻断后，被引用配置错误地停用，业务测试失败；随后恢复 |

## 3. 最终验证

全部命令在已有 Docker Go 镜像内运行，不连接数据库。变异恢复后执行以下回归：

| 命令 | 结果 | 证据 |
|---|---|---|
| `go test -count=1 -v ./internal/domain/supply -run TestFormalConfig` | 8个新增顶层测试组及其表驱动子用例通过 | [F4 定向输出](./evidence/2026-10-20-f4-formal-config-tests.txt) |
| `go test -count=1 -v -cover ./internal/domain/supply` | 33个顶层测试组通过，包覆盖率88.0%，退出码0 | [领域回归](./evidence/2026-10-20-f4-domain-regression.txt) |
| `go test -count=1 -v ./internal/api` | 25个顶层测试组通过，退出码0 | [API 回归](./evidence/2026-10-20-f4-api-regression.txt) |
| `go vet ./internal/domain/supply ./internal/api` | 无诊断，退出码0 | [vet](./evidence/2026-10-20-f4-vet.txt) |
| `go build ./...` | 后端编译通过，退出码0 | [build](./evidence/2026-10-20-f4-build.txt) |

测试执行真实领域服务；内存事务替身只模拟提交/回滚，加密替身只验证调用边界和安全投影，并非密码学实现。通过这些测试不能证明生产加密、SQL隔离、资源并发绑定或真实跨模块事务安全。

## 4. 接入和剩余交付

公共方需提供目标DDL、版本化认证加密及密钥管理、事务/日志、绑定/人员能力；SQL适配必须遵循相同锁顺序，避免并发关联绕过停用检查。正式配置新增/轮换依赖加密器；仅保留原密文的更新和停用不需要重新加密。

本轮没有修改运行环境或主路由，也没有实现 F4 HTTP、新主体 CRUD、网站账户、资质附件或页面。F3逐资源准入、完整 F1/F4 接入、阶段2及阶段3仍在 A 的整体目标内。下一步继续供应商主体（含税率、区域锁定和绑定规则）及资源领域能力。
