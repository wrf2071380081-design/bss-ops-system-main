# F4 正式配置原生 34 存储契约

2026-10-21；凭据策略按 DEC-018 于 2026-10-23 更新。复用supplier、supplier_formal_config、assignment_log、operation_log及A资源控制快照，不新增迁移。接既有FormalConfigService，不实现公共审批或人员转交系统。

## 配置与凭据

- scope/base_url、创建/修改人及时间使用原生列；note保留原说明用途，不覆写为控制JSON。
- 依据 DEC-018，当前 `api_key_enc` 列保存供应商提交的原始 Key，不做 AES-GCM 加密；列名沿用共享 000033 schema，后续字段改名需按共享迁移流程处理。Key 限制为非空、有效 UTF-8、无 NUL 且不超过 512 个字符。
- revision、ACTIVE/INACTIVE、区域及确认依据保存在operation_log.after的supply.formal_config/v1安全控制快照，保留动作FORMAL_CONFIG_REVISION。快照只含安全View和 Key 的 SHA-256 一向指纹，不含明文。该指纹用于核对当前列值与版本快照一致，不用于还原 Key。
- 旧 `se1` 密封信封只作兼容识别：读取时标为不可用，要求采购人重新录入；不自动解密、不将密文交给调用方，也不在本切片迁移数据库。
- 控制快照必须追加且保留，不能作为临时日志裁剪。最新损坏快照不回退；未初始化历史配置不默认有效。普通日志展示按业务operation翻译，不展示存储标记或指纹。
- scope最多255字符、URL最多512字符，非法UTF-8/NUL或超限由领域入口返回字段错误；Key按原生varchar(512)字符容量检查。时间在领域写命令中统一到UTC微秒，回执和持久化一致。

## 归属与事务

正式配置写入先锁supplier，归属从该主体的显式assignment_log(entity_type=supplier, field=buyer_id)记录链读取：初始分配old为空，随后old必须接续上一new；new/changed_by须为有效人员引用。不从裸created_by、资源buyer或当前调用人猜测当前采购人。A内部新建供应商仍须由创建路径按DEC-008原子保存“初始归属为认证创建者”的记录；外部注册的归属由可信办理流程建立。旧主体无明确归属记录时拒绝，配置命令不顺带创建归属或转交。

修改按supplier→config锁序，在锁定后以新语句读取当前控制头并核对原生投影、Key指纹、确认依据及expected revision。只增一版；省略Key保留原值。旧密封值必须在修改时显式替换。UPDATE_NOOP/DISABLE_NOOP仅追加普通安全日志，不增版本。日志失败或空日志回调不得返回成功。

停用查询本供应商所有资源的真实A控制快照，任何状态仍引用该配置即阻断；缺/坏资源快照不能当“无引用”。绑定与停用必须共用supplier锁，在同一外层事务检查/写入引用；跨模块锁序及并发联合验收仍需完成。底层资源存储不能作为绕过绑定领域服务的写入口。

所有操作加入调用方sql.Tx，不自行Begin/Commit/Rollback，调用方出错须整体回滚，提交成功后再响应。日志只接收安全DTO；新建/轮换仍要求URL策略通过，但不需要加密器或运行密钥。

## 资源读取

FormalConfigStore.ResourceView可注入ResourceStateStore.FactsLoader的config提供方。同一读取快照中返回配置安全View，跨供应商不泄露；不解密或返回凭据。配置更新后旧资源绑定版本不自动跟随，继续显示FORMAL_CONFIG_CHANGED；停用仍阻断报价。

已绑定配置缺失或不属于该供应商时，资源详情保留并显示FORMAL_CONFIG_UNAVAILABLE，不把资源本身变成404；配置快照/Key指纹损坏及SQL故障继续明确报错，不当作正常空配置。旧 `se1` 配置不作为可用 Key。

## 真实资源绑定

`NewResourceBindingWork(state, configs, resolve, proofLoader)`加入现有ResourceConfigBindingService。A按supplier→resource→formal config→B proof顺序取得锁和同一事务事实；不同schema的存储不能混装。proofLoader提供B当前配置节点指派及不可变来源，不能接受页面布尔声明。

正式配置维护按采购人绑定，资源初次配置关联按节点办理人，两者独立。绑定保存资源CAS与安全日志，保留flow/node/assignee/flow_revision及原资源来源引用；不在资源日志带入配置URL或密钥。重复同版本绑定只追加BIND_NOOP，配置更新后可由当前办理人显式关联新确认版本，不自动跟随。

绑定与停用真实并发的两个提交顺序均已在专用库验证：绑定先提交，停用看到引用并拒绝；停用先提交，后续绑定看到停用/版本变化并拒绝。此证据覆盖A的共同supplier锁，不证明B真实节点/审批或其余跨模块锁序已交付。

## 验收

原生34临时表与回滚，创建/更新/保留Key/轮换/NOOP/停用、真实归属转交链、403/409、真实SQL日志失败、Key漂移、旧密封值重录、坏控制头、长度边界、任何资源引用保护及安全View组合。当前定向单测通过；没有专用 PostgreSQL 测试库时，数据库集成用例跳过。
# 当前接入状态（2026-10-22）

2026-10-23 更新：正式配置加密器及 `MODEL_BSS_FORMAL_CONFIG_KEY_VERSION` / `MODEL_BSS_FORMAL_CONFIG_KEY` 启动要求已从源码移除。正式配置写服务不再依赖运行密钥；相关定向 TDD 与 API 入口编译见 [SUMMARY Stage 78](../TDD/SUMMARY.md#stage-78正式配置key移除aes-gcm加密)。本轮未部署 API、未迁移或写入运行库；当前资源绑定仍等待B提供真实节点办理资格后联合验收。

