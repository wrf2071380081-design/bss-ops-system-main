# F13 资源详情计划列表前端

日期：2026-10-20。承接[列表后端](./2026-10-20-plan-list-results.md)，遵循DEC-010：先计划，再由原发起人登记完成。

## 交付

ResourceDetailDrawer增加懒加载“计划”页签，ResourcePlansPanel按resource_id/region分页查询，展示类型、原发起人、预计时间、状态和当前资源版本前提。状态筛选回第一页；业务跳转集中“详情”列，复用PlanSummaryDialog。

列表请求失败展示错误和重试，不当作暂无计划；加载时清除上一范围结果，切换对象或卸载中止请求。只读详情保留未来预计时间和版本变化提醒，未新增直接上下架、代办或自动到期执行按钮。

新增planList.contract.ts校验查询和响应：canonical资源ID、区域/状态枚举、分页范围、返回resource/region/status范围、唯一计划、total及页码一致性。结果复用PlanSummary校验并返回副本；不从当前资源buyer推断计划发起人。

## TDD与验证

延续中断前的2项先行测试，运行并记录[缺模块编译红色](./evidence/2026-10-20-plan-list-ui-red.txt)，随后实现。

- [前端全部回归](./evidence/2026-10-20-plan-list-ui-unit.txt)：92项通过。
- [类型检查](./evidence/2026-10-20-plan-list-ui-typecheck.txt)、[构建](./evidence/2026-10-20-plan-list-ui-build.txt)：ExitCode=0，构建保留既有大包提示。
- [浏览器回归](./evidence/2026-10-20-plan-list-ui-browser.txt)：真实Vue资源详情挂载计划面板，受控HTTP；覆盖列表503重试、资源/区域范围、发起人展示、计划详情版本提醒、无登记完成按钮、状态筛选及空态，另含原广场流程。
- [详情截图](./evidence/2026-10-20-plan-list-ui-details.png)：已查看，原发起人、原因、预计时间和资源版本提醒正常展示。

## 边界及后续

测试API为替身，没有真实SQL/登录/事务端到端验收。公共生产路由未装配新广场，vite旧入口构建不证明新组件已部署；新组件通过类型检查及真实浏览器挂载验证。

本阶段只完成列表和只读详情。计划创建/修改/取消/执行页面仍需继续；执行HTTP仍待公共提交响应机制支持。真实数据库、公共主入口和Docker联调未完成。按用户最新要求，修改保留工作区，不执行git commit或push。
