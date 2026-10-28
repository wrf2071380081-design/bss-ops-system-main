# F4 供应商与配置契约

<a id="suppliers"></a>

更新：2026-10-27。供应商档案、曾用名、资质元数据及正式配置已有指定真实场景验收；合并执行、D07/权威审核和真实Inspector仍按各自边界接续。 当前迁移与部署见[交接](./handoff.md)，执行结果只维护[TDD SUMMARY](../TDD/SUMMARY.md)。下文保存业务/接口/存储约束，不再逐阶段重复测试结论。

## 身份、字段与动作边界

新供应商归属采购人为当前创建者，转交走既有公共能力。税率为比例字符串，0.06显示6%；缺失与真实0分开，不参与报价/毛利。档案/正式配置按采购人，资源办理按节点人员，计划执行按原发起人授权，不能统一套buyer_id。

正式Key按DEC-018保存原值，列表、详情、安全快照和日志不回显；留空修改保留旧Key，提供新Key才轮换。旧se1封套要求重录，不自动解密。不得将供应商草稿测试凭据当作平台内部测试凭据。

<a id="supplier-http"></a>

## 主体、列表和正式配置命令

### 1. 主体与责任

主体资料和正式配置的修改由绑定采购人执行，内部读取不套旧角色域。区域仅 CN/OVERSEAS；进入申报、资源或合同使用后锁定。主体 established 由可信办理完成动作派生，资源数量、合同及授信来自各自来源，不能作为普通主体字段随意写入。

税率按 DEC-007 使用比例字符串（`"0.06"` 表示6%），范围0～1，页面显示百分比；它不参与采购成本或毛利。公共人员、使用事实、附件及 schema 接口缺失时，不以客户端布尔值代替服务端事实。

#### 1.1 主体资料切片

可编辑资料为 name、region、registration_no、contact、phone、tax_rate、settlement_currency。name/region/settlement_currency 必填；tax_rate 允许为 `null` 表示未填写，数值字符串 `"0"` 表示已确认零税率，读写和页面展示必须保留二者区别。联系人、电话和登记号允许未补齐，不自造国内手机号或全球统一登记号格式。币种除三位大写语法检查外，服务在事务内核对公共币种目录，不能仅凭客户端文本认定币种存在。

税率为 `null` 或0～1的十进制比例字符串，最多8位小数；接受0和1，拒绝负数、指数、超过精度及大于1，不截断四舍五入。非空领域值统一为8位小数字符串；展示百分数由前端以十进制处理。v36 的 `supplier.tax_rate` 可空，空值以 SQL NULL 保存；不得将历史空值回填为零。

普通编辑要求当前绑定采购人和 expected_revision，不接受新 buyer_id、established、状态、资源数、额度或合同摘要。DEC-008 已确认新建默认绑定当前认证创建者；转交由公共 assignment 处理。

主体改名时，将旧名称写入独立 `supplier_alias` 表并与主体更新、revision 和 `operation_log` 快照同事务提交。曾用名按 trim + 小写规范化，在单一供应商内唯一；不同供应商允许同名别名，列表查询用 `EXISTS` 匹配以免同一主体重复。当前主体名称不作为自己的别名返回；改回旧名时从当前别名集合移除，后续操作快照保留历史变化。`GET /supplier-entities` 与供应商工作台列表均支持名称/曾用名检索。

A 只提交 [供应商曾用名 schema 变更申请](./supply-suppliers.draft.md#supplier-aliases)，由统一维护人审核、分配编号并编写增量迁移；A 曾在本地新建的 000038 迁移已撤回，不占用该编号。无 `supplier_alias` 表时，主体列表/详情、当前名称检索及不改名的主体资料维护可以工作；曾用名读写/检索仍依赖正式结构交付，缺表时改名请求明确失败。候选结构隔离验证不代表正式迁移交付。

区域变化时，在锁定主体的同一事务查询其**历史使用事实**：曾进入申报、资源或合同中任一场景后不可变更，即使相应业务已撤回/结束。禁止用客户端 region_locked 或当前资源数替代该查询。相关创建方须先锁同一主体后写使用关系，避免并发漏查。

主体仅修订当前资料，不新造官方价格版本；真实变化增加revision，无变化保存不增revision但记录操作。所有业务写与日志同事务，失败不返回成功对象；返回值与前后快照互不共享可变对象。新主体 established=false；普通编辑保留原值，后续由 B 可信办理事实驱动专门命令。

#### 1.2 主体 API 接入

目标主数据表与旧结算档案分开。A 的主体端点使用 `/api/internal/supplier-entities`，避免复用已有 `/api/internal/suppliers` legacy 只读接口。

| 方法 | 路径 | 行为 |
|---|---|---|
| GET | `/supplier-entities` | 内部主体列表，支持名称、区域筛选和分页 |
| GET | `/supplier-entities/:id` | 读取目标 supplier 主体与当前采购归属 |
| POST | `/supplier-entities` | 创建主体；buyer_id 只取认证员工，supplier 与初始 assignment_log、CREATE 留痕同事务 |
| PUT | `/supplier-entities/:id` | 当前归属采购人全量修订，要求 expected_revision；不能改归属、established 或状态 |

新主体 `established=false`。版本号由 `operation_log` 中的 supplier 快照承载；当前 000033 原生表没有 revision 列，不新增同名列。内部 GET 全员可读，写入按当前 buyer_id 限制。主体创建本身不触发供应商报价审批。

#### 1.3 供应商工作台事实摘要

- 合作阶段只取可信 `supplier.established`，不按资源数或合同记录推断。
- 合同状态只取该主体的合同资料或 onboarding 中合同节点状态。合同与合同节点均无记录时为“未登记”；不得用 `established` 推断合同已归档，也不得把缺记录默认显示成“办理中”。
- 办理状态基于已接入的未完成申报、onboarding flow 和待执行/执行中/阻塞的内部改价计划事实；不同状态维度独立投影。
- 当前授信按供应商、`SUPPLIER_TO_US` 和结算币种读取最高版本；同一最高版本重复或金额无效时返回冲突/异常状态，不拼接或回退旧 `supplier.credit_amount`。

截至 000036，结算币种没有独立目录表；当前主体写入按现有系统已支持的 CNY/USD 服务端白名单校验。`supplier.tax_rate` 是 numeric(6,2)，端点的比例字符串先转百分比保存；为避免数据库静默舍入，最多支持 0.01 个百分点精度，页面输入百分比并转换为比例字符串。

#### 1.4 供应商详情操作记录补充（2026-10-25，源码完成）

`GET /api/internal/suppliers/:id/operations?page=1&size=20&entity_type=...`的`entity_type`可省略，非空仅接受` supplier / supplier_qualification / supplier_formal_config / contract / credit_line / resource / plan`七类（实际参数不带空格）。按同一只读快照统计、分页，以时间和日志数字ID倒序稳定排列；模型端`/:id/resource-operations`仍仅返回资源/计划。

供应商归属按业务表关联：主体ID、资质/配置的supplier_id、合同/授信的SUPPLIER主体以及资源/计划所属供应商；不从快照推导权限或关联，不因供应商无资源丢失主体日志，不串入同数字ID客户的合同授信。只投影摘要字段，不返回before/after、Key、文件路径或配置正文。无资源的记录显式使用空`resource_id/supplier_model_id`，前端不得构造虚假资源链接。

详情页支持七类筛选、安全摘要弹窗；资源/计划打开具体对象，主体/资质/合同/授信打开相应资料区域。正式配置只显示日志摘要及现有配置维护入口提示，不创建不存在的配置详情路由。新后端查询及非空页面场景待Docker恢复后定向验证，不能视为完整运行验收。

### 2. 正式配置写命令

正式配置领域读写、原生存储、HTTP路由和模型广场配置页面已具备。资源绑定和逐资源准入仍需B的真实办理资格、报告及测试事实。

存储沿用原生 supplier_formal_config 和 operation_log 安全快照，不新增独立密钥版本/状态/revision列。按 DEC-018，现有物理列 `api_key_enc` 暂继续沿用历史名称，但当前写入原始 Key；如需改列名，另按共享迁移规则提出。

| 命令 | 输入 | 规则 |
|---|---|---|
| Create | 认证人员、supplier_id、scope、base_url、api_key、external_confirmed、reason、request_id | 在事务内读取并锁定主体；仅绑定采购；显式外部操作确认；生成配置ID并写 Key 与安全日志 |
| Update | 上述元数据、supplier/config ID、expected_revision、scope、base_url、可选 api_key、external_confirmed | 跨主体404；版本冲突409；省略 Key 保留，空 Key 拒绝；提供非空 Key 作为一次轮换 |
| Disable | 认证人员、supplier/config ID、expected_revision、reason、request_id | 逻辑停用；服务端查询任何关联资源，存在引用时409；不物理删除 |

ID/revision 均以 bigint 字符串对接 API。配置区域取自锁定的供应商，不接受请求自报。正式配置不从 B 的申报测试凭据自动生成。

scope、URL、说明以及新建 Key 必填。URL 必须是无用户密码段、无常见敏感查询参数的 HTTP(S) 地址，并通过可注入环境策略；该策略可在正式环境要求 HTTPS，不在领域里发网络请求。

scope最多255字符、URL最多512字符、Key最多512字符，中文按字符计数；非法UTF-8/NUL和超限由领域入口拒绝，不等到SQL层报500。

每次新增/更新需 `external_confirmed=true`，记录确认人员、时间及对应 revision。配置变化产生新的 revision 和本次显式确认；旧办理/测试引用的 config revision 不再适用。此确认仅表示外部平台操作，不能代替 B 的连通性、压力测试或办理完成证明。

未传 Key 且 scope/URL 不变的成功保存不增 revision，但仍追加 UPDATE_NOOP 日志。重复停用也留痕但不增 revision。幂等重放由公共 HTTP 中间件拦截。

### 3. Key 与安全投影

- 按 DEC-018，正式配置 Key 在现有物理列中明文保存；不依赖 SecretSealer、AES-GCM 或运行密钥环境变量。供应商提交测试 Key 与本正式配置是不同流程，二者来源及用途分开。
- HTTP 页面和 API 仅返回 `has_api_key` 与固定掩码；Key 原文不进入响应、操作日志、错误信息或页面状态。更新省略 Key 表示保留，非空 Key 表示替换。
- 历史 `se1` 加密值不会自动解密或作为有效配置使用；详情标记未设置，采购人需重新录入后才能继续使用。
- URL 策略仍由环境注入，可要求 HTTPS；不在事务锁内执行网络探测。

### 4. 事务与公共接入

FormalConfigUnitOfWork.Within 必须加入现有幂等/办理事务，无外层事务才开启顶层事务。事务端口提供锁主体、分配ID、锁配置、创建/CAS保存、查询资源引用、追加安全日志。日志失败与业务写整体回滚。

当前34映射：supplier_formal_config保存原生主体/范围/URL/Key及审计列，note保持原说明用途；版本、ACTIVE/INACTIVE与确认依据保存在operation_log安全快照，不新增独立列。主体采购人从明确的初始分配/转交记录链读取，不把历史created_by默认当当前归属。配置维护与资源绑定共用supplier锁；真库两个并发提交顺序已验证，B真实节点/主装配仍需联合验收。

正式 HTTP 采用详细设计 `/api/internal/suppliers/:id/formal-configs` 路径及统一包络/幂等，提供 `GET` 列表、`POST` 新建、`PUT /:configId` 更新和 `DELETE /:configId` 逻辑停用；写入要求内部认证和幂等，Key只在请求中出现，响应仅返回固定掩码。绑定正式配置及资源准入完整业务链路仍依赖真实办理/测试提供方。

正式配置、合同、授信、资源、计划五类因没有真实隔离数据尚未验证。供应商测试配置GET实测503，等待Inspector持久提供方及B侧调用/报告事实。

<a id="formal-config"></a>

## 正式配置存储

### 配置与凭据

- scope/base_url、创建/修改人及时间使用原生列；note保留原说明用途，不覆写为控制JSON。
- 依据 DEC-018，当前 `api_key_enc` 列保存供应商提交的原始 Key，不做 AES-GCM 加密；列名沿用共享 000033 schema，后续字段改名需按共享迁移流程处理。Key 限制为非空、有效 UTF-8、无 NUL 且不超过 512 个字符。
- revision、ACTIVE/INACTIVE、区域及确认依据保存在operation_log.after的supply.formal_config/v1安全控制快照，保留动作FORMAL_CONFIG_REVISION。快照只含安全View和 Key 的 SHA-256 一向指纹，不含明文。该指纹用于核对当前列值与版本快照一致，不用于还原 Key。
- 旧 `se1` 密封信封只作兼容识别：读取时标为不可用，要求采购人重新录入；不自动解密、不将密文交给调用方，也不在本切片迁移数据库。
- 控制快照必须追加且保留，不能作为临时日志裁剪。最新损坏快照不回退；未初始化历史配置不默认有效。普通日志展示按业务operation翻译，不展示存储标记或指纹。
- scope最多255字符、URL最多512字符，非法UTF-8/NUL或超限由领域入口返回字段错误；Key按原生varchar(512)字符容量检查。时间在领域写命令中统一到UTC微秒，回执和持久化一致。

### 归属与事务

正式配置写入先锁supplier，归属从该主体的显式assignment_log(entity_type=supplier, field=buyer_id)记录链读取：初始分配old为空，随后old必须接续上一new；new/changed_by须为有效人员引用。不从裸created_by、资源buyer或当前调用人猜测当前采购人。A内部新建供应商仍须由创建路径按DEC-008原子保存“初始归属为认证创建者”的记录；外部注册的归属由可信办理流程建立。旧主体无明确归属记录时拒绝，配置命令不顺带创建归属或转交。

修改按supplier→config锁序，在锁定后以新语句读取当前控制头并核对原生投影、Key指纹、确认依据及expected revision。只增一版；省略Key保留原值。旧密封值必须在修改时显式替换。UPDATE_NOOP/DISABLE_NOOP仅追加普通安全日志，不增版本。日志失败或空日志回调不得返回成功。

停用查询本供应商所有资源的真实A控制快照，任何状态仍引用该配置即阻断；缺/坏资源快照不能当“无引用”。绑定与停用必须共用supplier锁，在同一外层事务检查/写入引用；跨模块锁序及并发联合验收仍需完成。底层资源存储不能作为绕过绑定领域服务的写入口。

所有操作加入调用方sql.Tx，不自行Begin/Commit/Rollback，调用方出错须整体回滚，提交成功后再响应。日志只接收安全DTO；新建/轮换仍要求URL策略通过，但不需要加密器或运行密钥。

### 资源读取

FormalConfigStore.ResourceView可注入ResourceStateStore.FactsLoader的config提供方。同一读取快照中返回配置安全View，跨供应商不泄露；不解密或返回凭据。配置更新后旧资源绑定版本不自动跟随，继续显示FORMAL_CONFIG_CHANGED；停用仍阻断报价。

已绑定配置缺失或不属于该供应商时，资源详情保留并显示FORMAL_CONFIG_UNAVAILABLE，不把资源本身变成404；配置快照/Key指纹损坏及SQL故障继续明确报错，不当作正常空配置。旧 `se1` 配置不作为可用 Key。

### 真实资源绑定

`NewResourceBindingWork(state, configs, resolve, proofLoader)`加入现有ResourceConfigBindingService。A按supplier→resource→formal config→B proof顺序取得锁和同一事务事实；不同schema的存储不能混装。proofLoader提供B当前配置节点指派及不可变来源，不能接受页面布尔声明。

正式配置维护按采购人绑定，资源初次配置关联按节点办理人，两者独立。绑定保存资源CAS与安全日志，保留flow/node/assignee/flow_revision及原资源来源引用；不在资源日志带入配置URL或密钥。重复同版本绑定只追加BIND_NOOP，配置更新后可由当前办理人显式关联新确认版本，不自动跟随。

绑定与停用真实并发的两个提交顺序均已在专用库验证：绑定先提交，停用看到引用并拒绝；停用先提交，后续绑定看到停用/版本变化并拒绝。此证据覆盖A的共同supplier锁，不证明B真实节点/审批或其余跨模块锁序已交付。

<a id="supplier-aliases"></a>

## 曾用名结构与兼容

### 业务目的

供应商改名后可以用旧名称找到同一主体，列表只返回一条该主体记录；曾用名不写入联系人或备注，不改变供应商 ID、归属及历史操作记录。

### A 仓储当前需要的结构

建议独立表 `supplier_alias`，当前 A 仓储按以下名称读取；若统一维护方案不同，需同步映射。

| 字段 | 类型与约束 | 用途 |
|---|---|---|
| id | bigint，identity，主键 | 稳定读取顺序 |
| supplier_id | bigint，非空，引用 supplier.id，删除 RESTRICT | 供应商归属 |
| alias_name | varchar(128)，非空，去除首尾空白且非空串 | 展示与名称检索 |
| normalized_name | varchar(128)，非空 | Go 端 trim + 小写规范化结果，用于主体内去重 |
| created_at | timestamptz，非空，默认当前时间 | 记录时间 |

- 唯一约束为 `(supplier_id, normalized_name)`；不设置跨供应商的名称全局唯一约束。
- 主要访问方式为按 supplier_id、id 读取，以及名称包含查询的 EXISTS 子查询。普通 B-tree 不保证加速 `%keyword%` 检索；当前不额外引入扩展或全文索引。
- 当前名称不出现在自身曾用名集合；改回旧名时删除当前别名集合中的该名称，过去操作快照保持不变。
- 供应商行更新、别名集合更新、revision 快照及操作日志必须在同一事务内提交。
- 不增加 supplier.revision 等同名控制列；revision 继续采用现有 operation_log 快照映射。

### 兼容与发布要求

1. 新表上线时既有主体可从空别名集合开始；本申请不要求从旧日志自动回填曾用名，未回填范围需注明。
2. A 仓储已提供结构缺失兼容：无 `supplier_alias`，或 `id/supplier_id/alias_name/normalized_name/created_at` 缺列、类型不兼容、必填/默认约束不足时，主体列表和详情仍可读取，名称搜索仍匹配当前名称，未变化的主体资料可继续保存；别名数组为空。改名、读取/检索曾用名在结构未就绪时明确不可用，相关写入返回依赖不可用，不会谎报成功。因此正式结构交付仍是开放曾用名功能的前置，但不是部署普通主体读取和不改名维护的前置。
3. 统一维护人交付迁移和编号后，A 核对列名、约束、顺序与仓储映射，在隔离库做定向验证，再进入联合发布流程。
4. 回退由维护人配套说明：先停止依赖新表的代码；已经产生曾用名时先备份，不能无提示丢弃数据；不改写历史 operation_log。

<a id="supplier-qualifications"></a>

## 资质字段、修订及兼容

### 资质元数据读写规则

- 列表 API 在六个增量列均存在且类型/空值约束兼容时，才返回资质类型、有效期、权威审核状态、revision、创建人及 `metadata_supported=true`；旧 v36 缺列或部分结构类型不兼容时保留旧列表/基础上传并返回 `false`，不伪造历史值。
- 增量结构可用时，新记录可提交自由文本资质类型和可空有效期；审核状态不接受上传接口写入，仍由权威审核流程提供。类型枚举未确认前不硬编码类别。
- 上传兼容路径按 JSON 字段是否出现识别元数据请求，显式空值或 `null` 不会回退为基础上传；PostgreSQL 写入事务再次检查结构，六个元数据列齐备后拒绝完全省略元数据的旧请求。缺列/部分交付的旧结构仍允许基础上传。
- 维护 API 通过 `expected_revision` 更新名称、类型和有效期并增加 revision，附件保持不变；要替换或补交文件时新增资质记录，以保留旧附件引用。operation log 与主体写入借用同一事务。
- 上述为源码适配说明，未运行编译、测试、数据库或页面联调。增量写入在缺结构时返回依赖不可用；完整结构启用后，省略元数据会明确返回 400。文件仍使用已存在的 A 本地受控资质适配与 `file_ref` opaque 引用，不表示 D07 已接入；未来按真实 D07 文件契约替换。

### 正式字段与映射

| 逻辑字段 | 用途与规则 |
|---|---|
| `qualification_type` | 资质类别。允许值由产品/实际审核提供方确认后再约束；未确认前不硬编码类别枚举。 |
| `file_id` | D07 受控附件标识。类型、引用形式及外键目标与 D07 提供方对齐；不得复制旧 `file_ref` 或本地路径。 |
| `valid_from` | 有效期起始日期，可空。 |
| `valid_to` | 有效期截止日期，可空；两端都有值时要求 `valid_from <= valid_to`。 |
| `review_status` | 权威审核状态，可空以保留“未记录”；状态值及更新提供方由产品/审核流程确认。文件上传不得自动写成通过。 |
| `revision` | 并发维护使用的正整数修订号，初始为 1；更新采用 expected revision。 |
| `created_by` | 认证内部员工 ID；历史值缺失时保持空，不从上传人或当前查看人推断。 |

继续保留现有 `id/supplier_id/name/file_ref/uploaded_at`，供旧 API 和历史数据兼容。新代码优先使用 `file_id`；不得将历史 `file_ref` 自动转换为 D07 文件、猜测审核状态或补造有效期。资质写入与 operation log 仍按公共事务契约提交；文件元数据和受保护下载由 D07 真实提供方验证。

### 迁移前检查与兼容

1. 在实际目标库核对 schema 版本、列/索引、资质行数、空值分布和 `file_ref` 形态；不能把仓库迁移清单当作运行库版本证明。
2. 保留所有旧行、ID、名称、主体、上传时间和原 `file_ref`；不自动填 `file_id`、类型、有效期、审核状态、revision 以外的历史事实或上传人。
3. D07 尚未确认 ID 类型和文件元数据接口时，不创建猜测类型的外键，不宣称旧文件可通过 D07 下载。
4. 提交包含明确回退方式的迁移；旧 API 与新 API 并存期间继续读取旧字段。

<a id="supplier-merge"></a>

## 供应商合并只读预览

### 范围

该切片提供供应商合并前的只读影响预览，不判定“允许合并”，不执行合并，也不处理客户主体归并。客户归并仍由 C 负责。实际合并需要先确认跨区域、重复资源、在途申报/办理、账号、测试配置、合同授信、资质及客户关系的处置策略，并约定 B/C 的事务端口与授权检查。

### HTTP

`POST /api/internal/supplier-entities/:source_id/merge-preview`

请求：

```json
{"target_supplier_id":"22"}
```

服务端要求来源和目标 ID 均为正 bigint 字符串且不同；未知字段拒绝。路由要求内部认证。接口读取目标双方快照和关联计数，不写入主体、关系、日志或事件。每次预览应重新读取，不缓存或沿用旧结果。

### 响应

响应包含来源/目标主体的 ID、名称、区域、采购归属、结算币种、revision、状态、首次接入标志和时间；双方关联记录计数；相同 `standard_model_id + region` 的重复资源组数；以及需人工复核的条目。revision 和 ID 按字符串传输。

关联摘要覆盖资源、申报及待处理申报、供应商办理、合同、授信、正式配置、资质、资源关联客户、测试凭据引用、网站账号是否配置和曾用名。凭据只给是否存在/引用数量，不读取或返回凭据本身；账号只返回是否配置，不返回账号或密码；附件只计数，不返回 file_ref、文件路径或存储键。

`review_items` 说明区域不同、采购归属不同、首次接入历史、主体状态不同、重复资源、待处理申报/办理、正式配置、测试凭据、网站账号、合同授信、资质及客户关系等人工决策点。此清单是事实提示，不代表策略结论。

当前运行库没有 `supplier_alias` 时，`aliases` 返回 `null` 并包含 `ALIAS_STRUCTURE_UNAVAILABLE` 提示；不把结构缺失显示成零条曾用名。正式结构交付后返回真实计数。其他必须查询的原生关系表不可用时，接口应失败，不得静默用零值替代。

查询在同一个只读 `REPEATABLE READ` 快照内完成，主体资料复用现有供应商读取仓储；重复资源按相同标准模型和区域计数。

### UI 与后续执行边界

供应商主体列表提供“合并预览”入口。操作者选择保留的目标主体后，可以查看双方资料版本、关联数量和人工复核项。页面明确预览不会执行合并，当前没有“确认合并”按钮。预览不设置 `can_merge` 布尔值，前端不得据此授权后续写操作。

执行合并是独立后续切片。必须由服务端重新读取并锁定两主体、校验双方 revision/授权/关系，再在明确的共享事务内迁移允许迁移的引用并写审计。冲突策略或跨模块事务任一未确认时，不提供执行入口。

<a id="test-config"></a>

## 供应商与平台测试配置隔离

### 责任边界

- A 负责 F4 供应商配置入口、安全状态展示，以及与 Inspector 持久配置能力的后端适配。
- Inspector 是测试配置持久化/关联能力的目标方；B 负责测试任务、Inspector 调用、任务目标版本和报告事实。
- 内部平台测试使用平台自己的配置与额度，不读取供应商配置，不把平台 Key 发送到供应商提供的 URL。
- 供应商 Key 不应由每个申报行或资源重复保存。现有 `submission_row` / `resource` 测试字段不等价于一次保存的供应商级配置，不可直接当共享存储。

### 目标交互

供应商提交或维护测试 URL 与 Key 一次；保存成功后页面只显示 URL、配置状态和 `has_api_key`，不回显 Key。Key 留空表示保留 Inspector 已保存的值；首次保存必须提供 Key。显式替换 Key 时更新配置版本。重开页面和后续测试不要求再次输入。

配置页面/API只保留 Inspector 稳定配置引用或等价关联标识，不复制 Key 到 BSS 本地表。需要 Key 的后台测试由 Inspector 已保存配置完成；若 Inspector 接口必须由 BSS传入 Key，则该明文只通过既有受控服务端调用传递，不能经过普通前端读取接口。

Base URL 不得在 userinfo、fragment 或常见凭据型 query 参数中携带 Key/token/secret/password/authorization/credential/signature；A 保存前拒绝此类 URL，读取 Inspector 返回的配置时也拒绝，以防安全 DTO 虽不含单独 Key 字段、URL 却间接回显凭据。普通业务 query 参数可按具体 Inspector 接口策略使用。

### 待 Inspector/B 确认的接口事实

当前仓库尚未发现可用于供应商级持久配置的 Inspector API 或配置引用字段，因此本契约不虚构 URL、请求字段或认证方式。A 侧已有 fail-closed 适配端口及 API/UI 壳层，但不构成真实 Inspector 接入。接入前需由实际提供方确认：

1. 新建/更新供应商测试配置的接口、认证方式和稳定配置 ID。
2. 一次配置如何与供应商主体关联，如何区分供应商自测与平台内部测试。
3. 留空 Key 是否沿用、轮换如何更新版本、停用如何解除关联。
4. 测试任务如何指定已保存配置 ID、目标模型版本及测试类型。
5. 测试报告如何回传来源类型、配置版本和目标版本，避免旧报告覆盖新目标。
6. Inspector 的 URL/网络访问限制由谁维护；供应商 URL 不应使 BSS API 任意代发内部网络请求。

若 Inspector 不能保存/关联供应商配置，先由产品方确认唯一持久化所有者与必要结构，再补结构申请。未得到确认前，不新增 BSS 凭据表、不把正式生产配置或申报行字段复用为共享测试配置，也不接入假保存端点。

### A 侧当前源码状态（2026-10-25）

A 已加入 `SupplierTestConfigProvider` 插槽、输入校验与安全响应 DTO、`GET/PUT /api/internal/supplier-entities/:id/test-config`，以及供应商“档案与办理”页的配置卡片。页面只读回 URL、`has_api_key`、外部引用（若有）和 revision；Key 只作为保存请求字段短暂传入，留空表示让 Inspector 保留已有值。服务端不将 Key 写入 BSS、日志或错误；未知 provider 错误统一映射为不可用。运行主装配尚未注入真实 Inspector provider，所以目前 GET/PUT 会返回依赖不可用，页面明确显示未接入，不显示模拟配置或成功状态。供应商归属授权、持久化、轮换和 B 调用仍须真实接口提供方实现并联合验收。

### API 适配建议

内部页面可通过 A 的供应商主体接口读取安全状态并提交配置变更；A 后端负责调用 Inspector 配置 API。浏览器响应只包含供应商主体 ID、`configured`、`has_api_key`、可展示 URL、稳定外部配置引用（若 Inspector提供）和配置 revision，不包含原始 Key。

B 的测试服务使用 Inspector 配置引用启动供应商自测，并在任务/报告中保留供应商配置 revision、目标模型版本和测试类型。供应商自测通过不自动代表该供应商所有资源通过；每个资源仍需自身符合准入规则。内部测试仍使用平台配置。

正式曾用名/资质元数据已由v45落地，以上字段描述不再是等待迁移的申请。受控附件的持久路径和清扫规则见[部署说明](./f4-f12-file-persistence-deployment.md)。main账户设置来自团队提交，代码已合并并做fixture回归；本次合并构建尚未更新运行API。
