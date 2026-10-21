# F3 直接上下架领域切片

日期：2026-10-20。依据：A详细设计§6.2、§6.5、§8.3，v0.22的资源上下架和实际平台登记逻辑。

## 实现及边界

新增ResourceStateService.Change，当前仅支持绑定采购人直接OFFLINE/ONLINE命令；不接收计划ID授权标记，不替代F13原发起人执行服务，也不处理ADMIT或RETEST。

输入为认证WriteMeta及ResourceStateCommand：资源ID、expected_revision、action、external_confirmed、completion_note。必须确认真实外部操作并填写依据，原因和追踪必填。锁资源后检查当前buyer/revision/状态和ever_admitted；首次接入前不能走重新上架，重复业务状态请求返回冲突，HTTP幂等重放由外层处理。

OFFLINE从ADMITTED进入OFFLINE；ONLINE从OFFLINE进入ADMITTED，先在资源副本的候选状态调用现有BuildResourceSnapshot，重查当前模型核验、主体、正式配置版本和自身报告适用性，不忽略旧目标/失效/未通过报告。实际改变仅升资源revision、更新状态及时间，保留ever_admitted、测试目标与报告。

ResourceStateTransaction.AppendResourceStateEffect接收前后快照、事件类型、操作者、原因、请求追踪和实际完成依据。适配器需原子追加操作记录、自动公告和outbox；分配唯一event_id并用notice.source_event_id防重。影响范围PENDING表示尚未计算，不是0客户。C自行消费事件处理客户关系，A不直接修改客户配置；上架不自动恢复历史客户关系。

此处是领域实现和端口，没有真实日志/公告/outbox写库。ResourceStateUnitOfWork必须加入外层事务，实际日志/公告/事件唯一性及并发一致性仍需真库验证。

## TDD执行

先写下架成功、权限/确认/状态/revision拒绝、失败回滚及上架缺事实阻断测试，记录红色后实现。随后增加上架成功及模型/配置/压力测试/目标版本阻断场景。

- [红色输出](./evidence/2026-10-20-resource-state-red.txt)：新增服务类型缺失导致编译失败。
- [最终回归](./evidence/2026-10-20-resource-state-tests.txt)：supply/API共101组顶层测试通过，本切片新增5组。
- [vet](./evidence/2026-10-20-resource-state-vet.txt)、[build](./evidence/2026-10-20-resource-state-build.txt)：ExitCode=0。

通过Go容器挂载当前后端执行，事务适配为内存复制替身。验证效果端口失败和commit失败时无部分成功，不能据此证明数据库提交、锁和公告防重已正确实现。本轮未修改前端，未重新运行前端测试。

## 下一步

A新增严格HTTP请求及页面登记表单，接真实新表仓储/一致性事实读取、公共幂等事务和日志/outbox能力。F13计划登记单独检查原发起人；本次直接buyer命令不能被计划服务用于绕过该规则。完整ADMIT仍需B办理事实；RETEST需B明确内部触发权限。当前Docker服务及生产公共路由没有装配本命令。
