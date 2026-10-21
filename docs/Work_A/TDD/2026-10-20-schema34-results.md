# 最新main字段核对与首批适配

日期：2026-10-20。依据main422be85的实际000033/000034迁移，完整映射见[差异清单](../supply/2026-10-20-schema34-gap-matrix.md)。

本轮新增infra/supplypg.ReportCacheRow，对应test_report.cache_hit_tokens/cache_input_tokens的可空bigint和scene。DomainCache映射为A.ReportCacheCounters：精确整数转字符串，保留0，不经浮点；缺失或非法历史统计按000034显示NOT_PROVIDED，不改写原行或拼接其他报告统计。

先写3组测试，记录[缺类型编译红色](./evidence/2026-10-20-schema34-red.txt)，再实现映射。

- [回归](./evidence/2026-10-20-schema34-tests.txt)：supply/API/supplypg共129组顶层测试通过。
- [vet](./evidence/2026-10-20-schema34-vet.txt)、[build](./evidence/2026-10-20-schema34-build.txt)：ExitCode=0。

覆盖0、常规计数、超过JS安全整数和接近PG bigint上限、双NULL/单NULL、负数/分母0/命中大于总数、返回副本及原行不变。本轮为纯字段转换测试，无真实SQL扫描或端到端数据库验证。

没有降低核验/CAS/权限/报告适用性要求来兼容缺列，没有改写000033/34或擅自新增公共迁移，没有迁移本地数据库。当前只完成缓存字段转换，完整F1/F3/计划仓储仍需增量结构与真实事务适配。全部修改保留工作区，未提交/推送。
