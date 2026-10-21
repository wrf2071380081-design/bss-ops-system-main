# 工作台 A 公告真实读取仓储验证

日期：2026-10-20。范围：A 公告存储编解码、个人阅读投影的 SQL 读取和 A HTTP 注册器验证。

## 契约和代码

先形成 [公告存储契约草案](../supply/notice-storage.draft.md)，明确 notice 表字段与版本化 payload 的映射。协议目前是 A 实现草案，待公共与事件生产方接入评审，不代表已冻结跨模块生产协议。

新增：

- `internal/infra/supplypg/notice_row.go`：EncodeNoticePayload、NoticeRow.DomainNotice，严格校验 A 标记、版本、身份、区域及影响数量，保持精确整数字符串。
- `internal/infra/supplypg/notice_read.go`：NoticeReadStore 实现领域 NoticeReadStore 端口，构造参数是现有 *sql.DB；不从配置自行读取凭据。
- `internal/infra/supplypg/notice_row_test.go`、`notice_read_test.go`、`notice_read_integration_test.go`。
- `internal/api/supply_notice_pg_test.go`：真实 A HTTP 处理器与数据库集成测试。

仓储每次读取建立只读 REPEATABLE READ 事务，列表在同一快照完成目录检查、count、分页读取。事务提交错误也返回错误。全部输入参数化；只允许固定排序。人员检查覆盖整个显式选择，不静默丢弃不存在的人。全部人员从完整 internal_staff 目录取得，不先分页人员。领域 NoticeReader 仍负责根据认证 actor 计算操作权限。

## TDD 执行

1. 先写 payload 往返及拒绝测试，stub 返回 ErrReadContract，观察失败：[编解码红阶段](./evidence/2026-10-20-notice-pg-red.txt)。
2. 编解码实现后，先写真实读取测试；仓储 stub 返回依赖缺失，五个子场景均失败：[仓储红阶段](./evidence/2026-10-20-notice-pg-integration-red.txt)。
3. 实现 SQL 读取，再执行相同真实库测试通过；补充参数绑定、分页溢出、人员输入、缺 DB 等边界测试。
4. 将真实仓储注入 A 的 RegisterSupplyRoutes，在 httptest 中执行公告读取接口，验证响应、身份边界和无写入。

## 真实数据库证据

数据库：`model_bss_a_schema34_20261020`，不是运行库。测试显式校验环境变量、专用名称前缀和 current_database()。读取适配器创建独立快照，因此夹具先在专用库提交，测试结束按确切主键清理公告、人员和机构；不是在未提交夹具上测试，也没有清理运行数据。集成测试按本次记录逐个执行；共享测试库不应被其他测试同时修改。

| 验证 | 结果 |
|---|---|
| 公告/人员组合排序分页 | 先过滤再分页，按时间、公告数值ID、人员数值ID排序，跨页及越界 total 正确 |
| READ / UNREAD | 在 count 和分页前过滤；本人未读可操作、他人投影不可操作 |
| 全部人员与关键词 | 使用实际目录，百分号和下划线为字面字符，不是 LIKE 通配符 |
| 公告详情 | 同一公告不同人员状态独立，保持原始 read_at；GET 不新增阅读行 |
| 协议隔离 | 非 supply.notice 公告不混入；读到 A 未知版本明确失败 |
| 精确资料 | PENDING 不编造0；READY 可以明确为0；大整数资源ID/影响数量不经 float |
| 缺失人员 | 明确失败，不返回少了一人的“成功”分页 |
| HTTP | 本人列表、人员投影和详情通过；401、403、404、400通过 |
| 清理及运行库 | 专用测试机构残留0；运行库仍32、dirty=false |

原始输出：

- [领域读取器→真实PostgreSQL，五个子场景](./evidence/2026-10-20-notice-pg-integration.txt)
- [A HTTP→领域读取器→真实PostgreSQL](./evidence/2026-10-20-notice-pg-http.txt)
- [领域/API/适配三个包回归](./evidence/2026-10-20-notice-pg-tests.txt)
- [go vet](./evidence/2026-10-20-notice-pg-vet.txt)
- [go build ./...](./evidence/2026-10-20-notice-pg-build.txt)
- [夹具清理复查](./evidence/2026-10-20-notice-pg-cleanup.txt)
- [运行库复查](./evidence/2026-10-20-notice-pg-runtime.txt)

常规回归没有设置专用数据库变量，会跳过可选集成测试；真实执行证据是上述两项单独调用。REPEATABLE READ 配置和错误返回路径有代码依据，本次未注入并发写入或提交失败场景，不将其描述为已完成并发/commit失败测试。

## 接入边界

HTTP 测试使用真实处理器和 SQL，但认证上下文由测试注入，未验证实际登录会话；不是生产 main/router 装配或运行中 Docker 服务验收。未启用公告写入、标记已读真实事务、公共工作台四状态聚合。标记已读仍需与公共幂等事务和操作日志一起落实。

下一步公共方可在协议评审和运行库迁移后，将 `supply.NewNoticeReader(supplypg.NewNoticeReadStore(db))` 注入 `SupplyDeps.Notices`。A 继续实现阅读写入适配并验证首次/重复阅读及日志失败回滚。此次未更改公共迁移、未升级运行库、未提交或推送 Git。
