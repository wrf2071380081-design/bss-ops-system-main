# F3 统一采购系数命令阶段记录

日期：2026-10-20。依据：详细设计§6.1、§8.5、§12.1及[F3契约](../supply/supply-resources.draft.md)。

## 契约与交付

新增ResourceCoefficientService.Change和独立事务端口，服务于B调价申报与A调价计划，尚无HTTP或页面写入口。人工直接调价政策仍未确定，不接受MANUAL来源。

输入为认证WriteMeta、resource_id、expected_revision、十进制系数、来源kind/id/revision。只接受SUBMISSION/PLAN；系数(0,1]、最多4位小数，实际变更保存4位。来源引用不等于授权：事务适配器必须服务端读取相应不可变申报行或待执行计划，校验操作者、来源版本、资源、资源预期revision及系数，再返回证明。客户端不能提交Authorized标记。

PLAN验证原发起人；通过来源校验后不重复强制当前buyer身份，从而支持合法发起人与当前采购人不同。B申报授权需由B契约确定并实现；测试替身不证明实际授权规则已落地。

资源锁定后检查revision，使用CAS更新，只改coefficient/revision/updated_at，不改变测试目标版本、选定报告、状态和ever_admitted。数值无变化不升版本/不发事件，但追加CHANGE_COEFFICIENT_NOOP日志；新意图留痕，幂等重放由外层处理。事件含变更前后系数、来源、revision、region、原因、操作者和追踪。事件ID由公共outbox分配，C消费后自行处理，不直接重写原报价。

资源、操作记录、outbox和B/计划完成必须加入调用方同一个事务。日志/事件/提交失败返回nil，不产生部分成功。

## TDD证据

先写成功、来源伪造/不匹配、数值无变化和失败回滚测试，再实现服务；随后补充计划发起人与当前采购人不同的场景。

- [红色](./evidence/2026-10-20-resource-coefficient-red.txt)：新类型缺失的编译失败，非业务断言失败。
- [最终回归](./evidence/2026-10-20-resource-coefficient-tests.txt)：supply/API共96组顶层测试通过，本阶段新增5组。
- [vet](./evidence/2026-10-20-resource-coefficient-vet.txt)、[build](./evidence/2026-10-20-resource-coefficient-build.txt)：实现源码检查ExitCode=0；之后仅补充一项测试，最终回归已重新执行。

全部通过Go容器执行；仓储与来源验证是内存替身。回滚测试涵盖日志、outbox、commit失败及revision/非法精度/人工来源拒绝。没有真实SQL锁、唯一约束或跨模块事务验证。

## 后续接入

需要B与F13提供真实来源验证适配，公共方提供事务/日志/outbox；A真实资源仓储实现CAS。锁顺序必须在B行/计划/资源之间统一，事务中禁止网络查询；实际并发和来源完成失败回滚需真库联合测试。当前服务未被运行中的Docker API调用，未开放人工改系数按钮；模型广场完整维护及状态命令仍未完成。
