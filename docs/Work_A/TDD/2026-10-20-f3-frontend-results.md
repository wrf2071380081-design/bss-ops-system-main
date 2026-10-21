# F3 模型广场前端执行记录（v0.22）

日期：2026-10-20；分支：`feature/supply`。

## 1. 本轮交付

- `src/views/internal/resources/ModelPlaza.vue`：模型聚合目录、名称搜索、公司远程补全、模型类型/部署节点/供应商/状态筛选、排序及模型分页。
- `ModelSourcesDialog.vue`：从独立详情列进入供应来源，保留已应用筛选，单独资源分页与指标排序；名称和整行不另设重复跳转。
- `ResourceDetailDrawer.vue`：资源身份、接口模型ID、系数、配置引用版本、报价阻断项、官方价格及当前报告摘要；报告场景与原始Token计数。
- `src/api/resources{,.contract,.types}.ts`：A专用请求与响应校验；分组/资源两种分页语义；字符串ID/版本/系数/指标，缓存直接按原始整数计算百分数，真实零与缺失分开。
- 新增远程选项端口 `ResourceFilterOptionsStore` 及 GET `/api/internal/resources/filter-options`。每类最多50个匹配项，has_more明确提示继续搜索；从完整资源关联查询，不从当前页构造供应商/公司全集。

页面沿用原型浅色资料表、青绿色操作色和明确详情列。首次浏览器脚本点击ElSelect内的只读input被占位层遮挡，随后改为点击实际可交互的选择框外层；未使用强制点击。截图禁用过渡动画，避免截到标签进入动画的中间帧。

## 2. TDD与检查

| 检查 | 结果 | 证据 |
|---|---|---|
| 前端初始红色 | 缺契约模块的测试加载失败，退出码1 | [输出](./evidence/2026-10-20-f3-frontend-red.txt) |
| 选项端口初始红色 | 缺Go类型/端口的编译失败，退出码1 | [输出](./evidence/2026-10-20-f3-options-red.txt) |
| 前端定向契约 | 4项通过，包含筛选继承、精度、缺失状态和报告归属 | [输出](./evidence/2026-10-20-f3-frontend-contract-tests.txt) |
| 前端区域过滤变异 | 临时遗漏region后，展开查询断言失败；随后恢复 | [输出](./evidence/2026-10-20-f3-frontend-mutation-filter.txt) |
| 后端选项区域变异 | 临时取消供应商选项区域校验后，错误适配未被拒绝，用例捕获；随后恢复 | [输出](./evidence/2026-10-20-f3-options-mutation-region.txt) |
| `npm test` | 83项通过，退出码0 | [回归](./evidence/2026-10-20-f3-frontend-regression.txt) |
| `npm run typecheck` | 通过，退出码0 | [类型检查](./evidence/2026-10-20-f3-frontend-typecheck.txt) |
| `node node_modules/vite/bin/vite.js build` | 通过；现有大包警告仍在 | [构建](./evidence/2026-10-20-f3-frontend-build.txt) |
| `node tests/resource-plaza.browser.mjs` | 真实Vue页面交互通过 | [浏览器](./evidence/2026-10-20-f3-frontend-browser.txt) |
| Go领域/API回归 | 领域49个、API27个顶层测试组通过，退出码0 | [回归](./evidence/2026-10-20-f3-options-regression.txt) |
| Go vet / build | 均退出码0 | [vet](./evidence/2026-10-20-f3-options-vet.txt)、[build](./evidence/2026-10-20-f3-options-build.txt) |

初始红色是缺实现，不混称业务断言失败；两项变异有真实业务断言失败。首次类型检查的模板回调隐式any已补显式类型。

浏览器覆盖模型计数/区间、详情列导航、来源/详情读取失败重试、0.00%与12.34%、超安全整数TPS字符串、报告计数及场景、按供应商名称远程选择、区域/供应商范围继承和390px窄屏滚动。测试入口只挂载真实A组件，服务使用响应替身；没有改变公共生产路由，不证明真实认证、SQL或B接口已联调。新组件由类型检查和浏览器实际加载验证，现有主应用构建不代表新入口已部署。

## 3. 页面截图

以下是本轮Vue组件的测试场景，未包含公共AppLayout，也不是原型截图。已查看桌面、来源表和报告截图；完整应用与原型的视觉对照仍待公共装配后验收。

- [广场桌面](./evidence/2026-10-20-f3-frontend-plaza-desktop.png)
- [供应来源](./evidence/2026-10-20-f3-frontend-sources.png)
- [资源报告摘要](./evidence/2026-10-20-f3-frontend-report.png)
- [390px窄屏](./evidence/2026-10-20-f3-frontend-plaza-mobile.png)

## 4. 接入与剩余范围

1. 公共路由建议 `/internal/resources`，菜单“模型广场”，组件ModelPlaza.vue；与标准模型库分为两个入口，按内部认证读取，不套旧SKU角色权限点。
2. 真实资源/选项SQL仓储由A在公共目标DDL就绪后补齐；完整模型分页、过滤、数字排序及NULLS LAST仍需真库证明。选项端口缺失时503，页面显示真实失败和重试。
3. 公共顶部区域切换、多值筛选、采购人显示名及统一操作记录DTO仍需对齐。当前界面采用已冻结的单值查询和资源安全投影。
4. 本轮是读取界面。资源编辑、正式配置绑定、接入/上下架、历史报告及计划/日志完整面板、B测试/办理适配仍需后续实现；此处没有状态写入成功的证明。
5. 工作台A公告/计划子能力、完整F1/F3/F4及其他原范围继续推进，不因页面测试通过而声明整体目标完成。
