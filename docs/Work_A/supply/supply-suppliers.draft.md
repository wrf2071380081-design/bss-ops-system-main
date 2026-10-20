# F4 供应商与正式配置契约草案

依据：A 详细设计 §5/8.2、原型最终供应商详情（1053行）及正式配置操作（968—971行）。以函数的最终覆盖版本为准。

## 1. 主体与责任

主体资料和正式配置的修改由绑定采购人执行，内部读取不套旧角色域。区域仅 CN/OVERSEAS；进入申报、资源或合同使用后锁定。主体 established 由可信办理完成动作派生，资源数量、合同及授信来自各自来源，不能作为普通主体字段随意写入。

税率按 DEC-007 使用比例字符串（`"0.06"` 表示6%），范围0～1，页面显示百分比；它不参与采购成本或毛利。公共人员、使用事实、附件、加密及 schema 接口缺失时，不以客户端布尔值代替服务端事实。

## 2. 正式配置写命令

本切片先提供独立领域服务，HTTP 及真实存储后续装配。

| 命令 | 输入 | 规则 |
|---|---|---|
| Create | 认证人员、supplier_id、scope、base_url、api_key、external_confirmed、reason、request_id | 在事务内读取并锁定主体；仅绑定采购；显式外部操作确认；生成配置ID并写加密配置和安全日志 |
| Update | 上述元数据、supplier/config ID、expected_revision、scope、base_url、可选 api_key、external_confirmed | 跨主体404；版本冲突409；省略 Key 保留，空 Key 拒绝；提供非空 Key 作为一次轮换，不将明文与密文比较 |
| Disable | 认证人员、supplier/config ID、expected_revision、reason、request_id | 逻辑停用；服务端查询任何关联资源，存在引用时409；不物理删除 |

ID/revision 均以 bigint 字符串对接 API。配置区域取自锁定的供应商，不接受请求自报。正式配置不从 B 的申报测试凭据自动生成。

scope、URL、说明以及新建 Key 必填。URL 必须是无用户密码段、无常见敏感查询参数的 HTTP(S) 地址，并通过可注入环境策略；该策略可在正式环境要求 HTTPS，不在领域里发网络请求。

每次新增/更新需 `external_confirmed=true`，记录确认人员、时间及对应 revision。配置变化产生新的 revision 和本次显式确认；旧办理/测试引用的 config revision 不再适用。此确认仅表示外部平台操作，不能代替 B 的连通性、压力测试或办理完成证明。

未传 Key 且 scope/URL 不变的成功保存不增 revision，但仍追加 UPDATE_NOOP 日志。重复停用也留痕但不增 revision。幂等重放由公共 HTTP 中间件拦截。

## 3. 密钥与安全投影

- 领域通过 SecretSealer 接口请求具备密钥版本的认证加密；生产实现由公共能力提供。缺加密器时拒绝新建/轮换；缺 URL 策略时拒绝新增/修改。不重新加密的修改/停用可以保留原密文，不提供明文落库替代。
- 每次加密绑定供应商ID和配置ID作为认证附加数据上下文，防止密文跨配置交换；公共实现必须实际使用该上下文。
- 持久化信封包含密文和版本号，不作为 HTTP/日志对象。服务返回独立安全投影，仅含 has_api_key 和固定掩码，不含密文、密钥版本或明文。
- 日志前后快照使用安全投影；加密错误统一转换为依赖不可用，不将适配器可能包含秘密的错误透出。

## 4. 事务与公共接入

FormalConfigUnitOfWork.Within 必须加入现有幂等/办理事务，无外层事务才开启顶层事务。事务端口提供锁主体、分配ID、锁配置、创建/CAS保存、查询资源引用、追加安全日志。日志失败与业务写整体回滚。

目标 supplier_formal_config 需 supplier_id、scope、base_url、api_key_enc、secret_key_version、status、revision、确认依据、时间及人员；逻辑状态 ACTIVE/INACTIVE 待与公共DDL核对。关联写入方应遵循同一主体/配置锁顺序，防止“查询无引用后并发绑定”绕过停用门槛；单元替身不能证明数据库并发安全。

正式 HTTP 仍采用详细设计 `/api/internal/suppliers/:id/formal-configs` 路径及统一包络/幂等；本切片没有新增可运行路由。主体 CRUD、网站账户、资质附件及正式配置读取/HTTP/真库验收继续保留在 F4 范围内。
