import { defineConfig } from "vite";
import { execSync } from "node:child_process";

// 构建戳：把当前 commit 写进 index.html，浏览器里一眼能看出页面是哪一版，
// 用来根治"本地改了、线上还是旧的"却没人发现的问题（2026-11-20）。
function buildSHA(): string {
  try {
    return execSync("git rev-parse --short HEAD", { encoding: "utf-8" }).trim();
  } catch {
    return "unknown";
  }
}
const SHA = buildSHA();
const BUILT_AT = new Date().toISOString();
import vue from "@vitejs/plugin-vue";

// 本地开发：前端跑在 5173，后端跑在 8080。
// 生产是 nginx 同源反代（.env.local 里 VITE_API_BASE_URL=/api），
// 但 dev server 默认不会转发 /api，必须显式配 proxy，否则登录等接口全部打不到后端。
export default defineConfig({
  define: { __BUILD_SHA__: JSON.stringify(SHA), __BUILT_AT__: JSON.stringify(BUILT_AT) },
  plugins: [vue(), {
    name: "inject-build-stamp",
    transformIndexHtml(html: string) {
      return html.replace("</head>", `    <meta name="build-sha" content="${SHA}" />\n    <meta name="built-at" content="${BUILT_AT}" />\n  </head>`);
    },
  }],
  server: {
    host: "127.0.0.1",
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
});
