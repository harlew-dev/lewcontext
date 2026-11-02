// The web app is built twice from this one package: the app build (every
// page but the admin dashboard's) and the admin build, which the Go server
// hands out only behind ADMIN_ROUTE_KEY (docs/api/admin.md). WEB_BUILD picks
// one: `app` (the default) or `admin`. routes.ts, react-router.config.ts and
// vite.config.ts all read it here, so an unknown value fails every step.
export type WebBuild = "app" | "admin";

export function webBuild(): WebBuild {
  const value = process.env.WEB_BUILD || "app";
  if (value !== "app" && value !== "admin")
    throw new Error(`WEB_BUILD=${value}: use app (the default) or admin`);
  return value;
}

// Where the admin build's files are served from, in production and by its
// development server: /admin-assets/<file>, never /assets, so the Go server
// can keep them behind the assets pass.
export const adminAssetsBase = "/admin-assets/";

// The admin modules, as paths under src/: only the admin build may hold
// them (plans/features/admin/README.md, rule 7). The app build fails if any
// module it bundles is one of these (vite.config.ts), however it was imported.
const adminModules =
  /^(pages\/admin\/|components\/app\/admin\/|components\/layout\/admin-layout|lib\/admin|lib\/use-admin-resource|lib\/api\/admin|types\/admin)/;

export function isAdminModule(pathUnderSrc: string): boolean {
  return adminModules.test(pathUnderSrc);
}
