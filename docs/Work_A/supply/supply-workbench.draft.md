# 工作台 A 子能力：公告阅读与计划摘要

依据：v0.22 原型1263—1267行、A详细设计v1.1 §9.1/12.2、附01公共工作台归属。A提供公告/计划事实，公共负责人汇总A/B/C的待办/已办/待阅/已阅及人员选择界面。

## 1. 读取契约

| 接口 | 入参 | 结果 |
|---|---|---|
| GET `/api/internal/notices` | region、resource_id、read_status=ALL/UNREAD/READ、keyword、page/size | 当前认证StaffID的公告投影 |
| GET `/api/internal/notices/read-states` | 上述条件、staff_ids（逗号分隔）、all_staff=true | 指定人员或全部内部人员的阅读投影；省略人员条件默认为当前人员 |
| GET `/api/internal/notices/:id` | 可选staff_id | 公告及该人员阅读详情；省略staff_id看当前人 |
| GET `/api/internal/plans/:id` | 无人员替换参数 | 发起人、资源、预计/实际时间、修订号、状态及当前前提是否匹配的只读摘要 |

所有读取仅限认证内部人员，全员可读，不套旧角色域。staff_ids与all_staff不可同时使用；显式ID最多100个，重复/非法ID拒绝。全部人员通过真实内部人员目录查询，不以空数组猜测全集。分页对象为notice/person组合，total不是去重公告数；排序published_at desc、notice_id desc、staff_id asc，先过滤阅读状态再分页。

投影包含notice、staff_id/name、read_status、可空read_at、permissions.can_mark_read。仅查看自己且未读时可标记已读；查询他人不能切换认证身份。关联客户影响PENDING不能显示成0；READY时明确数量。

## 2. 标记已读

POST `/api/internal/notices/:id/read` 允许空body或`{}`，拒绝自报reader_id/staff_id/user_id等字段。Actor从认证StaffID读取，操作说明由服务端设为“阅读公告”，仍要求Idempotency-Key与请求追踪。

新请求同事务读取公告、核对内部人员、锁定/创建个人阅读行并追加操作记录。唯一键notice_id/staff_id由数据库强制。首次READ写阅读行，重复READ_NOOP保留首次read_at且不重复阅读行；新的请求意图仍留痕。公共幂等重放不再次执行业务。日志/提交失败必须整体回滚，不返回成功对象。

## 3. 计划只读摘要

计划发起人created_by与当前资源buyer_id分开。返回is_initiator仅说明发起人身份，不授权查看者代办。摘要始终read_only=true；本切片没有注册计划修改/执行接口，公共工作台据此展示详情。

预计时间仅用于追踪，不因尚未到期拒绝读取；当前资源revision与计划预期不同则返回precondition_matches=false和阻断原因，GET不修改计划状态。未来F13执行服务仍须验证发起人、实际外部操作确认、资源前提及共享事务。

## 4. 公共/真实适配要求

使用公共人员目录的StaffID，不把登录AccountID/OperatorID当作人员选择值。读取公告人员组合在一个一致性快照中完成；all_staff按真实目录分页组合，不先分页人员再伪造总数。

NoticeUnitOfWork须加入已有幂等事务，阅读唯一性与日志同事务；禁止独立提交。该端口不替代公共事务/日志工具。计划摘要从plan及当前resource一致性读取，不写旧表，不执行到期计划。新表/日志/主装配缺失仍是接入依赖。
