# 上下架入口按原型纠正

日期：2026-10-20。用户确认见[DEC-010](../supply/decisions.md)。

原型vResourceTable对已有待执行计划显示计划信息/发起人登记，无计划时进入vPlanCreateForm；vPlanForm/vExecutePlan由原发起人登记完成。先前直接登记切片未完整遵循这一入口流程，本次撤下直接状态HTTP路由、writer依赖和处理器、前端直接登记组件/请求及按钮。新增测试断言原直接状态路由404，避免仅隐藏前端后仍能调用。

保留底层领域状态逻辑供后续重构复用，并补HasPendingStatePlan事务端口：有PENDING/EXECUTING上下架维度计划返回409；查询失败关闭。计划创建/取消/执行及直接内部命令须先取得相同资源锁，避免先查后创建竞态。PRICE_CHANGE属于独立维度，不应一概阻断。

[红色测试](./evidence/2026-10-20-state-plan-guard-red.txt)证明原服务确实在已有计划/查询失败时仍变更状态；修复后最终验证见：

- [后端回归](./evidence/2026-10-20-plan-only-tests.txt)
- [vet](./evidence/2026-10-20-plan-only-vet.txt)、[build](./evidence/2026-10-20-plan-only-build.txt)
- [前端回归](./evidence/2026-10-20-plan-only-frontend.txt)、[类型检查](./evidence/2026-10-20-plan-only-typecheck.txt)、[广场浏览器](./evidence/2026-10-20-plan-only-browser.txt)

历史直接登记页面和HTTP报告已失效为当前功能说明；它们仍记录当时执行过程。当前没有可用的计划创建/修改/取消/执行写接口和页面，本次也没有宣称其已完成。后续补齐原型计划全流程，再接公共新表、主入口和真实事务联调。
