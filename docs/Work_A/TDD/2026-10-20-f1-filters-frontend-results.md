# F1 多选筛选前端记录

日期：2026-10-20。延续[F1筛选计划](./2026-10-20-f1-filters-plan.md)。

标准模型库新增公司、类型、供应商、部署节点多选。公司与供应商从独立filter-options接口读取，支持远程名称搜索、失败重试和has_more提示，不由当前模型页构造选项。节点改变后清除旧供应商选择并刷新选项；查询及重置回第一页。模型本身仍无region属性。

新增modelFilters.contract.ts校验及JSON数组编码，保留含逗号公司名和bigint文本ID；空维度不发送，拒绝重复/非法条件。模型类型保持原型枚举。既有单行维护入口及八类资料表不变。

## 验证证据

- [红色](./evidence/2026-10-20-f1-filters-frontend-red.txt)：测试先行，缺契约模块导致加载失败。
- [单元/场景回归](./evidence/2026-10-20-f1-filters-frontend-unit.txt)：90项通过，新增2项。
- [类型检查](./evidence/2026-10-20-f1-filters-frontend-typecheck.txt)：ExitCode=0。
- [构建](./evidence/2026-10-20-f1-filters-frontend-build.txt)：ExitCode=0，保留既有大包提示。
- [浏览器回归](./evidence/2026-10-20-f1-filters-browser.txt)：真实Vue页面，受控HTTP替身，验证选项503重试、含逗号公司双选、类型/节点/供应商组合的JSON请求、重置清空与既有维护/核验/冲突/历史/窄屏流程。
- [多选页面截图](./evidence/2026-10-20-f1-multi-filters.png)：已查看，长选项折叠及提示正常。截图捕获时查询按钮仍处于加载态，不作为真实筛选数据结果证明。

首次全回归出现Node内置amaro WebAssembly解析器异常（model-merge.test.ts未执行成功）；复跑90项通过，未改动该业务或测试。浏览器测试初次存在响应监听注册过晚的竞态，改成点击与预注册等待一起执行后通过。

## 边界

生产公共路由未装配新标准库入口，vite旧入口构建不能证明新页面已部署；新组件通过vue-tsc与浏览器真实挂载验证。真实数据库的同资源供应商/节点EXISTS、计数与分页仍需后续集成测试。远程选项返回前50项，用户可继续按名称搜索，不承诺一次返回全集。公共工作台及Docker新API接入没有在本切片完成。
