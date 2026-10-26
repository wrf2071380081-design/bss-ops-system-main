# Work B · 测试平台接入

分支：`feature/test-platform-integration`
基线：`main@51757ab`（v0.22 供应商域交付后，库版本 36）

本目录存放 B 线（测试平台 ↔ BSS 打通）的设计与实施记录。
需求盘点与决策结论见 `文档资料/测试平台接入-功能盘点与决策清单-20261023.md`（该目录不入库）。

## 目标

把外部测试平台的连通性与压力测试结果，接进「供应商申报 → 内部审批 → 资源接入」的主链路，
让测试结果成为两道门禁的真实依据，而不是人工登记的数字。

## 已确认口径（2026-10-23）

| 编号 | 结论 |
|---|---|
| Q1 | 提交申报判「测试完成 + 数据完整」（`verdict=fail` 也允许提交）；登记接入判「通过」 |
| Q2 | 压测取 `outcome=completed` 的最高并发档；最高档 `limited` 则回退，详情展示全部档位 |
| Q3 | 连通性判定用 `baseline`（L1），耗时取 L1 `minimal_chat`；一期不接 `health` |
| Q4 | 迁移 000037 增加 `cache_hit_ratio` + `cache_ratio_available`，页面三态由可用标志驱动 |
| Q5 | 凭据输入按主体级、快照落资源级；不改 `resource` 表结构 |
| Q6 | 指纹 = 标准模型 + 接口模型 ID + base_url + api_key 指纹；不含系数/报价/申报版本；换 Key 即失效 |
| Q7 | 内部复测不占机会、可重复、独立留档；展示源取最新一条有效报告 |
| Q8 | 人工补传与自动报告等效，标 `AUTO` / `MANUAL`，必须绑定指纹 |
| Q9 | 一期只做 `baseline` + `stress`，客户端预留 `category` 扩展位 |
| Q10 | BSS 侧建测试批次，每行一个平台任务，批次结论在 BSS 聚合，批次 ID 走 `labels.bss_batch` |
| Q11 | 轮询为主、回调为辅，不依赖公网入口 |
| Q12 | 终态立即拉取 json + md 落本地，`report_url` 只作兜底 |
| Q13 | 提交申报时创建 `PENDING_TEST` 资源，审批接入后转 `PENDING_ACCESS` → `ADMITTED` |

Q14–Q16 暂不处理。

## 实施顺序

- **M1** 出站客户端 + DTO + 单模型 `baseline` + 轮询 + 报告落库 + 报告 provider 注入（解掉现有 503）
- **M2** 批量拆分与聚合 + 压力测试 + 指标映射 + 两道门禁 + 资源四态；前置迁移 000037
- **M3** 主体级凭据 + 申报多行 + 发起测试 + 异常重试 + 指纹失效
- **M4** 内部复测 + 人工补传 + 测试期写锁 + 报告本地留存

## 待协调

Q13 要求「提交申报时创建 `PENDING_TEST` 资源」，与 A 侧已有的资源创建入口（`resource_creation.go`）
存在双写风险，落地前需与 A 线合并为同一条创建路径。

## 现状

- `internal/api/router_submission.go`：`registerSubmissionRoutes` / `registerOpenCallbacks` 仍为空实现
- 全仓库尚无调用测试平台的出站客户端
- `test_job` / `test_report` 表结构齐备，仅缺写入方
- A 侧 `ResourceReportReader` 适配层已就绪，但运行时未注入 provider，报告接口返回 503
