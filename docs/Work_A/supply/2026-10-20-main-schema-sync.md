# main 最新数据库结构同步

2026-10-20，用户要求拉取GitHub最新main并核对字段更新。

- 远端main从c453aff更新到422be85（按原型v0.22落目标数据模型与模块收敛）。
- 新增000033_target_model：standard_model/version、supplier/formal_config、resource、测试、申报办理、plan、notice/read、operation_log等目标表；新增表与旧表并存。
- 新增000034_test_report_cache：test_report.cache_hit_tokens、cache_input_tokens两个可空bigint字段，不回填旧报告。
- 公共router.go新增按A/B/C拆分的注册入口、配置及模块开关。A的router_supply.go存在add/add冲突，已保留现有RegisterSupplyRoutes及新增公共registerSupplyRoutes签名；公共入口仍为空壳，不能视为真实仓储已经装配。

## 当前状态

使用fetch及merge --no-commit --no-ff合入工作区，没有生成提交或推送；合并冲突已解决。原A未提交修改保留。Git仍处于等待合并提交的状态，后续不能把暂存的上游文件误当成A新开发内容。

本次不执行数据库迁移，也不重启/替换运行API。此前实查本地model_bss迁移version=32、dirty=false；代码中最新迁移是34，两者不要混淆。

## A接入前仍需核对的字段差异

- standard_model尚无A领域依赖的revision、verify_status、maintainer_id等显式列，JSON资料默认对象与A的类型化列表也需映射。
- resource使用supplier_model_name、latest_test_report_id；A契约使用接口模型ID、选定报告及目标版本，不能直接把名称字段当接口ID。当前DDL也未包含资源revision、正式配置引用版本等全部字段。
- plan使用before/after JSON和PENDING/DONE/CANCELED注释，未包含计划revision、expected_resource_revision、executed_at、failure_reason等完整生命周期列；需后续增量迁移或明确映射，不能修改已发布迁移冒充一致。
- notice使用type/target_type/target_id/payload，A公告字段需明确JSON映射；不能把新增表的存在等同于公告适配器完成。

此前“未找到000033”的报告为历史事实，现已找到000033及000034；后续以本次同步代码为准。仍需结合最新共享契约核对并补齐真库适配。
