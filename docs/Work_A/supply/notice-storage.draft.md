# A 公告存储契约草案

依据：000033 的 notice/notice_read、工作台 A 公告接口及 v0.22 按人员查看阅读状态规则。本文是 A 适配器的版本化协议，待公共及事件生产方接入评审；不自动转换既有公告，不修改公共迁移。

## 表字段映射

| 表字段 | A 语义 |
|---|---|
| notice.id | 公告 ID |
| notice.type | Notice.Kind |
| notice.target_type | RESOURCE |
| notice.target_id | Notice.ResourceID |
| notice.created_at | Notice.PublishedAt，发布时业务时间 |
| notice.payload | 下述版本化资料；不含可覆盖表字段的 ID、kind、发布时间 |
| notice_read.notice_id/staff_id/read_at | 个人首次阅读记录，人员使用 internal_staff.id |

payload 结构：

```json
{
  "contract": "supply.notice",
  "version": 1,
  "resource_name": "资源发布时名称",
  "region": "CN",
  "title": "资源公告",
  "content": "公告正文",
  "reason": "变更原因",
  "source_event_id": "123",
  "affected_status": "PENDING"
}
```

source_event_id 可省略；如有必须为正 bigint 字符串。affected_status=PENDING 时不能带数量；READY 时必须带 affected_customer_count，使用无符号整数字符串，允许 "0"。区域只接受 CN/OVERSEAS。未知字段、未知版本、缺标记或无效资料拒绝，不从资源当前状态猜历史公告，不把默认空对象当完整公告。

## 读取行为

- 仓储只读取 contract=supply.notice 的 A 公告，其他业务公告交各自适配器；A 标记下的未知版本在读到时明确报错，不按 v1 猜测。
- 选择人员来自真实 internal_staff 目录，包含保留的历史人员，不用登录账户替代人员。显式选择不存在人员返回不存在错误；all_staff 使用完整目录，不能先分页人员。
- notice 与人员组合后左联 notice_read；先按 READ/UNREAD、region、resource_id、keyword 过滤，再计算总数和分页。
- keyword 为标题、正文和资源名称中的字面子串，百分号、下划线不作通配符。
- 排序 created_at DESC、notice.id 数值 DESC、staff.id 数值 ASC；total 是公告/人员组合数量。分页超出范围仍保留正确 total。
- 列表的人员检查、count 和分页查询使用同一个只读 REPEATABLE READ 事务；查询和提交错误返回错误，不降级为空列表或未读。
- 仓储不生成 can_mark_read；交既有 NoticeReader 根据认证 actor 与所查看人员生成权限，查看他人不代办。

读取切片不写 notice_read，不装配生产主路由。

## 标记已读的事务适配

A 的 NoticeUnitOfWork 接收“从当前请求 context 取得已有 *sql.Tx”的解析函数，以及公共日志追加函数。每次调用从请求解析事务，不把某一次请求的事务保存在全局服务中；缺任一依赖即失败，不自行开启或提交另一笔写事务。

处理顺序：锁定 A 公告行 → 校验并锁定人员引用 → 读取个人阅读行 → 首次插入或保持原 read_at → 使用同一 *sql.Tx 追加 READ/READ_NOOP 操作日志。公告行锁覆盖阅读行尚不存在时的竞争；重复意图可以留 READ_NOOP 日志，但不改首次时间。公共幂等重放应在进入业务前返回原结果。

适配器仅参与外层事务，业务返回的 receipt 在外层提交前是候选结果。外层必须在错误时回滚、成功提交后才发送成功响应；不能直接把候选 receipt 当已提交成功。日志函数由公共负责人提供，A 只定义注入端口，不替代 pkg/oplog。请求追踪号须适配现有 varchar(64)、原因 varchar(500)，超长明确失败，不截断。

首次阅读时间及同次日志时间统一为 UTC 微秒精度，避免纳秒回执与 PostgreSQL 微秒存储不一致。并发首次阅读验证使用外层 READ COMMITTED 事务；公共方如使用更高隔离级别，应评审并处理序列化冲突的整笔事务重试，不把异常当作已读成功。
