/**
 * 产品版本标识（2026-11 版本说明 · 版本发布管理）。
 *
 * 口径：正式版本由 Git Tag / Release 管理，`main` 上的提交**不代表**已发布版本。
 * 界面只显示产品版本（v0.1.0），构建信息可用 VITE_APP_VERSION / VITE_APP_BUILD 覆盖，
 * 方便在测试环境区分同一版本的不同构建。
 */
export const APP_VERSION = import.meta.env.VITE_APP_VERSION || "0.1.0";
export const APP_BUILD = import.meta.env.VITE_APP_BUILD || "";
export const APP_VERSION_LABEL = APP_BUILD ? `v${APP_VERSION} · ${APP_BUILD}` : `v${APP_VERSION}`;
