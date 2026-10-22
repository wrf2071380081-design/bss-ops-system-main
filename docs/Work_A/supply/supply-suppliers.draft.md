# F4 供应商与正式配置契约草案

依据：A 详细设计 §5/8.2、原型最终供应商详情（1053行）及正式配置操作（968—971行）。以函数的最终覆盖版本为准。

## 1. 主体与责任

主体资料和正式配置的修改由绑定采购人执行，内部读取不套旧角色域。区域仅 CN/OVERSEAS；进入申报、资源或合同使用后锁定。主体 established 由可信办理完成动作派生，资源数量、合同及授信来自各自来源，不能作为普通主体字段随意写入。

税率按 DEC-007 使用比例字符串（`"0.06"` 表示6%），范围0～1，页面显示百分比；它不参与采购成本或毛利。公共人员、使用事实、附件、加密及 schema 接口缺失时，不以客户端布尔值代替服务端事实。

### 1.1 主体资料切片

可编辑资料为 name、region、registration_no、contact、phone、tax_rate、settlement_currency。name/region/tax_rate/settlement_currency 必填；联系人、电话和登记号允许未补齐，不自造国内手机号或全球统一登记号格式。币种除三位大写语法检查外，服务在事务内核对公共币种目录，不能仅凭客户端文本认定币种存在。

税率为0～1的十进制字符串，最多8位小数；接受0和1，拒绝负数、指数、超过精度及大于1，不截断四舍五入。领域统一为8位小数字符串；展示百分数由前端以十进制处理。

普通编辑要求当前绑定采购人和 expected_revision，不接受新 buyer_id、established、状态、资源数、额度或合同摘要。DEC-008 已确认新建默认绑定当前认证创建者；转交由公共 assignment 处理。

区域变化时，在锁定主体的同一事务查询其**历史使用事实**：曾进入申报、资源或合同中任一场景后不可变更，即使相应业务已撤回/结束。禁止用客户端 region_locked 或当前资源数替代该查询。相关创建方须先锁同一主体后写使用关系，避免并发漏查。

主体仅修订当前资料，不新造官方价格版本；真实变化增加revision，无变化保存不增revision但记录操作。所有业务写与日志同事务，失败不返回成功对象；返回值与前后快照互不共享可变对象。新主体 established=false；普通编辑保留原值，后续由 B 可信办理事实驱动专门命令。

### 1.2 主体 API 接入

目标主数据表与旧结算档案分开。A 的主体端点使用 `/api/internal/supplier-entities`，避免复用已有 `/api/internal/suppliers` legacy 只读接口。

| 方法 | 路径 | 行为 |
|---|---|---|
| GET | `/supplier-entities` | 内部主体列表，支持名称、区域筛选和分页 |
| GET | `/supplier-entities/:id` | 读取目标 supplier 主体与当前采购归属 |
| POST | `/supplier-entities` | 创建主体；buyer_id 只取认证员工，supplier 与初始 assignment_log、CREATE 留痕同事务 |
| PUT | `/supplier-entities/:id` | 当前归属采购人全量修订，要求 expected_revision；不能改归属、established 或状态 |

新主体 `established=false`。版本号由 `operation_log` 中的 supplier 快照承载；当前 000033 原生表没有 revision 列，不新增同名列。内部 GET 全员可读，写入按当前 buyer_id 限制。主体创建本身不触发供应商报价审批。

截至 000036，结算币种没有独立目录表；当前主体写入按现有系统已支持的 CNY/USD 服务端白名单校验。`supplier.tax_rate` 是 numeric(6,2)，端点的比例字符串先转百分比保存；为避免数据库静默舍入，最多支持 0.01 个百分点精度，页面输入百分比并转换为比例字符串。

## 2. 正式配置写命令

正式配置领域读写、原生存储、HTTP路由和模型广场配置页面已具备。正式配置列表及 CREATE/UPDATE/DISABLE 已在本地 v36 隔离库完成真实页面/API 联调（见 TDD Stage 63）。运行装配需要单独配置密钥；本地 API 已启用本地密钥，生产密钥仍由部署方管理。资源绑定和逐资源准入仍需 B 的真实办理资格、报告及测试事实。

存储沿用原生 supplier_formal_config 和 operation_log 安全快照，不新增独立密钥版本/状态/revision列。本节早期提出的额外列建议由密封信封与保留安全快照取代。

| 命令 | 输入 | 规则 |
|---|---|---|
| Create | 认证人员、supplier_id、scope、base_url、api_key、external_confirmed、reason、request_id | 在事务内读取并锁定主体；仅绑定采购；显式外部操作确认；生成配置ID并写加密配置和安全日志 |
| Update | 上述元数据、supplier/config ID、expected_revision、scope、base_url、可选 api_key、external_confirmed | 跨主体404；版本冲突409；省略 Key 保留，空 Key 拒绝；提供非空 Key 作为一次轮换，不将明文与密文比较 |
| Disable | 认证人员、supplier/config ID、expected_revision、reason、request_id | 逻辑停用；服务端查询任何关联资源，存在引用时409；不物理删除 |

ID/revision 均以 bigint 字符串对接 API。配置区域取自锁定的供应商，不接受请求自报。正式配置不从 B 的申报测试凭据自动生成。

scope、URL、说明以及新建 Key 必填。URL 必须是无用户密码段、无常见敏感查询参数的 HTTP(S) 地址，并通过可注入环境策略；该策略可在正式环境要求 HTTPS，不在领域里发网络请求。

scope最多255字符、URL最多512字符，中文按字符计数；非法UTF-8/NUL和超限由领域入口拒绝，不等到SQL层报500。凭据密封后必须符合原生api_key_enc容量，不能截断。

每次新增/更新需 `external_confirmed=true`，记录确认人员、时间及对应 revision。配置变化产生新的 revision 和本次显式确认；旧办理/测试引用的 config revision 不再适用。此确认仅表示外部平台操作，不能代替 B 的连通性、压力测试或办理完成证明。

未传 Key 且 scope/URL 不变的成功保存不增 revision，但仍追加 UPDATE_NOOP 日志。重复停用也留痕但不增 revision。幂等重放由公共 HTTP 中间件拦截。

## 3. 密钥与安全投影

- 领域通过 SecretSealer 接口请求具备密钥版本的认证加密；生产实现由公共能力提供。缺加密器时拒绝新建/轮换；缺 URL 策略时拒绝新增/修改。不重新加密的修改/停用可以保留原密文，不提供明文落库替代。
- 每次加密绑定供应商ID和配置ID作为认证附加数据上下文，防止密文跨配置交换；公共实现必须实际使用该上下文。
- 持久化信封包含密文和版本号，不作为 HTTP/日志对象。服务返回独立安全投影，仅含 has_api_key 和固定掩码，不含密文、密钥版本或明文。
- 日志前后快照使用安全投影；加密错误统一转换为依赖不可用，不将适配器可能包含秘密的错误透出。

## 4. 事务与公共接入

FormalConfigUnitOfWork.Within 必须加入现有幂等/办理事务，无外层事务才开启顶层事务。事务端口提供锁主体、分配ID、锁配置、创建/CAS保存、查询资源引用、追加安全日志。日志失败与业务写整体回滚。

当前34映射：supplier_formal_config保存原生主体/范围/URL/密封信封及审计列，note保持原说明用途；版本、ACTIVE/INACTIVE与确认依据保存在operation_log安全快照，不新增独立列。主体采购人从明确的初始分配/转交记录链读取，不把历史created_by默认当当前归属。配置维护与资源绑定共用supplier锁；真库两个并发提交顺序已验证，B真实节点/主装配仍需联合验收。

正式 HTTP 采用详细设计 `/api/internal/suppliers/:id/formal-configs` 路径及统一包络/幂等，提供 `GET` 列表、`POST` 新建、`PUT /:configId` 更新和 `DELETE /:configId` 逻辑停用；写入要求内部认证和幂等，Key只在请求中出现，响应仅返回固定掩码。主体资料 CRUD 页面和真库联调见 TDD Stage 64；正式配置保存/安全回显见 Stage 63。网站账户/资质附件、绑定正式配置及资源准入的完整业务链路仍待接入。
