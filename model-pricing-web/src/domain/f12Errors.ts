export type F12ErrorAction = "write" | "download";

function isApiError(error: unknown): error is Error & { httpStatus: number; code?: string | number } {
  return error instanceof Error && error.name === "ApiError" && "httpStatus" in error;
}

export function f12ErrorMessage(error: unknown, fallback: string, action: F12ErrorAction = "write"): string {
  if (isApiError(error) && error.httpStatus === 503 && (error.code === 10000 || error.code === "10000")) {
    if (action === "download") return "合同与授信附件服务暂不可用，请稍后重试。";
    return "合同与授信保存服务暂不可用，结构或办理授权依赖可能尚未就绪；当前表单和已选附件已保留。";
  }
  return error instanceof Error ? error.message : fallback;
}
