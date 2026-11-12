// 机器码 → 中文标签。
//
// 背景（2026-10-30 验收反馈「数据像乱码」）：库里和接口里的文本都是干净 UTF-8，
// 真正的问题是大量枚举以原始常量直接上屏——审计页整列 CUSTOMER_QUOTE_GENERATE、
// 业务对象 CUSTOMER_QUOTE · 19、价项 TEXT_INPUT、审批等待 PRICING_OP。
// 业务同学没有对照表，读起来就像乱码。
//
// 约定：
//   1) 这里只做展示映射，不改写语义、不拼接单位；
//   2) 映射表里没有的值原样返回（不能吞成空白，否则新枚举会变成空白格子更难排查）；
//   3) 服务端原文仍然保留在页面上（小号等宽字），便于对日志与接口。
export function labelOf(map: Record<string, string>, value?: string | null): string {
  if (!value) return "";
  return map[value] ?? value;
}

/** 审计动作。语义按 internal/repo 里写审计的调用点逐条核对（2026-10-30）。 */
export const AUDIT_ACTION_LABELS: Record<string, string> = {
  ALERT_HANDLE: "处置预警",
  COST_BASELINE_LOCK_PRIMARY: "锁定主供应商",
  COST_BASELINE_RECALC: "重算成本基线",
  COST_PARAM_UPDATE: "调整成本参数",
  CUSTOMER_QUOTE_ACCEPTED: "客户接受报价",
  CUSTOMER_QUOTE_GENERATE: "生成客户报价",
  CUSTOMER_TRANSFER: "客户转移",
  OFFICIAL_PRICE_CHANGE_APPLY: "提交官方价变更",
  PRICE_BOOK_PUBLISH: "发布价目表",
  PRICE_CHANGE_CONFIRM: "确认价格变更",
  PRICE_SYNC_JOB_CREATE: "新建采集批次",
  PRICE_UPCONDUCTION_DECIDE: "审批价格传导",
  PRICE_UPCONDUCTION_GENERATE: "生成价格传导",
  QUOTE_ACTIVATE: "启用供应商报价",
  QUOTE_APPROVE: "审批供应商报价",
  QUOTE_CONFIRM_REMOVE: "确认移除报价",
  QUOTE_REFRESHED: "刷新客户报价",
  QUOTE_RETRO: "补录供应商报价",
  QUOTE_SUBMIT: "提交供应商报价",
  QUOTE_WITHDRAW: "撤回供应商报价",
  SKU_PURCHASABLE: "SKU 上架可售",
  SPECIAL_PRICE_APPROVED: "审批特价",
  SPECIAL_PRICE_REQUEST: "申请特价",
  STAGING_PRICE_SUBMIT: "提交暂存价",
};

/** 操作记录（operation_log.action）→ 中文。措辞对齐交互原型 Token-Ops 的操作词表；
 *  表里没有的新枚举原样返回，避免出现空白格子。 */
export const OPERATION_ACTION_LABELS: Record<string, string> = {
  ADMIT: "确认模型已开通",
  APPROVE: "审批通过",
  ARCHIVE: "归档",
  COMPLETE_CONTRACT: "合同与授信确认完成",
  CONFIG_COMPLETE: "登记模型技术配置完成",
  CREATE: "新建",
  FORMAL_CONFIG_REVISION: "登记历史资源当前生产配置",
  PRICE_CHANGE: "采购改价整批",
  READ: "查看公告",
  REJECT: "驳回申报",
  RESOURCE_REVISION: "批量供应资源结果登记",
  SELECT_REPORT_PAIR: "测试结果已登记",
  START: "开始办理",
  SUBMISSION_HANDLING: "供应商申报",
  UPDATE: "修改",
  UPDATE_LICENSE: "维护供应商资质",
  UPDATE_NOOP: "提交（无变更）",
  UPDATE_PROFILE: "维护主体档案",
  UPLOADED: "上传附件",
  VERIFY: "核验通过",
};

/** 操作记录（operation_log.entity_type）→ 中文业务对象名。 */
export const OPERATION_ENTITY_LABELS: Record<string, string> = {
  contract: "合同",
  credit_line: "授信额度",
  customer_profile: "客户档案",
  onboarding_flow: "入驻流程",
  onboarding_node: "入驻节点",
  notice: "公告",
  price_book: "价目表",
  quote_sheet: "供应商报价单",
  resource: "供应资源",
  standard_model: "标准模型",
  submission: "模型申报",
  supplier: "供应商",
  supplier_formal_config: "正式配置",
};

/** 对象类型（原型筛选项）：把库里的 entity_type 归到原型的 8 个业务对象类别。
 *  原型一条记录可以同时属于多个类别（供应商+模型），我们按主体归类，保证筛选可解释。 */
export const OPERATION_ENTITY_CATEGORY: Record<string, string> = {
  contract: "客户",
  credit_line: "客户",
  customer_profile: "客户",
  notice: "系统记录",
  onboarding_flow: "系统记录",
  onboarding_node: "系统记录",
  price_book: "报价",
  quote_sheet: "报价",
  resource: "模型",
  standard_model: "标准模型库",
  submission: "申报",
  supplier: "供应商",
  supplier_formal_config: "供应商",
};

/** 对象类型下拉的固定顺序（与原型一致）。 */
export const OPERATION_ENTITY_CATEGORY_ORDER = [
  "标准模型库",
  "供应商",
  "客户",
  "模型",
  "报价",
  "申报",
  "请求",
  "系统记录",
];

/** 审计的业务对象类型（audit_log.target_type）。 */
export const AUDIT_TARGET_LABELS: Record<string, string> = {
  ALERT: "预警",
  CHANGE_REQUEST: "变更单",
  CONTRACT: "合同",
  COST_BASELINE: "成本基线",
  COST_PARAM: "成本参数",
  CUSTOMER_PROFILE: "客户档案",
  CUSTOMER_QUOTE: "客户报价",
  INTERNAL_STAFF: "内部人员",
  MODEL_SKU: "模型 SKU",
  ONBOARDING_FLOW: "入驻流程",
  PRICE_BOOK: "价目表",
  PRICE_UPCONDUCTION: "价格传导",
  QUOTE_SHEET: "供应商报价单",
  RESOURCE: "供应资源",
  ROLE: "角色",
  SUBMISSION: "模型申报",
  SUPPLIER: "供应商",
  SYNC_JOB: "采集批次",
};

/** 审计来源（audit_log.source_type）。INTERNAL 与 HUMAN 都是人工操作，历史数据两种都有。 */
export const SOURCE_LABELS: Record<string, string> = {
  INTERNAL: "内部人员",
  HUMAN: "内部人员",
  WORKER: "后台任务",
  CRON: "定时任务",
  SYSTEM: "系统",
  AGENT: "智能体",
};

/** 角色码（operator_role / approval_step.required_role / 权限快照）。 */
export const ROLE_LABELS: Record<string, string> = {
  PLATFORM_ADMIN: "平台管理员",
  MODEL_OPS: "模型运营",
  PROCUREMENT: "采购",
  PRICING_OP: "定价运营",
  SALES: "销售",
  FINANCE: "财务",
  OPS_ADMIN: "运维管理员",
  RETRO_OP: "补录专员",
  AUDIT_READONLY: "审计只读",
  SUPPLIER: "供应商",
  CUSTOMER: "客户",
  STAFF: "普通员工",
  SYSTEM: "系统",
};

/** 单据状态（申报单、报价单、变更单共用一套词表，未收录的原样显示）。 */
export const DOC_STATUS_LABELS: Record<string, string> = {
  DRAFT: "草稿",
  SUBMITTED: "已提交",
  PENDING_APPROVAL: "待审批",
  PENDING: "待审批",
  APPROVING: "审批中",
  APPROVED: "已通过",
  REJECTED: "已驳回",
  PROCESSING: "办理中",
  PENDING_TEST: "待测试",
  CONFIGURED: "配置完成",
  ADMITTED: "已接入",
  COMPLETED: "已完成",
  WITHDRAWN: "已撤回",
  CANCELED: "已取消",
  EFFECTIVE: "已生效",
  FORMAL: "正式",
  ACTIVATED: "已启用",
  DEPRECATED: "已退役",
  CLOSED: "已关闭",
  OPEN: "待处理",
  RESOLVED: "已处理",
};

/** 变更单类型（change_request.change_type）。 */
export const CHANGE_TYPE_LABELS: Record<string, string> = {
  PRICE_UP: "调价·上调",
  PRICE_DOWN: "调价·下调",
  PRICE_BOOK_PUBLISH: "价目表发布",
  SPECIAL_PRICE: "特价审批",
  DEPRECATE: "模型退役",
  COST_CHANGE: "采购成本变化",
};

/** 官方价项类型（standard_model.price_items[].task_type）。 */
export const PRICE_ITEM_TYPE_LABELS: Record<string, string> = {
  TEXT_INPUT: "文本输入",
  TEXT_OUTPUT: "文本输出",
  IMAGE_INPUT: "图片输入",
  IMAGE_GENERATION: "图片生成",
  AUDIO_INPUT: "语音输入",
  AUDIO_OUTPUT: "语音输出",
  VIDEO_GENERATION: "视频生成",
  EMBEDDING: "向量化",
  RERANK: "重排",
};

/** 报价组件类型（quote_component.component_type）。 */
export const COMPONENT_TYPE_LABELS: Record<string, string> = {
  input: "输入",
  output: "输出",
  embedding: "向量化",
  cache_read: "缓存读取",
  cache_write: "缓存写入",
  image: "图片",
};
