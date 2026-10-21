# 工作台 A 前端子组件执行记录

日期：2026-10-20。依据：[前端计划](./2026-10-20-workbench-a-frontend-plan.md)、[公告与计划契约](../supply/supply-workbench.draft.md)。后端独立切片提交为 f1c6965。

## 已交付

- 资源详情新增“公告”页签，提供当前账号阅读状态筛选、分页、加载失败重试及独立详情列。
- `NoticeDetailDialog.vue` 按原型公告卡片展示标题、资源、区域、时间、原因、影响范围、阅读人员、首次阅读时间和内容。正文使用文本插值，不执行公告HTML。
- 打开详情不改变阅读状态；可写权限由服务器返回，仅显式点击“标记已读”发送空对象，操作者不进入请求body/query。网络/服务失败保留幂等键重试，成功回执核对公告及人员后更新状态。
- 他人投影和显式 readOnly 均禁止代为已读；影响范围待计算与已计算0客户区分，数量使用整数文本保留大数精度。
- `PlanSummaryDialog.vue` 提供可复用的只读摘要，显示原发起人、原因、预计/实际时间、状态及计划依据/当前资源版本。未来预计时间正常展示，版本不符给出提醒，当前无执行写入按钮。
- API及运行时响应校验位于 `src/api/supplyWorkbench*.ts`。公共工作台可直接复用详情组件，当前没有改写公共工作台主页面、四状态聚合、人员选择和生产路由。

## 验证

| 检查 | 结果 | 证据 |
|---|---|---|
| 契约红色 | 新增契约文件尚未实现，模块加载失败；不是业务断言失败 | [红色输出](./evidence/2026-10-20-workbench-a-frontend-red.txt) |
| 前端全部单元/场景测试 | 88项通过，本切片新增5项 | [回归输出](./evidence/2026-10-20-workbench-a-frontend-unit.txt) |
| vue-tsc --noEmit | ExitCode=0 | [类型检查](./evidence/2026-10-20-workbench-a-frontend-typecheck.txt) |
| vite build | ExitCode=0，保留既有大包提示 | [构建](./evidence/2026-10-20-workbench-a-frontend-build.txt) |
| 公告/计划浏览器流程 | 全部通过 | [浏览器输出](./evidence/2026-10-20-workbench-a-frontend-browser.txt) |
| 模型广场浏览器回归 | 原流程及资源详情公告页签空态通过 | [广场回归](./evidence/2026-10-20-workbench-a-resource-browser.txt) |

浏览器脚本 `tests/workbench-a.browser.mjs` 挂载真实Vue组件，在Edge无头浏览器运行，HTTP使用受控替身。覆盖列表/详情/计划503重试、GET不写、未知/零影响、本人标已读503后同键重试、请求无阅读者字段、回执首次时间、他人只读、未来计划版本提醒、筛选资源/区域和窄屏。`resource-plaza.browser.mjs` 另外覆盖真实资源详情公告页签挂载。

已查看他人公告和计划窄屏截图，公告卡片和计划窄屏内容无横向裁切；窄屏长详情通过页面滚动查看：

- [本人未读详情](./evidence/2026-10-20-workbench-notice-own.png)
- [首次阅读回执](./evidence/2026-10-20-workbench-notice-read.png)
- [他人只读详情](./evidence/2026-10-20-workbench-notice-other.png)
- [计划版本提醒](./evidence/2026-10-20-workbench-plan-readonly.png)
- [计划窄屏](./evidence/2026-10-20-workbench-plan-mobile.png)

## 未完成的接入

浏览器测试页标题及打开示例详情按钮仅属于测试夹具，不是生产工作台。生产公共菜单尚未引用这些新入口，因此旧生产入口的vite构建通过不能证明新组件已部署；新组件由vue-tsc及真实挂载浏览器流程验证。

公共工作台需以真实人员目录调用阅读状态接口，再传入选中人员；A真实新表仓储、公共日志/事务/认证装配仍待联调。计划详情目前只接受已知ID，不伪造计划列表或执行能力。公告发布、影响客户明细、计划执行完整生命周期及统一工作台四状态仍未完成。当前本机Docker API及5173运行入口不代表已运行本切片。

原型对齐依据为静态读取v0.22的 opNoticeCard、zTaskDetail、vPlanForm；未声称原始HTML已被浏览器视觉验收。已读行为采用单独接口，查看他人不临时切换登录身份。
