# F1 数据库结构检查及本地实查

日期：2026-10-20。承接[F1增量结构交接](../supply/2026-10-20-f1-schema-handoff.md)。

新增infra/supplypg.InspectF1Schema：显式传入sql.DB及schema，参数化查询information_schema.columns和pg_indexes，不创建连接、不改结构、不启用路由。AssessF1Columns按拟建F1仓储契约输出缺列/类型不符清单，排序稳定；列齐备只代表columns_compatible，repository_ready始终false，仍需人工/真库约束、数据和事务验收，不能将列存在当作完整就绪。

检查包含standard_model核验/维护人/revision/当前版本、八类资料列、standard_model_version快照及bigint版本、operation_log核心字段；索引定义只读取用于审阅，不以字符串匹配伪证唯一约束正确。长度、NULL、FK、草稿身份唯一、版本唯一、历史回填、CAS和提交响应列为RequiredChecks。

## TDD与验证

先写4组测试，记录[缺新接口编译红色](./evidence/2026-10-20-f1-schema-red.txt)，再实现。

- [回归](./evidence/2026-10-20-f1-schema-tests.txt)：supply/API/supplypg共137组顶层测试通过。
- [vet](./evidence/2026-10-20-f1-schema-vet.txt)、[build](./evidence/2026-10-20-f1-schema-build.txt)：ExitCode=0。
- [真实本地只读检查](./evidence/2026-10-20-f1-schema-live32.txt)：model_bss_pg/model_bss的schema_migrations=32、dirty=false；public下三个F1目标表的列和索引查询均0行。

单测覆盖缺字段不补默认值、错误revision类型拒绝、列齐备不宣称完整就绪、nil连接拒绝。真实库通过psql查询同类元数据，尚未对InspectF1Schema的sql.DB扫描函数做数据库集成测试；不能把psql结果说成该函数已端到端测试。

本轮没有应用迁移/重启服务/修改数据，没有新增公共DDL或填假版本仓储。后续应在独立测试库应用公共迁移并补齐F1缺字段，再实现与验证真实仓储；当前运行库不具备目标表，生产新入口保持未装配。修改保留工作区，未提交/推送。
