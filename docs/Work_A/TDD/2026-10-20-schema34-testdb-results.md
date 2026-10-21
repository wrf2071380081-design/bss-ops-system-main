# 000033/000034 独立测试库验证记录

日期：2026-10-20。代码依据：origin/main `422be85`。

## 执行范围

在现有 PostgreSQL 容器内创建独立数据库 `model_bss_a_schema34_20261020`，从运行库 `model_bss` 复制结构，不复制业务数据。随后依次执行仓库中的 `000033_target_model.up.sql`、`000034_test_report_cache.up.sql`，每个文件在独立事务中执行并遇错停止，均成功。

这里验证的是两个 up SQL 脚本及执行后的实际结构，没有通过迁移运行器升级测试库；结构复制不包含 `schema_migrations` 的数据，因此不能把该测试库描述为迁移元数据版本已登记为 34。测试库保留供后续仓储验证使用。

## 实际检查结果

| 检查 | 结果 |
|---|---|
| F1 元数据检查器查询真实 PostgreSQL | 通过，能读取列、类型及索引定义 |
| standard_model 必需字段 | 缺少 maintainer_id、revision、version_no、verify_status、checked_by |
| standard_model_version.version_no | 实际 integer，与当前适配契约 bigint 不一致 |
| F1 接入就绪判定 | columns_compatible=false、repository_ready=false，符合预期 |
| 不完整官方身份草稿 | 第二条 company/official_id 均为空字符串的模型触发现有唯一索引冲突，验证了与允许保存不完整草稿的业务要求不符 |
| test_report 缓存字段 | cache_hit_tokens、cache_input_tokens 均为可空 bigint |
| 草稿约束验证数据 | 在事务中插入后回滚，standard_model 中合成记录数为 0 |
| 运行库复查 | model_bss 仍为 migration 32，dirty=false，未出现 standard_model 目标表 |

约束验证回滚不恢复测试库序列消耗。未修改运行库数据、迁移版本或服务配置，也未验证降级脚本。

## 测试证据与边界

新增 `internal/infra/supplypg/f1_schema_integration_test.go`，仅在显式提供专用测试库环境变量且实际数据库名称校验通过时运行。编译测试二进制后，在 PostgreSQL 容器内通过 Unix socket 执行 `TestF1SchemaPostgres34Metadata`，结果 PASS。

另执行 `go test -count=1 ./internal/domain/supply ./internal/api ./internal/infra/supplypg`，三个包通过。常规回归未提供测试库环境变量，该可选集成测试会跳过；真实数据库测试的执行结果来自上面的单独调用。

- [结构复制输出](./evidence/2026-10-20-schema34-testdb-baseline.txt)
- [000033 执行输出](./evidence/2026-10-20-testdb-000033_target_model.up.sql.txt)
- [000034 执行输出](./evidence/2026-10-20-testdb-000034_test_report_cache.up.sql.txt)
- [真实数据库检查器输出](./evidence/2026-10-20-schema34-live-inspector.txt)
- [缓存字段与草稿唯一约束验证](./evidence/2026-10-20-schema34-live-draft-constraint.txt)
- [运行库版本复查](./evidence/2026-10-20-schema34-runtime-unchanged.txt)
- [三个包回归输出](./evidence/2026-10-20-schema34-testdb-regression.txt)

## 下一接入条件

独立库验证确认：仅执行现有 000033/000034 还不足以接通 F1。公共负责人需按 [F1 增量结构交接需求](../supply/2026-10-20-f1-schema-handoff.md) 补齐字段、身份唯一策略和历史数据处理；A 随后实现真实读取与事务仓储，并验证核验、版本、并发和回滚，再接主路由及本地运行环境。

本轮没有修改已发布迁移，也没有提交或推送 Git。结果不代表 F1、工作台或模型广场已完成真实数据库闭环。
