import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { data, redirect } from "react-router";
import { adminRoles, type AdminRole, type AdminSession } from "@/types/admin";
import { adminRoutes } from "./admin-routes";
import { ApiError } from "./api";
import { loaderAdminApi } from "./api/admin";
import { unauthorized, unavailable, type Unavailable } from "./loaders";
// The admin pages' key and session. The key is the ADMIN_ROUTE_KEY from the
// page's URL (/admin/:key): it lives only there and here in memory, never in
// storage, SWR keys or the page's text, and goes to the API in X-Admin-Key
// (useAdminApi). The session is null on the sign-in page. previousKey says
// the URL's key is being retired (the API's X-Admin-Key-Status: previous,
// on the layout's first call or any since), and retiring records that it is.
type Admin = {
  adminKey: string;
  session: AdminSession | null;
  previousKey: boolean;
  retiring: () => void;
};
const AdminContext = createContext<Admin | null>(null);
export function AdminProvider({
  adminKey,
  session,
  previousKey = false,
  children,
}: {
  adminKey: string;
  session: AdminSession | null;
  previousKey?: boolean;
  children: ReactNode;
}) {
  // The key can start being retired mid-visit (the operator rotated it), so
  // every admin call can turn this on; nothing turns it off: once the
  // previous key is removed, its sessions end.
  const [retired, setRetired] = useState(previousKey);
  const retiring = useCallback(() => setRetired(true), []);
  const value = useMemo(
    () => ({ adminKey, session, previousKey: retired, retiring }),
    [adminKey, session, retired, retiring],
  );
  return (
    <AdminContext.Provider value={value}>{children}</AdminContext.Provider>
  );
}
// For hooks that work on the admin sign-in page too (useAdminApi).
export function useOptionalAdmin(): Admin | null {
  return useContext(AdminContext);
}
// The key, for building admin page links (adminRoutes.*).
export function useAdminKey(): string {
  const admin = useContext(AdminContext);
  if (!admin) throw new Error("useAdminKey() needs an AdminProvider");
  return admin.adminKey;
}
// The signed-in admin's session, on the pages inside the admin layout.
export function useAdminSession(): AdminSession {
  const session = useContext(AdminContext)?.session;
  if (!session) throw new Error("useAdminSession() needs a signed-in admin");
  return session;
}
// Whether this page's key is being retired (the layout's banner).
export function usePreviousAdminKey(): boolean {
  return useContext(AdminContext)?.previousKey ?? false;
}
// Whether an admin with role may do what needs need: an operator anything a
// viewer may; an unknown role can do nothing, nor can anything need one.
// Hide actions with it; the API refuses them regardless (403).
//   adminCan(useAdminSession().role, "operator")
export function adminCan(role: AdminRole, need: AdminRole): boolean {
  const have = adminRoles.indexOf(role);
  const needed = adminRoles.indexOf(need);
  return have >= 0 && needed >= 0 && have >= needed;
}
// The admin pages first ask the API whether the key in the URL is right
// (GET /api/admin/me with it). A wrong key is the API's 404, and the page is
// the ordinary not-found page, so the admin pages never show for it; the
// server already answered the HTML with 404 too. These loaders live here,
// not in lib/loaders.ts, so they stay out of the app build.
type Probed = { session: AdminSession; previousKey: boolean };
async function adminProbe(
  key: string,
): Promise<Probed | "signed-out" | Unavailable> {
  let previousKey = false;
  try {
    const session = await loaderAdminApi(key, () => (previousKey = true)).me();
    return { session, previousKey };
  } catch (error) {
    if (error instanceof ApiError && error.status === 404)
      throw data(null, { status: 404 });
    if (unauthorized(error)) return "signed-out";
    return unavailable;
  }
}
// The admin sign-in page: a signed-in admin goes on to Users.
export async function adminLoginLoader(
  key: string,
): Promise<{ signedOut: true } | Unavailable> {
  const probe = await adminProbe(key);
  if (probe === "signed-out") return { signedOut: true };
  if ("unavailable" in probe) return probe;
  throw redirect(adminRoutes.users.index(key));
}
// The admin layout: the admin session (and whether the key is being
// retired), or back to the admin sign-in page.
export async function adminLoader(key: string): Promise<Probed | Unavailable> {
  const probe = await adminProbe(key);
  if (probe === "signed-out") throw redirect(adminRoutes.login(key));
  return probe;
}
