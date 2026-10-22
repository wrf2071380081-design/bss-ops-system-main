# A / F3 从进入办理的申报行创建资源

日期：2026-10-21。依据：DEC-003、最新需求REQ-11/17、已有CreateFromSubmission草案。

## 输入与可信来源

命令仅指定申报行ID及不可变行版本。认证WriteMeta提供人员、原因、追踪号；不接受页面直接指定supplier/model/region/buyer或测试结论。

B在共享事务内读取并锁定申报行及办理事实，提供SubmissionResourceProof：row_id/version、target_version、supplier_id、standard_model_id、region、接口模型ID、采购系数、已分派采购人、flow_id/revision、当前调用的授权人员，以及审批/分派已落实且进入办理的事实。ApprovedAndAssigned与EnteredHandling是B服务端事实，不是HTTP入参；B负责按真实审批和一次性分派生成。

资料草稿、未完成审批分派、尚未进入办理均不能创建正式资源。模型与供应商身份须有效，供应商区域须匹配；已有模型未核验不阻止建立待接入来源，但后续报价仍阻断。

## 初态

资源默认PENDING_TEST、ever_admitted=false、revision=1，不自动ADMITTED。B提供当前行自身报告时，报告的供应商、标准模型、区域、行/版本及目标版本必须相符；草稿报告resource_id可以为空，不为满足外键而捏造资源引用。仅有效且连通与压力均通过时初态PENDING_ACCESS，失败或无报告仍PENDING_TEST。缓存数据不设准入阈值。

保留自身选定报告引用便于追溯，不复制报告正文/凭据。正式配置另行绑定，申报测试配置不复制为正式配置。系数为(0,1]的最多四位十进制，保存规范化四位字符串。

后续证据关联需要可比较的B任务序号：初建带报告时同步保存Report.Sequence与LatestReportSequence；无真实序号拒绝写入，不用报告ID或时间猜测。序号体系须从草稿行进入正式资源后连续可比较，见[测试证据关联契约](./resource-evidence.draft.md)。

## 防重与事务

共享事务以来源申报行和(supplier_id,standard_model_id,region)共同防重；不存在资源时也须有来源/身份键锁及数据库唯一约束防并发双建。已有资源必须同时匹配同一来源行/版本和身份，才作为同一创建意图的重读，不能把另一条申报静默接管现有资源。

重复创建不重置资源状态、版本、采购人、报告或修改时间；返回当前资源及created=false，新的非幂等重放请求意图仍记CREATE_NOOP留痕。真正幂等重放由公共中间件返回原结果。

新资源、B行resource_id回写与操作日志在同一事务。CREATE_NOOP只允许补回相同资源引用，禁止覆盖B已有不同绑定。日志、回写、唯一冲突或提交失败须整笔回滚，不留下单边记录。资源ID由公共数据库分配，需支持GENERATED ALWAYS身份列，不在A自行使用时间戳ID。

已新增原生34 A存储及创建事务适配，见[控制快照与创建装配](./resource-control-storage.draft.md)。真实SQL验证创建→草稿报告转存→核对归属/结论→行关联及控制头；重复创建不重置，失败整体回滚。B真实审批分派/不可变版本/锁序提供方与公共主装配仍待接入；不发布可由浏览器绕过审批和分派的直建资源接口。
