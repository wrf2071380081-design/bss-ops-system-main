# F4 正式配置原生 34 存储契约

2026-10-21。复用supplier、supplier_formal_config、assignment_log、operation_log及A资源控制快照，不新增迁移。接既有FormalConfigService，不实现公共加密、审批或人员转交系统。

## 配置与凭据

- scope/base_url、创建/修改人及时间使用原生列；note保留原说明用途，不覆写为控制JSON。
- api_key_enc保存带协议标记的版本化密封信封：密钥版本和不透明密文字节以安全编码组合，整体不能超过原生varchar(512)。不截断、不以明文替代；旧未知编码拒绝猜测。此编码不是加密，认证加密仍由SecretSealer提供，并绑定供应商/配置身份作为AAD。
- revision、ACTIVE/INACTIVE、区域及确认依据保存在operation_log.after的supply.formal_config/v1安全控制快照，保留动作FORMAL_CONFIG_REVISION。快照只含安全View和密封信封的SHA-256指纹；不含明文、密文或密钥版本。指纹只用于核对当前密文列与该次版本是否一致，不用于代替认证解密。
- 控制快照必须追加且保留，不能作为临时日志裁剪。最新损坏快照不回退；未初始化历史配置不默认有效。普通日志展示按业务operation翻译，不展示存储标记或指纹。
- scope最多255字符、URL最多512字符，非法UTF-8/NUL或超限由领域入口返回字段错误；密封信封容量在编码时另行检查。时间在领域写命令中统一到UTC微秒，回执和持久化一致。

## 归属与事务

正式配置写入先锁supplier，归属从该主体的显式assignment_log(entity_type=supplier, field=buyer_id)记录链读取：初始分配old为空，随后old必须接续上一new；new/changed_by须为有效人员引用。不从裸created_by、资源buyer或当前调用人猜测当前采购人。A内部新建供应商仍须由创建路径按DEC-008原子保存“初始归属为认证创建者”的记录；外部注册的归属由可信办理流程建立。旧主体无明确归属记录时拒绝，配置命令不顺带创建归属或转交。

修改按supplier→config锁序，在锁定后以新语句读取当前控制头并核对原生投影、凭据指纹、确认依据及expected revision。只增一版；省略Key保留原信封。UPDATE_NOOP/DISABLE_NOOP仅追加普通安全日志，不增版本。日志失败或空日志回调不得返回成功。

停用查询本供应商所有资源的真实A控制快照，任何状态仍引用该配置即阻断；缺/坏资源快照不能当“无引用”。绑定与停用必须共用supplier锁，在同一外层事务检查/写入引用；跨模块锁序及并发联合验收仍需完成。底层资源存储不能作为绕过绑定领域服务的写入口。

所有操作加入调用方sql.Tx，不自行Begin/Commit/Rollback，调用方出错须整体回滚，提交成功后再响应。日志只接收安全DTO；新建/轮换缺真实加密器或URL策略时保持拒绝。

## 资源读取

FormalConfigStore.ResourceView可注入ResourceStateStore.FactsLoader的config提供方。同一读取快照中返回配置安全View，跨供应商不泄露；不解密或返回凭据。配置更新后旧资源绑定版本不自动跟随，继续显示FORMAL_CONFIG_CHANGED；停用仍阻断报价。

已绑定配置缺失或不属于该供应商时，资源详情保留并显示FORMAL_CONFIG_UNAVAILABLE，不把资源本身变成404；协议/密文指纹损坏及SQL故障继续明确报错，不当作正常空配置。

## 真实资源绑定

`NewResourceBindingWork(state, configs, resolve, proofLoader)`加入现有ResourceConfigBindingService。A按supplier→resource→formal config→B proof顺序取得锁和同一事务事实；不同schema的存储不能混装。proofLoader提供B当前配置节点指派及不可变来源，不能接受页面布尔声明。

正式配置维护按采购人绑定，资源初次配置关联按节点办理人，两者独立。绑定保存资源CAS与安全日志，保留flow/node/assignee/flow_revision及原资源来源引用；不在资源日志带入配置URL或密钥。重复同版本绑定只追加BIND_NOOP，配置更新后可由当前办理人显式关联新确认版本，不自动跟随。

绑定与停用真实并发的两个提交顺序均已在专用库验证：绑定先提交，停用看到引用并拒绝；停用先提交，后续绑定看到停用/版本变化并拒绝。此证据覆盖A的共同supplier锁，不证明B真实节点/审批或其余跨模块锁序已交付。

## 验收

原生34临时表与回滚，创建/更新/保留Key/轮换/NOOP/停用、真实归属转交链、403/409、真实SQL日志失败、密文漂移、坏控制头、长度边界、任何资源引用保护及安全View组合。测试用认证加密和URL策略属于受控提供方，不宣称生产密钥或公共能力已配置。
# 当前接入状态（2026-10-22）

`AESGCMSealer` 与 HTTPS 公网 URL 策略已实现并接入 `cmd/server/supply_runtime.go`。运行需设置 `MODEL_BSS_FORMAL_CONFIG_KEY_VERSION` 和 `MODEL_BSS_FORMAL_CONFIG_KEY`；后者是标准 Base64 编码的32字节密钥。两者都未设置时，服务可启动但正式配置写服务不装配、写接口返回503；只配置其中一项或密钥非法时启动失败。Stage60已为本机 Docker API 配置专用 `local-v1` 密钥；密钥保存在 Git 忽略且限权的本机环境文件，不代表生产密钥管理已配置。当前库没有供应商/正式配置，因此密文真实写入尚未验收。资源绑定仍等待B提供真实节点办理资格后联合验收。

