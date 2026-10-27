# v43～v45 数据库升级与本机旧版本对齐

<a id="database"></a>

更新：2026-10-27，升级执行Stage205、main合并Stage206、文档精简Stage208。日常库 `model_bss_pg/model_bss = 45 / dirty=false`；仍用原 `model_bss_api_latest:8080`。未新建数据库或容器，未新增业务提交或推送。结果统一见 [TDD SUMMARY](../TDD/SUMMARY.md)，历史版本含义不覆盖正式编号。

## 正式编号

| 版本 | 迁移文件（各含 up/down） | 内容 |
|---|---|---|
| v43 | `000043_supplier_subject_link` | 团队 main：供应商关联法律主体、唯一索引及资料匹配回填 |
| v44 | `000044_f12_records` | A：合同/授信资料、附件、流程来源、操作人及版本约束 |
| v45 | `000045_supplier_metadata` | A：供应商曾用名，资质类型、有效期、审核事实、revision及操作人 |

文件位于 `model-pricing-backend/migrations/`。团队v43与`origin/main@1fe8907`内容哈希一致，没有改写。A尚未提交的`000043_f12_records`改名为`000044_f12_records`，原`000044_supplier_metadata`改名为`000045_supplier_metadata`；down报错版本号同步更新，业务DDL不变。当前1～45编号无重复且up/down配对。

合并后团队v43已由main跟踪，A的v44/v45随本次分支交付连同业务代码和本说明一起纳入。Windows检出可能按Git设置转换LF/CRLF，应按Git规范化内容及diff核对团队文件，不把换行导致的原始字节哈希差异误判为修改迁移。

## 团队正常升级：v43 → v44 → v45

适用：已执行团队v43，实际有`supplier.subject_id`且版本43/false。升级前备份数据库及受控附件，检查现有版本重复并暂停业务写入。

1. 用项目既有迁移工具、当前迁移目录，目标升级到44，执行F12迁移。
2. 确认17个可空元数据列、版本唯一键及约束，版本44/false。
3. 继续目标升级到45，执行供应商元数据迁移。
4. 检查曾用名表、资质六列及约束，版本45/false，恢复API并检查`/healthz`。

正常v42先执行团队v43；正常v44（已有主体关联及F12）只执行v45。不要重复手工执行已应用DDL；历史版本冲突应停止核查，不能删除或重编号业务资料。不要给团队正常库使用下面的一次性脚本。

### 迁移目录与执行命令

正式文件分别为 [v43 up](../../../model-pricing-backend/migrations/000043_supplier_subject_link.up.sql) / [down](../../../model-pricing-backend/migrations/000043_supplier_subject_link.down.sql)、[v44 up](../../../model-pricing-backend/migrations/000044_f12_records.up.sql) / [down](../../../model-pricing-backend/migrations/000044_f12_records.down.sql)、[v45 up](../../../model-pricing-backend/migrations/000045_supplier_metadata.up.sql) / [down](../../../model-pricing-backend/migrations/000045_supplier_metadata.down.sql)。A文档仍集中在Work_A，不再在migrations目录复制一套结果报告；目录内SQL是实际执行依据。

项目Makefile使用`migrate`命令。部署端先准备连接正确目标库的`DB_DSN`环境变量，不在聊天或日志打印凭据。下面仅适用于**正常团队43/false起点**，每步先核对版本和真实结构；当前本机已45，不应重跑这两步：

```powershell
Set-Location H:/Desktop/Dongxin/bss-ops-system-main/model-pricing-backend
migrate -path migrations -database "$env:DB_DSN" version
# 确认团队43且已备份、停写后：
migrate -path migrations -database "$env:DB_DSN" up 1
migrate -path migrations -database "$env:DB_DSN" version
# 确认44及F12能力后：
migrate -path migrations -database "$env:DB_DSN" up 1
migrate -path migrations -database "$env:DB_DSN" version
```

不要用`migrate force 45`代替DDL，也不要对本机旧编号库照抄正常升级命令。版本只读查询为`SELECT version, dirty FROM public.schema_migrations;`，业务资料的`version/revision`与数据库迁移版本不是同一个概念。

<a id="local-upgrade-files"></a>

## 本机旧 v44 一次性补齐（已执行）

旧本地编号44表示`F12@43 + 供应商元数据@44`，缺团队主体关联；它与正常团队44结构不同，不能直接重放新v44/v45。

本机一次性补齐脚本限定数据库名`model_bss`、单条44/false记录、旧F12/曾用名结构存在且主体关联列不存在。主体匹配有歧义即拒绝。在同一事务内取得迁移锁、原样执行团队v43，再用本机结构断言检查列、有效唯一键、已验证CHECK及外键，并比较21张业务表的行数和内容摘要；供应商只排除允许回填的新`subject_id`列。任何失败整体回滚。全部通过后才在同一事务内把版本记录44更新为45并提交，不是单独强制改数字。

两个脚本仅服务本机旧编号库的一次性升级/断言，不参与应用启动或团队正式迁移，现已移出交付目录，在后端忽略的`.qa/docs-consolidation-*/original/docs/Work_A/supply/sql/`保留原文；本机索引`.qa/docs-consolidation-current.txt`记录本次目录。正式部署只用migrations。此脚本已执行成功，**不要重跑**。现在45库应在前置检查处拒绝；不得用于dirty、其他库名、结构不明或部分失败数据库。结构断言使用事务内会话设置，独立执行时也须在同一个显式事务中运行，检查后ROLLBACK即可。

## 最小验证与运行边界

先在原数据库事务临时表中运行v43→v44→v45正式DDL，检查主体回填、唯一约束和资质约束，最终ROLLBACK；未新建测试库。再对本机补齐路径做真实结构预演并回滚。随后暂停同一个API、取得最终备份、提交补齐、恢复API，确认45/false和业务摘要不变。

升级后F12能力、供应商能力、F12运行装配三项Go定向测试通过。测试数据库门槛已对齐44/45；历史文件名及opt-in变量中的43/44保留兼容，不表示运行库版本。本轮未重跑此前全部页面写入或调用Inspector。

## 备份与回退

最终备份在后端忽略目录`.qa/schema-v45-20261027/model_bss_before_canonical45_final.dump`，SHA-256为`146C4246DBA47CF0E6B8F858BA5FE494749D344D1C6FC250B1ACAE4241CBB187`。最终备份目录已检查，较早备份另通过结构解析，未做全量恢复演练。受控附件目录`.private/`需与数据库引用一起保留，不能只备份数据库。

优先保留45结构并向前修复；此次为增量结构，原API可继续读原字段。团队正常路径down对应45→44撤回元数据、44→43撤回F12；有新业务资料时down会拒绝丢弃数据，不能强删列。

本机备份属于**旧编号44**，不能对当前库直接down到44来恢复旧状态：那会撤回供应商元数据，却不会恢复旧编号含义。必须恢复时，先停API、导出升级后的新增资料及附件，再整体恢复该备份和配套附件，并使用与旧编号一致的工作区/迁移目录；不要将新迁移目录直接对着恢复后的旧44库启动。破坏性恢复另行确认目标及保留方案。本轮未执行down或恢复。

## 合并边界

Stage205先解决数据库；Stage206已将`work-A-fix`快进到`origin/main@1fe8907`并恢复合并后的全部A改动。团队v43保持原样，A的v44/v45随本次分支提交交付；没有新建业务提交、推送或改写历史。合并后的构建及定向测试通过，但运行API尚未替换为此次合并构建，具体边界见SUMMARY Stage206。
