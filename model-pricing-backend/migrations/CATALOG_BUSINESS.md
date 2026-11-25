# 官方采集业务存储版本

`000079_catalog_business` 将官方模型采集的批次、候选快照、审核、证据索引和采集计划归入 BSS 的 `model_catalog` schema。DSH 通过带独立服务凭据的 `POST /api/agent/catalog/business` 调用，不直连 BSS 数据库。请求协议为 `catalog-business.v1`，员工读写仍校验 BSS 会话与业务权限。

升级前停止采集入口和调度、双库备份。合并 main 后 BSS 目标版本为 81；首次切换业务归属时，先导入旧采集计划并核对原 ID 与配置，再执行 Agent 14 及后续版本，配套部署 API、Runtime 和 Web。BSS API 配置 `MODEL_BSS_AGENT_CATALOG_TOKEN`，DSH Runtime/Web 配置相同的 `BSS_AGENT_CATALOG_BUSINESS_TOKEN`，至少 32 位随机值，不复用控制接口凭据。计划配置归 BSS，到点执行仍在 DSH；其他类型计划不迁移。

该结构迁移不自动清空任何旧数据。历史重置需要单独授权、备份与明确的数据库目标。`down.sql` 会删除整个新业务 schema，只适用于确认可丢弃的新结构回退，不能对运行中的业务数据直接执行。生产升级不适用本机已获准的历史清空操作。

本机 2026-11-24 已应用 BSS 77 与 Agent 14；保留 1 条平台采集计划，旧采集历史按授权备份后重置。详细记录位于 Agent 仓库 `docs/Work_A/agent/batches/catalog-business-in-bss/record.md`。

`000080_catalog_plan_removal` 为采集计划增加 deleted_at。plans-bulk 允许 plan.action=delete，仍校验内部员工身份、共享管理资格和 revision；设置 active=false、next_at=NULL 后从列表及领取队列排除，保留原计划和 plan_occurrences。共享平台管理资格由受信 DSH 网关确认，个人计划仍校验创建人。模型批准写入的 M1:E 校验不变。升级配套 Agent 000015–000016，禁止通过物理删除计划破坏执行记录。

## 2026-11-24 合并 main 后的编号收口

main 的 `000077_schema_drift_repair` 保持原文件与编号。此前本机已经执行的采集 77、计划删除 78 属于合并前的历史编号，不重写数据库版本记录，也不清空数据。

| 版本 | 用途 |
| --- | --- |
| 77 | main 原有的结构漂移修复 |
| 78 | 旧版采集库兼容检查点，仅 SELECT 1，确保迁移器能找到数据库当前版本 |
| 79 | 采集业务结构，CREATE IF NOT EXISTS / CREATE OR REPLACE，保留已有表和数据 |
| 80 | 计划删除标记，ADD COLUMN IF NOT EXISTS |
| 81 | 前向重复 main 77 的修复 DDL，补齐旧版 78 库曾跳过的列及索引 |

已有 BSS 表结构而未建采集模块的库按 77→78→79→80→81 执行；历史采集库从 78 按 79→80→81 执行。不得 force 到目标版本，不得仅改文件名后跳过补丁。79/80 以及 78 检查点拒绝自动向下回退，避免删除升级前已经存在的业务表或删除标记；服务回滚保持新结构，数据库回退须使用已验证备份。

发布时先暂停本机 API 与 DSH 执行入口，备份两库，再用生产相同的 migrate 执行 BSS up；Agent migrate.ps1 继续校验已有 1–16 的文件校验和，不新增空迁移。升级前后核对 model_catalog 所有业务表的数据摘要、计划配置及 schema_migrations.dirty=false。

注意：仓库从完全空库重放时，旧 000020 演示迁移要求特定报价数据存在。本次兼容测试以只含 BSS 表结构的隔离基线验证，不等于解决或验证了历史全量空库引导。

## 82：型号级采集完整性查询

`000082_catalog_model_coverage_index` 为成功批次的 `result.models` 添加 GIN 索引，用于按精确型号查询最近一次完整成功。当前结果与历史完整成功分别返回，部分成功不会覆盖完整成功时间；个人批次按员工隔离，平台批次共享。此迁移只增加索引，不改写历史候选、批准记录或正式档案。条件规格、能力证据与条件价格通过现有 JSONB 字段扩展，配套契约见 `contracts/catalog/model-profile.md`。Agent 无新增表结构，保持迁移版本 16 并校验原文件摘要。
