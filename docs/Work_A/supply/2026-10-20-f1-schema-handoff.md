# F1 增量结构交接需求（待公共负责人实施）

依据main422be85迁移000033/34、F1既定业务决策。本文不是已应用迁移，不占用公共迁移编号，不修改已发布脚本。

## 1. 首个真实闭环需要的字段

| 表 | 字段/约束 | 业务含义 |
|---|---|---|
| standard_model | maintainer_id bigint → internal_staff.id | 当前维护人；新建取认证创建者，转交后不等同created_by |
| standard_model | revision bigint，正数 | 所有写入CAS，更新WHERE id/revision，0行即冲突 |
| standard_model | version_no bigint，正数 | 当前不可变业务快照版本，与revision显式区分，不靠MAX推断 |
| standard_model | verify_status varchar，INCOMPLETE/UNVERIFIED/VERIFIED | 资料完整性与人工核验；checked_at非空不代表已核验 |
| standard_model | checked_by bigint → internal_staff.id，可空 | 人工核验人；新建或资料变更后按业务规则清除 |
| standard_model | status ACTIVE/INACTIVE统一词典 | 000033注释有DEPRECATED/DRAFT，需处理已有值，不能无依据改成ACTIVE |
| standard_model_version | version_no统一bigint，保留(model_id,version_no)唯一 | 与API正bigint字符串范围一致，snapshot保存完整业务资料与核验依据 |
| standard_model | 官方身份完整时(company,official_id)唯一 | 已确认可保存不完整草稿；当前全量唯一索引会让第二条空身份草稿冲突。建议仅对非空身份加唯一条件，不能限制为仅VERIFIED唯一 |

公司64、名称/官方ID128、类型16字符的实际数据库长度目前由适配层拒绝超长，不截断。若产品要求更长公司名称，公共增量迁移与API上限一起调整。

## 2. 现存数据处理要求

迁移前输出行数、空/重复官方身份、非空旧JSON结构、无有效内部创建人、未知status清单，不把未知数据自动认定为业务就绪。

- 新空表可直接建立完整非空约束；已有行则分阶段补列/核对/回填，再建立约束。
- 既有created_by可能为空或已发生转交，不能盲目全部回填为maintainer。缺有效归属的行需明确人工认领/迁移方案，不能默认为当前访问人员。
- checked_at仅核对日期，历史行不能批量标VERIFIED。根据实际资料重算完整性，未补齐INCOMPLETE、完整但无人工证据UNVERIFIED。
- 当前版本应与真实完整快照一并建立；不能只DEFAULT version_no=1而不创建版本行，也不能把迁移日冒充历史业务修改时间。
- 非空旧JSON对象和旧价格结构先转换并审阅，不丢字段、不把未知能力改成不支持、不把缺价格改为0。
- 不重写旧报价快照或旧报告，不对已发布历史版本补写未经核验的事实。

## 3. F1 真实事务与查询验收

新增/修改/核验：当前投影、版本快照、operation_log同事务；日志失败或commit失败不得返回成功。新建identity分配遵守GENERATED ALWAYS AS IDENTITY（如领域先取序列ID，需明确INSERT策略），不能直接用普通INSERT显式ID导致冲突。

公共operation_log.request_id是varchar(64)，部分A领域允许128，需统一上限并让超长输入明确失败，禁止截断追踪号；reason(500)同理。读写不回显敏感来源参数。

列表关联筛选需同一资源EXISTS同时匹配supplier_ids和regions，按模型去重后分页，NULLS LAST+稳定ID。无关联筛选时无资源模型仍可见。

独立测试库最小验收：两个待补齐草稿、完整官方身份重复拒绝、补齐后核验、核验后改价清除核验状态、历史价格不变、非维护人403、并发revision409、日志/commit失败回滚、空值/排序/分页。完成后再接主路由与本地Docker，不以编解码单测代替真库验收。

## 4. A 当前可交付与下一接入条件

后续已在独立测试库实际应用000033/34并运行检查器：上述五个缺列、version_no类型差异，以及第二条空身份草稿的唯一约束冲突均已确认。运行库仍32。详见[独立测试库验证记录](../TDD/SUMMARY.md#stage-29)；该记录补充下面早期仅检查运行库的证据，不代表完整仓储已就绪。

现新增InspectF1Schema/AssessF1Columns读取元数据检查列及类型；索引定义只读审阅，不把列存在当完整仓储就绪。2026-10-20再查本地model_bss仍32、dirty=false，public无三个F1目标表。证据见[实查记录](../TDD/SUMMARY.md#stage-08)。

已有领域服务、读取器、HTTP注册器和页面；现新增infra/supplypg.ModelDataRow编解码。该类型只映射八类资料，不构造StandardModel身份/核验/CAS聚合，不是可直接部署的完整仓储。

随后已新增ReadModelData，在调用方事务内读取现有资料列，以to_json(text[])明确处理数组，再解码JSONB与date；独立真库验证了特殊字符、NULL拒绝、精确价格、默认资料、查询错误及夹具回滚，见[资料读取验证](../TDD/SUMMARY.md#stage-05)。该函数不提供完整聚合、写入、核验或CAS，不绕过增量结构需求。

公共方落实字段和事务工具后，A继续实现ModelReadStore/ModelUnitOfWork，连接现有ModelService；不启用填假revision/假维护人的过渡仓储。字段全表差异见[差异矩阵](./2026-10-20-schema34-gap-matrix.md)。
