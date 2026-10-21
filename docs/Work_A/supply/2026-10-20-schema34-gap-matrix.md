# main 000033/000034 与 A 代码差异清单

基线：2026-10-20拉取的main `422be85`，`000033_target_model.up.sql`及`000034_test_report_cache.up.sql`。本清单按实际DDL核对，不以设计文档中的“已有”代替代码事实。

## 1. 处理原则

- A调整仓储/转换层，不改已确认业务规则来迁就缺列。
- 不修改已发布000033/000034；增量DDL由公共负责人统一编号与交付，本轮不擅自添加000035。
- 未核验数据不能默认VERIFIED；缺revision不能用updated_at或MAX(version_no)冒充；未知操作者不能默认为当前查看人。
- 不覆盖现有A工作区，不提交、不推送，不执行数据库迁移。Git仍处于main待提交合并状态。

## 2. 逐项映射及差距

| 对象 | 最新DDL实际情况 | A当前需要 | 处理与责任 |
|---|---|---|---|
| standard_model 身份资料 | company(64)、name/official_id(128)、model_type及模态数组 | 同名字段，允许资料不完整保存 | A映射同名列；数据库长度需与输入校验统一，不能截断保存。空官方身份的唯一索引需明确草稿策略，否则多个待补齐模型可能冲突 |
| standard_model JSON | capacity/abilities/api_access/lifecycle默认`{}`；price_items/sources默认`[]` | 类型化Fact/Ability/PriceItem/Source列表 | A写JSON编解码；空对象可明确映射待补齐，但非空旧结构必须审查，不能默默丢字段或推断能力。JSONB本身不限制对象/数组，仍需固定协议 |
| 标准模型核验/绑定 | 无maintainer_id、verify_status、checked_by、revision、current version显式列 | 当前维护人、人工核验、CAS及不可变版本 | 公共方补增量结构；created_by只说明创建者，不能覆盖转交后的维护人。checked_at仅日期，不能证明人工核验完成 |
| standard_model_version | standard_model_id、integer version_no、snapshot | 当前指针、不可变版本与revision分离 | A可映射历史snapshot；公共方确认integer范围或升级bigint，补当前版本字段。不从历史最大版本推断并发revision |
| supplier | 无buyer_id/revision；tax_rate numeric(6,2)、status注释ACTIVE/FROZEN | 绑定采购人、CAS、比例字符串8位精度 | 公共方补绑定/revision、确认税率列精度。禁止把0.0625舍入0.06，或猜测既有6是6%还是比例6。旧值需显式清理/迁移策略 |
| supplier_formal_config | scope/base_url/api_key_enc，缺确认/版本/状态/密钥版本 | 已确认版本、停用保护、版本化加密与区域绑定 | 公共方补结构；api_key_enc仅字符串不能假定已满足版本化认证加密。A后续实现精确编解码，不回传密文 |
| resource 接口标识 | supplier_model_name、standard_model_name_snapshot | supplier_model_id接口ID | 不是重命名关系；公共方增加接口ID，B/A明确来源，不用显示名称猜接口ID |
| resource revision/引用 | latest_test_report_id，缺revision、formal_config_id/revision、origin_row/version、target_version | CAS、正式配置版本引用、自身报告适用性 | latest_test_report_id可映射A.LatestReportID；其余公共方补结构/关联，不能凭最新时间或批次通过推断 |
| resource state | varchar默认PENDING_TEST，注释含PENDING_ADMIT/PLAN_OFFLINE/PLAN_ONLINE | PENDING_ACCESS/ADMITTED/OFFLINE，计划不立即改实际状态 | 注释不是CHECK。A适配需明确状态词典；旧计划状态数据必须审查，不能无依据映射当前实际状态 |
| test_report 身份 | resource_id NOT NULL，关联test_job | 草稿阶段行/版本报告、不可变目标和有效性事实 | 公共方/B补申报行/版本与目标关联方案。没有资源ID时不能捏造正式资源来满足NOT NULL |
| test_report 指标 | connection_ms/rps/tps numeric；connection_passed/stress_passed可空 | 精确可空字符串、连通/压力事实 | A仓储扫描精确decimal文本，不经float；NULL结论不是通过。原始报告写入仍归B |
| test_report 缓存 | 000034新增cache_hit_tokens/cache_input_tokens可空bigint；scene在000033 | ReportCacheCounters字符串计数/场景 | **本轮已实现ReportCacheRow映射**。0有效；缺失/负数/分母0/命中大于总数按000034显示NOT_PROVIDED。单条选定报告映射，不借其他报告 |
| plan | resource_id/kind/reason/due_at/before/after/status/created_by/created_at | revision、expected_resource_revision、executed_at、failure_reason、修订、同维度唯一 | 公共方补增量结构；before/after可保存快照，不能拿created_at冒充实际完成时间。DONE/EXECUTED须显式词典与历史补证，不能默认转换完成依据 |
| notice | type/target_type/target_id/payload/created_at | 资源/区域/内容/影响状态及数量/来源事件 | A可用payload明确编码，公共方固定JSON版本及来源事件防重约束。不能从无载荷的目标关联猜影响客户数或当前区域 |
| notice_read | (notice_id,staff_id)主键、read_at | 首次阅读及个人状态 | 已具备基本字段；A可实现仓储，仍需与操作日志/幂等事务共享。缺记录才是未读，查询失败不是未读 |
| operation_log | before/after JSON、operator_id、request_id(64)、reason(500)、created_at | A操作前后安全快照、认证StaffID、追踪及时间 | A可映射OccurredAt→created_at；需统一request/reason长度，当前部分A服务允许128字符追踪，数据库不可截断。原子写入仍需公共工具 |
| outbox/提醒 | 000033/34没有专用出站事件/提醒投递结构 | 资源/公告原子事件、plan revision提醒失效 | 需公共稳定端口及持久化方案，不用操作日志冒充已投递事件或实际完成 |

## 3. 建议接入顺序

进展补充：已在独立测试库验证000033/34脚本、F1缺列/类型及空身份草稿唯一冲突，见[真实库验证记录](../TDD/2026-10-20-schema34-testdb-results.md)。运行库仍32，尚未完成业务仓储、历史数据与事务验收。

1. 公共方统一增量字段与状态/JSON协议，先覆盖F1核验/CAS/绑定和F3身份/正式配置/目标版本，再覆盖计划执行及事件。
2. A先实现有确定字段的转换，逐个仓储接入；当前新增`internal/infra/supplypg/report_cache.go`仅为000034计数映射，不是完整报告或资源仓储。
3. 用独立测试库应用迁移，核对列/约束、历史空值、CAS冲突和事务失败回滚，再装配真实主路由。不能直接把带空依赖的注册器接到运行环境后宣称已完成。
4. 本地运行库尚为32；新代码迁移最高34不代表运行库已升级。数据库应用另行执行，不在本轮发生。

对应TDD证据：[000034适配记录](../TDD/2026-10-20-schema34-results.md)。本清单取代此前“公共新表不存在”的当前判断，但不改变早期报告的历史事实。
