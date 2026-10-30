import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
const apiURL = process.env.API_URL || "http://127.0.0.1:8080";
export default defineConfig({
  plugins: [tailwindcss(), reactRouter()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  // APP_NAME from the root .env (exported by make and scripts/dev.sh) is baked
  // into the build, including the prerendered pages; src/lib/app.ts reads it.
  define: {
    "import.meta.env.APP_NAME": JSON.stringify(process.env.APP_NAME ?? ""),
  },
  // In development the browser talks to Vite, which forwards API calls to Go
  // with the Origin header unchanged (APP_ORIGIN stays http://localhost:3000).
  server: {
    host: "0.0.0.0",
    port: 3000,
    strictPort: true,
    proxy: { "/api": { target: apiURL } },
  },
});
