import { reactRouter } from "@react-router/dev/vite";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin, type ServerOptions } from "vite";
import { readPolicy } from "./src/lib/policies.server.ts";
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
const webDirectory = fileURLToPath(new URL(".", import.meta.url));

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

// The app build's policy manifest: build/client/.policies.json, the version
// (`updated` date) of each policy document and whether it's still the
// committed template, read with the pages' own reader. Go reads it at
// startup (internal/web): POLICIES=on needs it, and production refuses a
// template. A dot-file, so Go never serves it.
function policyManifest(): Plugin {
  return {
    name: "policy-manifest",
    apply: "build",
    applyToEnvironment: (environment) => environment.name === "client",
    generateBundle() {
      const document = (name: "terms-of-service" | "privacy-policy") => {
        const { updated, boilerplate } = readPolicy(name, webDirectory);
        return { version: updated, boilerplate };
      };
      this.emitFile({
        type: "asset",
        fileName: ".policies.json",
        source: JSON.stringify({
          termsOfService: document("terms-of-service"),
          privacyPolicy: document("privacy-policy"),
        }),
      });
    },
  };
}

export default defineConfig({
  plugins: [
    tailwindcss(),
    reactRouter(),
    ...(build === "admin"
      ? [adminPagesUnderBase()]
      : [noAdminModules(), policyManifest()]),
  ],
  resolve: { alias: { "@": srcDirectory } },
  // The admin build's files live at /admin-assets/, apart from the app's.
  base: build === "admin" ? adminAssetsBase : "/",
  // APP_NAME from the root .env (exported by make and scripts/dev.sh) is baked
  // into the build, including the prerendered pages; src/lib/app.ts reads it.
  // RELEASE, when the build is given one, names it in the errors the app
  // reports (src/lib/client-errors.ts); empty otherwise.
  define: {
    "import.meta.env.APP_NAME": JSON.stringify(process.env.APP_NAME ?? ""),
    "import.meta.env.RELEASE": JSON.stringify(process.env.RELEASE ?? ""),
  },
  server: build === "admin" ? adminServer : appServer,
});
