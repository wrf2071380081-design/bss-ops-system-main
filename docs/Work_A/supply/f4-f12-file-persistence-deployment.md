# 附件与运行目录部署说明

<a id="deployment"></a>

更新：2026-10-27。只使用原日常model_bss_api_latest和model_bss_pg；日常库45/false。F12/资质挂载已有指定验收，main账户设置合并构建及其上传目录尚待部署。 当前迁移与部署见[交接](./handoff.md)，执行结果只维护[TDD SUMMARY](../TDD/SUMMARY.md)。下文保存业务/接口/存储约束，不再逐阶段重复测试结论。

## 三类目录分别配置

| 参数 | 当前用途/状态 |
|---|---|
| MODEL_BSS_F12_ATTACHMENT_STORE_DIR | 原API使用/app/private/f12-attachments，宿主model-pricing-backend/.private/f12-attachments，已验授权下载和重启保持 |
| MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR | 原API使用/app/private/qualification-attachments，宿主.private/qualification-attachments，已验元数据与文件保持 |
| MODEL_BSS_UPLOAD_DIR | main账户设置企业证明目录，代码默认var/uploads；尚未替用户配置该运行持久卷，更新API前核对绝对容器路径、宿主绑定和实际工作目录 |

旧二进制挂载文件名bin/server-v36不代表数据库版本或当前源码版本。此次合并只完成构建/定向回归，没有替换运行二进制。正式配置Key按DEC-018处理，不需要另生成AES密钥。


<a id="mount-example"></a>

## 既有A附件挂载示例与约束

```yaml
services:
  api: # 替换为现有 API 服务名
    environment:
      MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR: /var/lib/model-bss/attachments/qualifications
      MODEL_BSS_F12_ATTACHMENT_STORE_DIR: /var/lib/model-bss/attachments/f12
    volumes:
      - supplier_qualification_files:/var/lib/model-bss/attachments/qualifications
      - f12_attachment_files:/var/lib/model-bss/attachments/f12

volumes:
  supplier_qualification_files:
  f12_attachment_files:
```

| 环境变量 | 用途 | 未设置时 |
| --- | --- | --- |
| `MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR` | 供应商资质文件 | 资质文件能力不装配 |
| `MODEL_BSS_F12_ATTACHMENT_STORE_DIR` | 合同与授信附件 | 附件能力不装配，相关请求按依赖缺失失败关闭 |

部署时先确认两个卷都挂载到 API 容器的可写目录，再重建 API 容器。仅重启旧容器不会加载新的环境变量或挂载。目录需由 API 私有访问，限制容器外的读写权限；不要把卷挂到 `nginx`、前端容器或静态文件服务。

备份和恢复时，两个卷中的 `.blob` 与 `.json` 文件必须成对保存，并与数据库中对应的文件引用一起备份。恢复需保留文件内容及对应目录；只恢复数据库或只恢复文件卷都会产生不可用引用。保留策略按业务及备份策略执行；应用启动时的孤儿清扫处理 F4 资质目录中超过24小时且未被 `supplier_qualification.file_ref` 引用的文件，以及 F12 中超过24小时且未被合同/授信引用的文件。清扫会先核对数据库引用；数据库查询失败时不删除候选文件。该清扫不是通用备份或即时回滚机制。不要用会删除命名卷的清理命令处理承载业务附件的环境。

资质和 F12 目前仍是 A 的本地私有文件适配器，不等于 D07 已接通。该配置只解决 API 容器重建后本地文件仍可用；迁移到 D07、鉴权、审计和保留策略仍按对应共享契约单独交接。

F12本轮实际结果见SUMMARY Stage200。F4目录、上传业务数据后再重建容器、数据库与附件联合恢复等未执行场景仍按TDD清单单独验收，不能从F12重启下载推定全部通过。

## 生命周期与备份

F12独立上传成功但业务保存失败时保留文件供重试，超过24小时仍未引用才清扫；资质单业务动作保存失败清除新文件。查询引用失败必须停止删除，历史合同和资质引用的文件保留。清扫不按日志“看起来无用”判断；数据库/附件一同备份，避免只有引用没有字节。

目录仅后台受控访问，下载再次校验主体/来源/权限，不将磁盘路径、Key或原始平台URL回给列表/日志。私有文件、运行上传、转储及临时输出不入Git。迁移/回退及本机升级前备份位置只维护[数据库说明](./database-v45-upgrade.md)，不在本文件复制旧43/44阶段的部署快照。
