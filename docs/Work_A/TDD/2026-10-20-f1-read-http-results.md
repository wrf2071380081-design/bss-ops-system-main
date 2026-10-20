# F1 读取与 HTTP 切片执行记录

日期：2026-10-20。工作分支：`feature/supply`。本记录承接 [F1 领域切片](./2026-10-20-f1-domain-results.md)。

## 1. 已交付范围

- `ModelReader`：列表、详情、版本列表及 B/C 使用的历史快照；ID 保持字符串，历史价格保留，历史版本不可用于新报价。
- `RegisterSupplyRoutes`：三个读取接口以及新增、整体修改、人工核验接口。内部人员可读，写操作使用认证 StaffID，并交由领域检查维护人。
- 严格请求解码：限制体积和嵌套深度，拒绝无效 UTF-8、重复/非规范字段、尾随 JSON、数字型金额/版本、自报维护人及不完整 PUT 结构。
- 扁平 DTO、缺失项、操作资格、报价就绪判断、统一错误映射，以及现有公共幂等中间件接入。
- 补正无变化写入留痕：新请求成功保存/重复核验时写 `UPDATE_NOOP` / `VERIFY_NOOP`，不增加版本、revision，也不改变资料更新时间；日志失败不得成功。幂等重放由中间件直接返回，不再次调用领域写服务。

## 2. TDD 与缺陷证据

读取和 HTTP 切片按实施计划推进。实现过程中补充了字符串版本上限与无效 UTF-8 的失败用例。以下持久化证据可以直接核查：

| 检查 | 实际观察 |
|---|---|
| [无变化写入红色输出](./evidence/2026-10-20-f1-noop-audit-red.txt) | 修复前 3 个业务断言失败：缺少保存日志、缺少重复核验日志、日志失败仍返回成功；退出码1 |
| [重复键变异](./evidence/2026-10-20-f1-read-http-mutation-duplicate-json-key.txt) | 移除重复键保护后，请求错误地返回200，测试失败；随后恢复实现 |
| [历史报价资格变异](./evidence/2026-10-20-f1-read-http-mutation-historical-readiness.txt) | 将历史快照强制标为可报价后，测试失败；随后恢复实现 |

上一切片测试 `NoOpDoesNotCreateVersionOrLog` 的“不留痕”期待已被本轮替换。上一份输出属于历史基线，不能用来证明当前无变化写入规则。

## 3. 最终验证

以下命令在 Docker `golang:1.26-alpine` 内运行，未连接数据库。

| 命令 | 结果 | 原始输出 |
|---|---|---|
| `go test -count=1 -v -cover ./internal/domain/supply` | 25 个顶层测试组通过；包语句覆盖率88.0%；退出码0 | [领域测试](./evidence/2026-10-20-f1-read-domain-tests.txt) |
| `go test -count=1 -v ./internal/api` | 25 个顶层测试组通过，其中7组为新增 Supply HTTP 测试；退出码0 | [API 回归](./evidence/2026-10-20-f1-api-regression.txt) |
| `go vet ./internal/domain/supply ./internal/api` | 无诊断，退出码0 | [vet](./evidence/2026-10-20-f1-read-http-vet.txt) |
| `go build ./...` | 后端编译通过，退出码0 | [build](./evidence/2026-10-20-f1-read-http-build.txt) |

领域测试运行真实模型/服务，替换持久化与官方来源策略。HTTP 测试运行真实 Gin 注册器、解码、DTO、错误映射和公共幂等中间件；读取仓储、写服务边界和幂等存储使用替身，认证上下文由测试注入。因此不能把 HTTP 测试视为真实登录、数据库或端到端验收。

## 4. 待接入与完整目标

- 目标 schema、真实仓储、SQL 排序/分页、事务与日志适配、官方来源策略仍需按[公共接入请求](../supply/public-integration-request.md)落地验证。
- 公共幂等同键异体目前400，与共享契约409有差异；FAILED 重试及提交失败后的响应一致性仍需公共负责人处理。
- 主路由、入口和生成 OpenAPI 未修改；新增代码尚未切换到本机正在运行的 API。当前 Docker 环境和旧页面不代表新版 F1 已部署。
- F1 原型页面、F4/F3、阶段2的 F12/F8/F13 以及阶段3的 F2 仍在完整 A 工作范围内。本切片不代表完整 F1 或阶段1完成。
