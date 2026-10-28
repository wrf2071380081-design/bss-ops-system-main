# A 测试与联调入口

<a id="tdd"></a>

| 文件 | 用途 |
|---|---|
| [SUMMARY](./SUMMARY.md) | 唯一执行结果：真实业务、fixture、构建及未测限制分别记录 |
| [当前验收清单](./handoff-acceptance.md) | 当前待验与可复用结论；不复制完整历史 |
| [A/B联调计划](./ab-integration-plan.md) | 跨流程/报告/办理及运行版本的下一步 |
| [供应商与客户F12计划](./supplier-customer-a-plan.md) | 剩余A供应侧/客户F12切片及依赖 |
| [定向回归场景](./regression-scenarios.md) | 合并早期模块计划的场景；不等于已运行结果 |

测试源码跟随业务代码；此目录只保存场景/计划与唯一结果。旧独立验收报告已并入SUMMARY并保留执行日期/环境边界；不要依据旧环境命令重建已清理副本。只用原日常model_bss环境，受影响场景定向验证，不全站重跑。

截图/终端输出/生成物放忽略的.qa或evidence，不新增*-results.md；临时库、数据转储和私有附件不入Git。当前源码、运行二进制及数据库版本分别看[交接](../supply/handoff.md)，不从旧文件名推断部署。
