/**
 * crypto.randomUUID 的兼容层。
 *
 * 背景（2026-11-16 PM 反馈「报价单无法创建」）：测试服务器是 http://10.19.218.33:8081，
 * 属于**非安全上下文**，浏览器不提供 crypto.randomUUID（只有 HTTPS 和 localhost 才有）。
 * 而页面在 setup 阶段就调用它生成幂等键/生命周期标识（约 60 处），于是整个页面直接抛
 * `crypto.randomUUID is not a function`，表单白屏、按钮无反应——本地 localhost 复现不了。
 *
 * 这里在应用启动前补一个等价实现（RFC 4122 v4）。优先用 getRandomValues，
 * 极端情况下退到 Math.random（仅用于幂等键/生命周期标识，不用于安全用途）。
 */
export function installSecureRandom(): void {
  const api = globalThis.crypto as (Crypto & { randomUUID?: () => string }) | undefined;
  if (!api || typeof api.randomUUID === "function") return;

  const fallback = (): string => {
    if (typeof api.getRandomValues === "function") {
      const bytes = api.getRandomValues(new Uint8Array(16));
      bytes[6] = (bytes[6] & 0x0f) | 0x40;
      bytes[8] = (bytes[8] & 0x3f) | 0x80;
      const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
      return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
    }
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, char => {
      const random = (Math.random() * 16) | 0;
      return (char === "x" ? random : (random & 0x3) | 0x8).toString(16);
    });
  };

  Object.defineProperty(api, "randomUUID", { value: fallback, writable: true, configurable: true });
}
