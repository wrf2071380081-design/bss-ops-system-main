import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// 本地开发：前端跑在 5173，后端跑在 8080。
// 生产是 nginx 同源反代（.env.local 里 VITE_API_BASE_URL=/api），
// 但 dev server 默认不会转发 /api，必须显式配 proxy，否则登录等接口全部打不到后端。
export default defineConfig({
  plugins: [vue()],
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
