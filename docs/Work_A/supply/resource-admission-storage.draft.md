# F3 测试证据与登记接入原生事务适配

2026-10-21。复用现有34资源/报告/配置/模型/日志及既有event_outbox，不新增迁移。A接既有TestEvidenceService和ResourceAdmissionService；B提供真实调用鉴权、报告来源/任务序号、办理资格与逐行完成，公共方提供事务/outbox与主装配。

## 测试证据

TestEvidenceWork加入调用方sql.Tx，按supplier→resource→B来源的共同顺序取得事实。B授权适配返回明确可信结论和指定报告引用；不可信调用403，缺实现503。A再从真实test_report/test_job读取该份报告的结论、指标与缓存并核对业务身份，不相信页面自报通过。

选定当前证据时CAS更新资源和保留控制快照；旧目标、旧任务及重复报告仅留普通日志，不改变revision。日志保留本次report_id、任务序号、来源/目标版本和SELECTED/IGNORED/NOOP结果，均无凭据。ADMITTED/OFFLINE资源失败复测不自动改实际上下架状态，quote_readiness按新证据阻断。

## 登记接入

AdmissionWork按supplier→resource→formal config→B来源/节点的顺序锁定；同一事务读取A原生模型/配置/控制头与所选真实报告，B proof在该事务读取当前指派及适用前置。不把采购人、审批资格、查询筛选人当当前办理人。

资源PENDING_ACCESS且自身测试、配置、来源及版本均满足后，由领域服务提出ADMITTED/ever_admitted=true/revision+1。模型核验仍单独决定报价，不额外阻止合法接入。资源更新、安全控制日志、完成说明、B逐行完成与outbox同事务；任何一步失败由外层整体回滚。A不自动完成整批节点、流程或设置supplier.established。

完成说明要求非空且合法UTF-8、无NUL；保留原办理节点引用与完成依据。控制快照使用已有supply.resource/v1协议的可选completion_note和test_evidence字段，不在日志复制正式URL、密钥或报告正文。

## Outbox 接入

AdmissionOutboxWriter接收A准备的安全事件并返回真实outbox主键；A在同事务核对event_outbox中新行的类型、payload、请求号、创建人和PENDING状态，空/错误回调不能当成功。事件ID及异步投递归公共适配，不在锁内网络发送。

事件类型RESOURCE_ADMITTED，payload为supply.resource_admitted/v1：resource_id、supplier_id、standard_model_id、region、字符串resource_revision、来源行/目标版本、formal config引用与版本、选定report引用/任务序号、办理flow/node/revision、actor、reason、request_id、completion_note及occurred_at。C消费安全引用并按需要重读A事实；A不改C客户关系或现有报价。

外层必须提交成功后才发布HTTP/幂等响应。重复ADMIT由状态/revision冲突拒绝，不通过重复生成事件“补成功”；真正幂等重放在公共事务入口处理。尚无新浏览器直建/直接接入/自报测试的HTTP入口。

## 验收与边界

专用34库中验证真实A创建→报告关联→正式配置→绑定→接入→模型核验后可报价；同批资源各用自身结果；旧/乱序报告不覆盖；办理人/版本/前置错误拒绝；日志、B行、事件及外层回滚；一行成功不结束整批；已接入失败复测保持实际状态并阻断报价。

B proof/授权/版本和逐行完成及公共outbox为受控事务提供方，测试中实际写原生B行和outbox。该证据不等于真实B后端、回调鉴权、公共幂等或生产页面/Docker已联调。
