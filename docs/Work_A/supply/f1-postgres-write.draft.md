# F1 PostgreSQL 写仓储契约

> 2026-10-21按用户指示调整：[当前33–36基线](./2026-10-21-schema34-baseline.md)。现实现不依赖下文早期建议的五个独立控制列；控制元数据保存于已有版本快照，先锁行再比快照revision，保留integer版本及原身份唯一约束。以下相冲突的早期候选结构描述保留作演进记录。

2026-10-21，A 适配草案。前置：[公共增量结构](./2026-10-20-f1-schema-handoff.md)。

ModelUnitOfWork 通过注入 resolver 加入调用方已有 sql.Tx，公共日志通过同一个 tx 的回调写入。不自行 Begin/Commit/Rollback；返回模型只是外层提交前的候选，外层错误必须回滚。

- 显式选择 schema，生产应为 public；仅接受 SQL 标识符，不接受 SQL 片段。测试使用 PostgreSQL 会话临时表 pg_temp，不补齐 public 业务表。
- 标准模型依赖 maintainer_id/revision/version_no/verify_status/checked_by 增量列；不存在即 SQL 失败，不填默认归属或版本。
- NextID 使用目标 identity 序列；Create 用 OVERRIDING SYSTEM VALUE，兼容 GENERATED ALWAYS。新建归属由现有领域服务取认证创建者。
- GetForUpdate 以行锁读取全部当前资料和授权/CAS/核验元数据，不从版本 MAX 推断当前状态。
- Update 保留创建时间和维护人，WHERE id/revision 做 CAS；0 行冲突。数据库官方身份唯一冲突映射 ErrIdentityConflict，版本唯一冲突映射 ErrRevisionConflict，不截断字段。
- AppendVersion 只 INSERT，不修改已有快照。snapshot 为版本化对象：contract=supply.standard_model，schema_version=1，model=完整 StandardModel；其中 ID/revision/version_no 为字符串，价格仍为十进制字符串。旧快照不得据此直接当新格式读取。
- 当前投影、版本和公共日志必须在同一外层事务；NOOP 只写日志。SQL 失败后调用方不得继续使用失败事务或发布候选结果。

本切片不装配生产主路由，不实现公共转交，不替代来源核验策略，不交付公共迁移。临时目标表测试证明 SQL 适配与事务行为，不证明运行库已具备这些列、外键回填或公共幂等提交已达标。
