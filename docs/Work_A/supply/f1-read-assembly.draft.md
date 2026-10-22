# F1 读取能力组合与 HTTP 接入约定

> 当前已按[33–36原生结构](./2026-10-21-schema34-baseline.md)适配；下文“公共增量结构先交付”的前提撤销。生产仍需真实日志/认证/幂等装配、历史数据检查和运行库/容器核验，不等于构造仓储就部署完成。

2026-10-21，开发者 A 草案。沿用现有 ModelReader、FilterOptionsStore 和 RegisterSupplyRoutes，不新增公共主路由。

## 组合入口

公共增量结构和数据检查满足后，使用：

```go
repository, err := supplypg.NewModelRepository("public", supplypg.ModelReadTransactions(db))
if err != nil { return err }
reader := supply.NewModelReader(repository)
// 由公共装配把 reader 注入 SupplyDeps.Reader。
```

ModelRepository 同时满足 ModelReadStore 和 ModelFilterOptionsStore，避免列表可用而筛选选项缺失。二者使用相同 schema 及事务运行器，每次仓储调用在独立一致快照内完成；借用公共事务时仍须保证同等隔离，不独立提交写事务。

筛选选项的旧 `NewFilterOptionsStore(db)` 入口保留 public 语义及只读事务，以兼容资源读取能力；组合入口复用同一查询构造与解析逻辑，不复制一套筛选业务规则。任何SQL失败均返回错误，不转换为空选项成功。

## 读取 HTTP 验收

GET 列表、filter-options、详情、versions 均经既有内部人员身份检查。普通内部人员可读取，can_edit 按真实认证主体与当前维护人计算；历史版本始终只读。切换工作台筛选人员或添加 staff_id 参数不改变操作者。

测试可注入认证上下文，但这不证明真实 JWT 登录或生产主装配已完成。HTTP读取不调用写服务，不自动核验、创建版本或添加操作记录；缺少增量结构导致SQL失败时，必须返回安全的错误，不能显示伪造数据或空列表成功。

此组合只处理读取。写入仍需公共日志、认证及幂等外层事务正确装配并在提交后发送成功响应；不得因为读取验证通过就启用尚未验证的写入口。
