# F3 上下架登记页面切片

> 当前直接登记页面已按用户DEC-010撤下，本文仅保留历史执行事实，见[纠正记录](./2026-10-20-plan-only-correction.md)。

日期：2026-10-20。承接[HTTP记录](./2026-10-20-resource-state-http-results.md)，参考v0.22资源操作的实际平台确认与依据输入。

## 页面交付

资源详情仅当can_edit=true、ever_admitted=true且实际状态为ADMITTED/OFFLINE时显示下架/重新上架登记入口。待测试/待接入及非绑定人员不显示该入口。新增ResourceStateDialog：操作原因、对应区域B端实际操作确认、完成依据，提交失败保留输入，提交期间禁止关闭及重复提交。

POST仅传expected_revision/action/external_confirmed/completion_note/reason；认证身份由后端取得。沿用公共幂等生命周期，503等未知结果重试保持同键；403/409冻结继续提交，保留输入并提示关闭刷新后重新核对，不把旧revision自动替换后强行重试。

成功核对收据对象、目标状态和下一revision，再重读资源安全详情，同时刷新供应来源与广场聚合。不在前端把“上架成功”直接推断成报价可用，也不自动恢复历史客户配置。

## TDD及验证

- [契约红色](./evidence/2026-10-20-resource-state-ui-red.txt)：先写2项测试，缺新模块导致加载失败。
- [前端回归](./evidence/2026-10-20-resource-state-ui-unit.txt)：92项通过。
- [类型检查](./evidence/2026-10-20-resource-state-ui-typecheck.txt)：ExitCode=0，包含最后的来源/广场刷新事件链。
- [构建](./evidence/2026-10-20-resource-state-ui-build.txt)：ExitCode=0，保留既有大包提示；构建后又补充了三层刷新事件链，由最终类型检查及浏览器回归覆盖。
- [浏览器回归](./evidence/2026-10-20-resource-state-ui-browser.txt)：真实Vue广场/来源/详情/表单，HTTP受控替身。覆盖缺确认时不发送请求、下架503重试同键、精确请求字段、成功重读显示重新上架入口、上架409保留完成依据并禁用继续提交，及既有广场流程。首次脚本点击隐藏的ElementPlus原生checkbox失败，改点可见标签后通过。
- [冲突截图](./evidence/2026-10-20-resource-state-ui-conflict.png)：已查看，输入、实际平台确认和冲突提示完整展示。

## 待接入及范围限制

公共生产路由仍未装配新广场入口。浏览器测试使用真实组件与模拟HTTP，不证明真实数据库、日志/公告/outbox及Docker环境已完成。生产构建仍走旧入口，不能单凭vite成功认定新页面已部署。

计划创建/执行、首次接入、内部补测权限和人工直接改价不是本页面切片完成项。当前重新上架最终条件由服务端一致性事实检查；前端允许打开登记不等于服务端承诺可上架。
