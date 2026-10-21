# A 公告阅读写入的真实事务适配

日期：2026-10-20。范围：标记已读的 SQL 事务参与者、公共日志注入端口、首次阅读时间精度与真实并发验证。

## 契约与责任边界

新增 `supplypg.NewNoticeUnitOfWork(resolve, appendLog)`，实现既有 supply.NoticeUnitOfWork。resolve 从每次请求 context 提取外层 *sql.Tx，appendLog 接收同一 *sql.Tx 和 NoticeReadOperation。服务可长期持有此 UnitOfWork，但每次 Within 创建新的事务参与者，不缓存请求事务。

适配器不自行 Begin/Commit/Rollback；外层必须处理业务错误回滚，并在成功 Commit 后才对外发布 receipt。缺解析函数、日志函数或事务时明确不可用。公共日志 helper 仍由公共负责人提供，本次仅定义调用端口；测试中的直接 operation_log INSERT 是测试夹具 logger，不是生产公共 helper。

SQL 顺序：公告行 FOR UPDATE → 人员引用 FOR KEY SHARE → 个人阅读行 FOR UPDATE → 首次 INSERT 或保留原时间 → 同事务追加 READ/READ_NOOP 日志。公告锁覆盖个人阅读行尚不存在的情况。适配器检查操作前后快照与实际读取/插入内容相符，不接受任意 entity/actor 或变更首次时间的 NOOP。追踪号按现有 varchar(64)、原因按 varchar(500) 检查，拒绝超长，不截断。

详见 [存储契约](../supply/notice-storage.draft.md)。该协议及公共接入仍待共同评审。

## TDD 过程

1. 先写操作/依赖校验及真实写入测试，stub 返回依赖缺失，观察 [单测红阶段](./evidence/2026-10-20-notice-write-red.txt) 与 [真库红阶段](./evidence/2026-10-20-notice-write-pg-red.txt)。
2. 实现请求事务解析、行锁、首次插入、只追加日志端口与结构校验，真库基础场景通过。
3. 补并发测试，通过 PostgreSQL pg_blocking_pids 确认第二事务实际等待第一事务的公告锁；不是仅通过延时猜测阻塞。
4. 检查发现时间精度缺口：Go 首次回执可含纳秒，PostgreSQL timestamptz 保存微秒，重复读取会出现差异。新增 [精度红阶段](./evidence/2026-10-20-notice-write-precision-red.txt)，随后将 NoticeService 的首次时间及日志时间统一为 UTC 微秒，带纳秒输入的真库测试通过。

## 六个真库场景

复用独立库 model_bss_a_schema34_20261020，执行前校验显式环境变量、专用前缀及 current_database()；测试夹具先提交以便多个连接观察，再按确切主键清理。未修改迁移结构。

| 场景 | 验证 |
|---|---|
| 首次阅读 | 返回候选 receipt；外层提交前，另一个连接看不到阅读及日志；提交后各有一条。logger 收到的 *sql.Tx 与请求事务完全相同 |
| 新的重复阅读意图 | created=false，保持首次时间，阅读行仍一条，追加 READ_NOOP 日志 |
| 实际 SQL 日志失败 | 测试 logger 故意向 varchar(64) 写65字符，确认 PostgreSQL 22001 错误；业务不返回成功 receipt，外层回滚后阅读及日志均不落库 |
| 外层主动回滚 | 即使业务已生成候选 receipt，外层回滚仍同时撤销阅读和日志 |
| 不存在人员 | 返回 Forbidden，不生成阅读或日志 |
| 两个并发首次阅读 | 明确使用 READ COMMITTED；确认第二连接被第一连接阻塞，第一提交后第二读取到首次时间并走 NOOP；最终一条阅读、两条日志 |

并发验证只覆盖 READ COMMITTED，未验证更高隔离级别或真实公共幂等重放。外层主动回滚不是“注入 Commit 失败”，本次未注入实际 Commit 失败。锁保护要求所有该业务写入遵循同一锁顺序。

## 最终证据

- [六个真实数据库场景](./evidence/2026-10-20-notice-write-pg.txt)
- [领域/API/适配三个包回归](./evidence/2026-10-20-notice-write-tests.txt)
- [go vet](./evidence/2026-10-20-notice-write-vet.txt)
- [go build ./...](./evidence/2026-10-20-notice-write-build.txt)
- [夹具清理](./evidence/2026-10-20-notice-write-cleanup.txt)：专用测试机构残留0，删除人员前其阅读及日志关联均已处理
- [运行库复查](./evidence/2026-10-20-notice-write-runtime.txt)：model_bss 仍32、dirty=false

常规包回归未设置专用库变量，可选真库测试默认跳过；实际执行证据是第一项单独调用。已执行 gofmt。未增加前端改动。

## 生产接入仍需完成

公共方需适配现有幂等事务句柄到请求 *sql.Tx 解析函数、提供同事务公共日志函数，并落实提交后响应。随后可构造 NoticeService(NewNoticeUnitOfWork(resolve, appendLog), clock) 注入 A 的 SupplyDeps.NoticeWriter。适配器不凭空取得事务，也不替换公共认证或幂等机制。

当前没有把真实写入接入生产主路由，没有宣称公告端到端完成；运行中 Docker 服务未更换、运行库未升级，代码未提交或推送。
