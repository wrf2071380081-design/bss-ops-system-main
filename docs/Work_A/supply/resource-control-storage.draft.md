# F3 资源控制快照存储契约

2026-10-21。沿用原生33/34，不增列、不改公共迁移。A实现存储适配，B仍负责可信申报、办理和报告事实，公共方仍提供事务/日志及生产装配。

## 载体与协议

- `resource` 保存已有业务投影；revision、来源行/目标版本、正式配置绑定版本、选定报告任务序号随完整非敏感资源快照保存到现有 `operation_log.after`。
- 控制记录固定 `entity_type=resource`、`action=RESOURCE_REVISION`，JSON 为 `contract=supply.resource`、`schema_version=1`、`operation=具体业务动作`、`resource=完整安全资源状态`。所有ID和版本用字符串表达；时间统一UTC及数据库微秒精度。
- 节点办理动作增加可选proof，保存flow_id、node_id、assignee_id和字符串flow_revision；资源来源行/版本和目标版本已在resource中。proof人员必须与操作日志actor一致；普通创建/其他不适用操作不凭空填造节点依据。
- 测试关联可选test_evidence保存本次安全报告引用、任务序号、来源/目标版本及处理结果；ADMIT保留completion_note与节点proof。更新控制头后会核对这些必要依据未被日志适配丢弃，字段不包含URL、凭据或报告正文。旧目标/旧任务/NOOP只留普通日志，不覆盖当前控制头。
- 该保留动作只承载真实资源变更，不用于普通浏览、NOOP或其他业务日志。真实业务动作保留在operation字段；日志展示接入需读该字段。before同样是旧控制快照，创建时为空。
- 同一资源先锁原生行，再以新语句读取最新控制记录（按日志ID倒序），revision直接取已保存JSON，不从时间、行数或最大版本临时推算。日志ID只确定同资源串行提交的记录顺序。
- 最新记录协议/字段损坏或原生投影不符时拒绝，不能退回较旧有效快照。没有控制快照的历史资源也拒绝，不自动认领或补初始revision。
- 这些记录属于业务状态依据，必须追加且保留，不能按普通临时日志清理；若公共日志未来归档/裁剪，需先显式迁移权威状态并保持同等读写协议。本实现不改公共日志管理策略，也不偷偷回填旧资源。

## 写入与并发

`ResourceStateStore`提供预分配ID、原生插入＋控制日志、锁定读取、expected revision更新＋控制日志等底层能力；不提供HTTP写入口，不代替已有领域准入和绑定校验。

创建只能由已校验的申报办理服务调用；领域初态仍为PENDING_TEST/PENDING_ACCESS。原生SQL的PENDING_ADMIT与领域PENDING_ACCESS互相映射。供应商/模型/区域唯一约束继续生效，不绕过已有identity。

更新锁资源，核对当前完整快照与原生列，比较expected revision；新revision必须恰好+1，资源ID、供应商、模型、区域及创建时间不变。原生写与控制日志使用调用方同一sql.Tx。日志适配只接收无密钥的结构；其成功返回后重读控制头，确认完整快照确已保存，不能把空回调当成功。

每个方法不自行Begin/Commit/Rollback。调用方发生任何错误必须整体回滚，且在外层提交成功后才发布HTTP/幂等响应。持有锁期间不发网络请求。真实B行回写、节点结果和outbox仍要加入同一外层事务，不能因A存储完成而省略。

## 申报创建事务适配

`NewResourceCreationWork(store, resolve, ResourceCreationHooks)` 接入既有ResourceCreationService。A重新从SQL读取供应商/模型及现有资源，不把B响应中的旧Supplier/Existing当作权威A状态；重复创建返回当前控制快照，不重建状态或重置revision，追加普通CREATE_NOOP日志，不写新的控制头。

LockSource、AttachRow及必要的PrepareReport由B提供，在传入的同一sql.Tx内完成真实身份/不可变版本及分派验证、申报行CAS和草稿报告转存。LockSource须实现跨模块统一的锁序与不存在资源时的身份键保护：已有资源的办理、创建重读与接入必须统一为资源锁在B行/节点锁之前；不能先单独锁B行，再与持有资源锁的办理事务互相等待。可以先非锁读取不可变来源身份，按统一顺序取得资源/身份键及B锁后重读并核对版本。当前测试的B回调是夹具，不证明该真实锁协议已交付；上线前须共同验证创建与办理/接入的竞争。

原生report要求resource_id非空且有FK，因此草稿报告不能提前伪造资源。创建先插入latest_test_report_id为空的真实资源行，然后PrepareReport将已选草稿证据转存为该资源的原生报告，再以真实SQL核对报告/任务归属及连通/压力结论与原可信证据一致，最后关联报告并保存控制头。没有转存提供方、报告结论被替换或B行回写失败，均返回错误并要求外层整体回滚。既有报告ID须由B按共享ID空间保持引用，不静默替换选定报告。

资源进入PENDING_ACCESS仍不是ADMITTED，正式配置另行绑定，报价仍受模型核验、逐资源测试和正式配置等条件约束。此适配不新增浏览器直建资源入口，也不实现供应商报价审批。

## 读取组合

`ResourceStateStore.FactsLoader(reportLoader,configLoader)` 从真实控制记录组装前一切片的ResourceReadFacts。无报告/无配置绑定不调用对应提供方；有引用而没有对应真实提供方返回依赖不可用。B报告引用和正式配置安全摘要仍由各自可信适配提供，不存凭据、不假定通过。

登记接入组合读取将原生行的带时区时间规范为UTC/微秒后再逐字段比较，不以Go time.Location标记不同误判同一时刻为不同版本；保留身份、业务字段、revision与控制头的严格一致性检查。真实A组合链路与事件验证见[证据/接入适配](./resource-admission-storage.draft.md)。

## 本轮验收

原生34临时表＋整体回滚；创建/更新/重读revision与来源绑定、旧revision拒绝、唯一冲突、最新坏快照拒绝、绕过业务修改拒绝、日志SQL失败及空日志回调失败、外层回滚、只读仓储及HTTP采用真实控制头。创建服务测试补回原生资源↔报告↔任务外键验证顺序，B回调仍为受控提供方。独立连接真实竞争行锁，验证先写未提交不可见、后写等待后因旧revision冲突，以及没有额外版本/日志。B与正式配置提供方及生产装配另验，不把夹具当生产接口。
