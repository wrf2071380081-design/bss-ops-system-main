# A 模块契约索引

<a id="supply"></a>

本目录只维护12份当前文档，不再按每次切片新增文件。实现/验收状态先看交接，业务决定看DEC，具体字段/HTTP/事务看模块契约。历史测试结果只放TDD/SUMMARY；旧原文及一次性SQL在本机忽略目录留档，不在Git再建一套archive。

| 文档 | 唯一用途 |
|---|---|
| [需求与职责](./requirements.md) | A/B/C边界、主路径、已撤销旧前提和开发约束 |
| [业务决定](./decisions.md) | DEC编号和覆盖关系 |
| [交接与接入](./handoff.md) | 当前交付/待接事实、构造/事务端口、部署与待验边界 |
| [F1标准模型](./supply-standard-models.draft.md) | 资料/价格/核验、HTTP、版本读写、官方来源与日志 |
| [F3资源广场](./supply-resources.draft.md) | 读取/来源/控制快照、测试证据、配置绑定/接入与回写 |
| [F4供应商配置](./supply-suppliers.draft.md) | 主体、曾用名、资质、正式配置、合并预览及测试配置 |
| [F12合同授信](./f12-records.draft.md) | 双主体、版本、授权/齐备、字段/约束及旧读取兼容 |
| [F13工作台计划](./supply-workbench.draft.md) | 个人阅读、计划/提醒/实际执行、内部改价及事务 |
| [数据库升级](./database-v45-upgrade.md) | 正式43/44/45、本机旧编号识别、备份和回退 |
| [附件部署](./f4-f12-file-persistence-deployment.md) | 三类目录、持久挂载与文件生命周期 |
| [原型交互](./prototype-alignment.md) | 已观察/确认的UI与展示约定；不复制执行结果 |

当前日常库45/false，main@1fe8907已合并，运行API尚未替换为合并构建。本轮已获用户授权提交推送到work-A-fix。测试/计划入口见[TDD索引](../TDD/README.md)，全工作入口见[Work_A](../README.md)。
