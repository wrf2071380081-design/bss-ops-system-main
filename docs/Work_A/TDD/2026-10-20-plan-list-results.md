# F13 资源计划列表读取

日期：2026-10-20。依据详细设计§8.4及DEC-010，服务于资源详情计划面板与公共工作台接入。

## 交付

新增PlanReader.List、可选PlanListStore及GET `/api/internal/plans`。支持resource_id、region、kind、status、page/size，默认1/20、每页至多100；固定created_at DESC、数值id DESC稳定排序。所有内部人员读取，不按当前buyer做读取域过滤；创建和办理资格不因可读而扩大。

仓储返回每条计划及其当前资源一致性事实。列表与GET详情共享buildPlanSummary验证与安全投影，返回原发起人、is_initiator、当前/预期资源revision、precondition_matches及blocked_reasons；read_only仍为true，未来预计时间不阻断阅读。

领域读取层校验枚举/ID/分页、返回范围、重复计划、总数和不可能页，返回副本。可选列表能力缺失503，不以一次GET或当前页数据伪造列表。SQL必须先过滤再分页，total是计划数，不是资源数。

## TDD与证据

先写3组领域测试，记录[缺类型编译红色](./evidence/2026-10-20-plan-list-red.txt)；实现领域读取后增加HTTP测试，记录[未注册路由404的断言红色](./evidence/2026-10-20-plan-list-http-red.txt)，再注册路由。

- [最终回归](./evidence/2026-10-20-plan-list-tests.txt)：supply/API共124组顶层测试通过。
- [vet](./evidence/2026-10-20-plan-list-vet.txt)、[build](./evidence/2026-10-20-plan-list-build.txt)：ExitCode=0。

覆盖默认分页/稳定排序参数、原发起人与当前buyer分离、结果副本、非法条件、缺列表能力、跨资源/重复行/错误total/越界页、HTTP查询转发及拒绝staff_id/重复字段。真实Gin与领域读取器，认证/仓储为替身；Go容器执行，本轮未改前端。

## 待完成

真实SQL筛选/排序/计数及一致性尚未验收；端口查询字段不是SQL已经正确实现的证据。计划列表与创建/办理页面尚待接入，执行HTTP仍需公共事务/幂等响应提交顺序支持。当前Docker及公共主路由没有装配这些新能力。
