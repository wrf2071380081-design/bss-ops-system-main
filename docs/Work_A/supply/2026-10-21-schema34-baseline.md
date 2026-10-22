# 按现有 33–36 结构实施的设计调整

日期：2026-10-21。当前结构基线：000033_target_model + 000034_test_report_cache + 000035_work_item_view + 000036_staff_approval_flag。用户已明确按当前库结构实施，详细设计可依据v0.22原型与最新需求调整。本文件优先于早期“必须新增控制列才能接入”的建议。本轮只读复核运行库为36 / dirty=false。

## 1. 保留的业务结论

标准模型库和模型广场不增加审批；不完整官方资料可以保存，但人工核验与报价条件仍须真实满足。新模型创建者初始化为维护人；读取全员可见，写入检查维护人。逐资源测试、正式配置与测试凭据隔离、先审批分派再办理、个人阅读和原发起人执行计划规则保持不变。

## 2. F1 复用现有存储

| 业务事实 | 33–36实际载体 | 实施约定 |
|---|---|---|
| 当前官方身份及资料 | standard_model既有列 | 使用已有JSONB/text[]/date列及长度，不新增同义列 |
| 初始创建人 | standard_model.created_by | 创建时写认证创建者，不用于推断已转交后的维护人 |
| 当前维护人、revision、核验状态/人 | standard_model_version.snapshot中的完整模型元数据 | 创建/修改/核验时保存真实业务快照，读取从快照取，不填默认值 |
| 当前业务版本 | 同模型最新的有效版本行 | 版本行version_no与snapshot.version_no严格一致；revision来自已保存JSON，不能用最大版本数临时冒充 |
| 并发写 | standard_model行锁 + 快照revision比较 | 先锁实体，再用新语句读当前版本；过期请求409；当前投影、版本和日志同一外层事务 |
| 数据一致性 | 当前列与最新版本快照 | 身份、资料、状态、时间必须一致；绕过业务修改、缺快照或旧格式不自动认定有效 |
| 历史版本号 | 原有integer列 | 保留整数范围；API仍用字符串，无需为字符串表达强制改bigint；越界明确拒绝 |
| 官方身份唯一 | 现有(company,official_id)全量唯一索引 | 保留约束。资料不完整不豁免身份冲突，相同空身份也409，不生成伪造官方ID或要求新增条件索引 |

snapshot继续使用已有 `contract=supply.standard_model, schema_version=1, model=完整领域模型` 包装。人工核验状态有明确存储依据；填写checked_at仍不能代替人工核验。无变化保存不新增版本，只留痕。

本方案不把现有未知历史行默认为“当前用户维护/已核验”。无合法快照的历史行需要显式转换或认领评审；不能自动伪造维护人或版本。公共转交能力后续应通过A维护命令同步完整新版本，不能只改单列或把created_by当转交字段。

## 3. F3 与工作台的判断方式

先复用resource的buyer_id、region、coefficient、ever_admitted和latest_test_report_id；复用test_job/test_report现有任务、指标及34缓存字段；复用notice/notice_read的个人阅读记录。状态词典、JSON协议及事务规则由A适配，不把注释差异直接当缺表。

资源来源、正式配置确认与B办理资格等真实事实，先从现有申报/任务/配置/日志及B接口逐项确定载体；尚未核实前，不把A内存结构中的每个字段都转为数据库加列要求。现有字段无法证明的业务条件保留拒绝原因，不默认通过。本次首先落实F1，其他模块按同样方法逐项调整并验证。

F3后续已落实[资源控制快照存储](./resource-control-storage.draft.md)：resource保持原生列，A控制元数据与完整安全资源状态写入operation_log.after的保留RESOURCE_REVISION记录，协议为supply.resource/v1。锁资源后读取已保存revision，更新/日志同外层事务；该类状态依据不能按临时日志裁剪。创建服务已接真实A存储，并为草稿报告转存保留B事务接口，不在草稿阶段伪造resource。B真实来源/版本/锁序、正式配置适配及生产装配仍须完成。

F4后续已落实[正式配置原生存储](./formal-config-storage.draft.md)：api_key_enc保存带密钥版本的密封信封；配置revision/逻辑状态/确认依据在保留安全快照，日志仅含密封信封指纹，不含密文或密钥版本。当前采购人来自显式assignment_log记录链，缺链的历史主体拒绝猜测。资源绑定共用supplier锁，真实引用与停用竞争已验证；公共加密/URL/归属初始化及B节点事实仍须真实装配。

## 4. 结构版本与部署

代码按33–36结构实现，不等于当前每个容器都已部署本工作区代码。最近一次运行库只读复核为36 / dirty=false；本次没有重新迁移或部署。000035为公共工作台视图，000036只新增员工供应商审批资格；A不修改已发布迁移，也不增加平行控制列。

早期临时补列测试证明的是此前候选方案，保留作历史证据；当前验收改为复制34原生列/整数版本/全量身份唯一约束的测试，不再补五个控制列。全部A产出继续放docs/Work_A，未经另行授权不提交或推送。

### 4.1 2026-10-21 本地 Docker 迁移与只读复核记录

- 迁移前记录：`model_bss_pg` 为 PostgreSQL 15，`schema_migrations` 是32 / dirty=false；000033目标表不存在，内部员工11行。先对隔离副本完成000033–000036演练，再对运行库应用同一迁移。
- 迁移后最后一次只读复核：`schema_migrations` 为36 / dirty=false；内部员工仍为11行；`test_report.cache_hit_tokens/cache_input_tokens`、`v_work_item`、`internal_staff.can_approve_supplier`均存在/可查询，审批资格默认false，未给员工授予权限。详见[TDD Stage 36](../TDD/SUMMARY.md#stage-36本机-postgresql-迁移对齐)。
- 同阶段由当前源码构建API并重建`model_bss_api_latest`，`/healthz`曾返回`{"db":"up","status":"ok"}`。这证明该次部署时数据库连通，不证明容器已包含本轮尚未提交的A代码或公共主路由接线。
- 本轮初次受限Docker CLI调用返回named pipe `Access is denied`；随后只读容器查询成功，确认PostgreSQL/API容器运行且`schema_migrations=36|false`。宿主没有Go命令，故后端验证在Go 1.26临时容器中执行。本轮没有迁移运行库或重建API容器。
