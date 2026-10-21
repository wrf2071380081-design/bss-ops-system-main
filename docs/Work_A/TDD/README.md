# A 模块 TDD 文档

- [阶段汇总](./SUMMARY.md)：合并PR #3中31份结果报告，保留验证结论和未完成边界。
- [业务契约与决策](../supply/README.md)。
- [上下架入口纠正](./2026-10-20-plan-only-correction.md)：直接登记已撤下，以计划流程为准。

## 保留的实施计划

- [F1 标准模型独立领域切片实施计划](./2026-10-20-f1-domain-plan.md)
- [F1 v0.22 多值筛选计划](./2026-10-20-f1-filters-plan.md)
- [F1 标准模型库前端切片（v0.22）](./2026-10-20-f1-frontend-plan.md)
- [F1 读取与 HTTP 接口切片实施计划](./2026-10-20-f1-read-http-plan.md)
- [F3 模型广场前端切片（v0.22）](./2026-10-20-f3-frontend-plan.md)
- [F3 资源读取与模型广场独立切片](./2026-10-20-f3-read-plan.md)
- [F4 正式配置独立切片计划](./2026-10-20-f4-formal-config-plan.md)
- [F4 供应商主体切片计划](./2026-10-20-f4-supplier-profile-plan.md)
- [工作台 A 前端子组件计划](./2026-10-20-workbench-a-frontend-plan.md)
- [工作台 A 子能力 TDD 计划](./2026-10-20-workbench-a-plan.md)

## 维护约定

测试源码随业务代码维护；结果更新到 SUMMARY.md。evidence/ 只在本地保存，不提交终端输出、截图或生成产物。历史提交保留，不用 amend/rebase/force push 清理已合入记录。
