import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin, type ServerOptions } from "vite";
import { adminAssetsBase, isAdminModule, webBuild } from "./web-build.ts";
const apiURL = process.env.API_URL || "http://127.0.0.1:8080";
// The admin build's development server, which only the app's one reaches.
const adminDevPort = 3002;
const build = webBuild();

// In development the browser talks to the app's Vite on :3000, which
// forwards API calls to Go with the Origin header unchanged (APP_ORIGIN stays
// http://localhost:3000), and admin pages and their files to the admin
// build's Vite on 127.0.0.1:3002, websockets (hot reload) included. Neither
// checks the admin key or the assets pass; the API still checks the key.
const appServer: ServerOptions = {
  host: "0.0.0.0",
  port: 3000,
  strictPort: true,
  proxy: {
    "/api": { target: apiURL },
    "^/admin(/|$)": { target: `http://127.0.0.1:${adminDevPort}` },
    [`^${adminAssetsBase}`]: {
      target: `http://127.0.0.1:${adminDevPort}`,
      ws: true,
    },
  },
};
const adminServer: ServerOptions = {
  host: "127.0.0.1",
  port: adminDevPort,
  strictPort: true,
  // The page is on :3000, so its hot-reload socket goes there too.
  hmr: { clientPort: 3000 },
};

// Vite answers every URL outside `base` with a 404 hint, so the admin build's
// development server would refuse its own pages (/admin/<key>/…). This puts
// `base` in front of them; Vite takes it off again, and React Router renders
// the page for the original URL (req.originalUrl).
function adminPagesUnderBase(): Plugin {
  return {
    name: "admin-pages-under-base",
    apply: "serve",
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        if (req.url && /^\/admin(\/|\?|$)/.test(req.url))
          req.url = adminAssetsBase.slice(0, -1) + req.url;
        next();
      });
    },
  };
}

const srcDirectory = fileURLToPath(new URL("./src", import.meta.url));

// The app build's main guard against admin code: it fails if any module it
// bundled is an admin module (web-build.ts), whatever the import looked like
// (an alias, a relative path, an extension, "..", a dynamic import()).
// ESLint stops such imports earlier, and scripts/check-app-build.sh reads the
// output for anything that isn't a module, such as a pasted string.
function noAdminModules(): Plugin {
  return {
    name: "no-admin-modules",
    apply: "build",
    buildEnd(error) {
      if (error) return;
      const found = new Set<string>();
      for (const id of this.getModuleIds()) {
        if (id.startsWith("\0")) continue; // virtual modules
        const file = path.relative(srcDirectory, id.split("?")[0]);
        if (file.startsWith("..") || path.isAbsolute(file)) continue;
        const under = file.split(path.sep).join("/");
        if (isAdminModule(under)) found.add(`src/${under}`);
      }
      if (found.size > 0)
        this.error(
          `Admin code reached the app build: ${[...found].join(", ")}. Only admin code may import it (plans/features/admin/README.md, rule 7).`,
        );
    },
  };
}

export default defineConfig({
  plugins: [
    tailwindcss(),
    reactRouter(),
    ...(build === "admin" ? [adminPagesUnderBase()] : [noAdminModules()]),
  ],
  resolve: { alias: { "@": srcDirectory } },
  // The admin build's files live at /admin-assets/, apart from the app's.
  base: build === "admin" ? adminAssetsBase : "/",
  // APP_NAME from the root .env (exported by make and scripts/dev.sh) is baked
  // into the build, including the prerendered pages; src/lib/app.ts reads it.
  define: {
    "import.meta.env.APP_NAME": JSON.stringify(process.env.APP_NAME ?? ""),
  },
  server: build === "admin" ? adminServer : appServer,
});
