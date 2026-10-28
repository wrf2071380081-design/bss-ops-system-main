import { assertContractId } from "../domain/contractId.ts";

export interface SupplierMergePartyDTO {
  id: string;
  name: string;
  region: "CN" | "OVERSEAS";
  buyer_id: string;
  revision: string;
}

export interface SupplierMergeReferencesDTO {
  resources: number;
  submissions: number;
  pending_submissions: number;
  onboarding_flows: number;
  contracts: number;
  credit_lines: number;
  formal_configs: number;
  qualifications: number;
  customer_relations: number;
  test_credential_records: number;
  website_account_configured: boolean;
  aliases: number | null;
}

export interface SupplierMergeReviewItemDTO {
  code: string;
  title: string;
  source_count: number;
  target_count: number;
}

export interface SupplierMergePreviewDTO {
  source: SupplierMergePartyDTO;
  target: SupplierMergePartyDTO;
  source_references: SupplierMergeReferencesDTO;
  target_references: SupplierMergeReferencesDTO;
  duplicate_resource_keys: number;
  review_items: SupplierMergeReviewItemDTO[];
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("供应商合并预览响应格式不正确。");
  return value as Record<string, unknown>;
}

function id(value: unknown): string {
  assertContractId(value);
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) throw new Error("供应商合并预览 ID 必须是十进制字符串。");
  return value;
}

function count(value: unknown): number {
  if (!Number.isSafeInteger(value) || Number(value) < 0) throw new Error("供应商合并预览引用数量不正确。");
  return Number(value);
}

function party(value: unknown): SupplierMergePartyDTO {
  const row = record(value);
  const partyID = id(row.id);
  if (typeof row.name !== "string" || !row.name.trim() || (row.region !== "CN" && row.region !== "OVERSEAS") ||
      typeof row.buyer_id !== "string" || !/^[1-9]\d*$/.test(row.buyer_id) || typeof row.revision !== "string" || !/^[1-9]\d*$/.test(row.revision)) {
    throw new Error("供应商合并预览主体信息不符合字段契约。");
  }
  return { id: partyID, name: row.name, region: row.region, buyer_id: row.buyer_id, revision: row.revision };
}

function references(value: unknown): SupplierMergeReferencesDTO {
  const row = record(value);
  for (const secret of ["api_key", "test_api_key", "api_key_enc", "file_ref", "file_path", "storage_path"]) {
    if (secret in row) throw new Error("供应商合并预览不得返回凭据或物理文件路径。");
  }
  if (typeof row.website_account_configured !== "boolean" || !(row.aliases === null || Number.isSafeInteger(row.aliases) && Number(row.aliases) >= 0)) {
    throw new Error("供应商合并预览主体引用信息不符合字段契约。");
  }
  return {
    resources: count(row.resources), submissions: count(row.submissions), pending_submissions: count(row.pending_submissions),
    onboarding_flows: count(row.onboarding_flows), contracts: count(row.contracts), credit_lines: count(row.credit_lines),
    formal_configs: count(row.formal_configs), qualifications: count(row.qualifications), customer_relations: count(row.customer_relations),
    test_credential_records: count(row.test_credential_records), website_account_configured: row.website_account_configured,
    aliases: row.aliases === null ? null : count(row.aliases),
  };
}

export function normalizeSupplierMergePreview(value: unknown, sourceID: string, targetID: string): SupplierMergePreviewDTO {
  const row = record(value);
  const source = party(row.source), target = party(row.target);
  if (source.id !== sourceID || target.id !== targetID || source.id === target.id || !Array.isArray(row.review_items)) {
    throw new Error("供应商合并预览主体不匹配。");
  }
  const reviewItems = row.review_items.map(item => {
    const review = record(item);
    if (typeof review.code !== "string" || !/^[A-Z][A-Z0-9_]*$/.test(review.code) || typeof review.title !== "string" || !review.title.trim()) {
      throw new Error("供应商合并预览待核对事项不符合字段契约。");
    }
    return { code: review.code, title: review.title, source_count: count(review.source_count), target_count: count(review.target_count) };
  });
  return { source, target, source_references: references(row.source_references), target_references: references(row.target_references),
    duplicate_resource_keys: count(row.duplicate_resource_keys), review_items: reviewItems };
}
