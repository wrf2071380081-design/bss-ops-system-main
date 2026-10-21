# F1 现有资料字段编解码适配

日期：2026-10-20。依据000033实际列和[F1公共交接需求](../supply/2026-10-20-f1-schema-handoff.md)。

新增infra/supplypg.ModelDataRow及EncodeModelData/DomainData，映射已有资料标量、模态列表、JSONB和可空checked_at，不推断核验、维护人或revision。

允许capacity/abilities/api_access/lifecycle默认空对象映射为空资料列表；非空旧对象、null、未知结构字段及尾随JSON明确报映射错误，不静默丢数据。新写JSON统一数组；nil模态数组规范为非nil空数组，避免NOT NULL列被写成NULL。价格字符串/维度大整数文本原样往返；数据库字符长度和日期不兼容则拒绝，不截断。

先写4组测试，[红色](./evidence/2026-10-20-f1-row-red.txt)为新映射类型缺失的编译失败。

- [后端回归](./evidence/2026-10-20-f1-row-tests.txt)：supply/API/supplypg共133组顶层测试通过。
- [vet](./evidence/2026-10-20-f1-row-vet.txt)、[build](./evidence/2026-10-20-f1-row-build.txt)：ExitCode=0。

测试覆盖默认空结构、非空旧JSON拒绝、型化资料和来源日期往返、零价格及大整数维度文本、超长资料和非法日期、模态副本。Go容器执行，无真实SQL/驱动text[]扫描/仓储事务验证；本轮未改前端。

编解码不代替领域业务校验，也不对未核验行开放报价。完整F1仓储仍待公共增量列及初始数据策略落定。未迁移数据库，未提交/推送。
