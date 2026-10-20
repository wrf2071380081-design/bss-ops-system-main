# F1 标准模型库前端切片执行记录（v0.22）

日期：2026-10-20；分支：`feature/supply`。

## 1. 本轮交付

- A 专用 `src/api/standardModels{,.contract,.types}.ts`，使用新 `/api/internal/standard-models`，不调用旧 SKU 业务接口。
- `src/views/internal/models/StandardModelList.vue`：官方八类资料、筛选/分页/排序、待补齐/核验状态，每行右侧单一维护入口，窄屏横向滚动。
- `StandardModelMaintainDialog.vue`、`StandardFactEditor.vue`、`StandardPriceEditor.vue`：具名字段与价格项编辑、不完整资料保存、缺失项、人工核验、历史只读、操作记录读取、非维护人只读、冲突保留输入和离开保护。
- 价格和版本保持字符串，使用 decimal.js 校验；不将未披露视为不支持，不将零价视为未知。来源链接限制 HTTP(S)，不插入不可信 HTML。
- 复用公共 HTTP/幂等请求机制；读请求在页面销毁时取消，提交期间不能关闭对话框，失败保留输入。

按原型源码的白色资料表、浅灰背景、青绿色操作色及中文系统字体实现，表格不使用固定 colgroup。对话框滚动正文，标题与操作区保持可见。没有调用或改写原型脚本。

## 2. TDD 与验证证据

| 检查 | 结果 | 证据 |
|---|---|---|
| 请求工具初始红色 | 缺少契约模块导致测试加载失败，退出码1；不是业务断言失败 | [初始输出](./evidence/2026-10-20-f1-frontend-red.txt) |
| 响应身份红色 | 响应ID不匹配时未拒绝，`Missing expected exception`，退出码1；随后补正 | [断言输出](./evidence/2026-10-20-f1-frontend-response-red.txt) |
| 契约定向测试 | 5项通过，包括十进制、字段剔除、响应身份/类型和null草稿数组 | [定向输出](./evidence/2026-10-20-f1-frontend-contract-tests.txt) |
| 精度变异 | 允许9位小数后，价格校验用例失败；恢复源码后再回归 | [变异输出](./evidence/2026-10-20-f1-frontend-mutation-precision.txt) |
| `npm test` | 全量79项通过，退出码0 | [回归输出](./evidence/2026-10-20-f1-frontend-regression.txt) |
| `npm run typecheck` | vue-tsc通过，退出码0 | [类型检查](./evidence/2026-10-20-f1-frontend-typecheck.txt) |
| `node node_modules/vite/bin/vite.js build` | 构建通过，保留现有大包警告 | [构建输出](./evidence/2026-10-20-f1-frontend-build.txt) |
| `node tests/standard-model.browser.mjs` | 真实Vue页面的浏览器交互用例通过 | [浏览器输出](./evidence/2026-10-20-f1-frontend-browser.txt) |

首次类型检查发现模板回调隐式any，已修正。浏览器用例覆盖单维护入口、精确零价、409后输入保留/刷新、保存、核验、历史只读、记录、非维护人只读、待补齐保存及390px窄屏滚动。

浏览器测试用 Vite 的测试专用入口挂载真实页面，使用服务响应替身和开发身份，只替换服务边界；没有修改公共生产路由。它不验证真实 AuthN、数据库、日志落库或事务提交，不能视为端到端联调。生产构建当前仍由公共旧路由决定入口；未挂入的新页面由 vue-tsc 和浏览器实际组件加载验证，不以旧路由构建通过冒充新版已部署。

## 3. 截图

下列是本轮真实Vue组件的测试场景截图，未包含公共 AppLayout，也不是原型截图。已查看桌面/窄屏输出；尚未完成整体应用与原型的完整视觉对照。

- [桌面资料表](./evidence/2026-10-20-f1-frontend-desktop.png)
- [维护表单](./evidence/2026-10-20-f1-frontend-maintenance.png)
- [390px窄屏](./evidence/2026-10-20-f1-frontend-mobile.png)

## 4. 接入与剩余验收

1. 公共菜单/路由需装配 A 页面，建议 `/internal/standard-models`；仅内部认证，不沿用旧SKU角色权限点。公共后端入口需装配现有 `RegisterSupplyRoutes` 与真实仓储/事务/日志。
2. 操作记录使用附01的 `/api/internal/operation-logs?entityType=standard_model&entityId=...`；当前展示最近100条。分页、人员名称和最终时间字段仍需与公共日志DTO对齐，不能将测试替身视为公共接口已交付。
3. 当前公司/类型筛选接现有单值F1查询；原型多值选择与供应商/节点关联筛选、模型库级操作记录侧栏及抓取状态仍待相应契约/页面补齐。F2不提供模拟写入入口。
4. 完成真实新增→待补齐→补齐→核验→改价→历史追溯、403/409及日志失败回滚后，再声明 F1 跑通；本轮仅交付独立前端切片。
5. 按最新优先级继续模型广场、资源与缓存报告摘要，以及工作台的 A 子能力；其他 A 原范围不取消。
