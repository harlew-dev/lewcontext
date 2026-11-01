import { createContext, useContext, type ReactNode } from "react";
import { data, redirect } from "react-router";
import type { AdminSession } from "@/types/admin";
import { adminRoutes } from "./admin-routes";
import { ApiError } from "./api";
import { loaderAdminApi } from "./api/admin";
import { unauthorized, unavailable, type Unavailable } from "./loaders";
// The admin pages' key and session. The key is the ADMIN_ROUTE_KEY from the
// page's URL (/admin/:key): it lives only there and here in memory, never in
// storage, SWR keys or the page's text, and goes to the API in X-Admin-Key
// (useAdminApi). The session is null on the sign-in page.
type Admin = { adminKey: string; session: AdminSession | null };
const AdminContext = createContext<Admin | null>(null);
export function AdminProvider({
  adminKey,
  session,
  children,
}: {
  adminKey: string;
  session: AdminSession | null;
  children: ReactNode;
}) {
  return (
    <AdminContext.Provider value={{ adminKey, session }}>
      {children}
    </AdminContext.Provider>
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
// The admin pages first ask the API whether the key in the URL is right
// (GET /api/admin/me with it). A wrong key is the API's 404, and the page is
// the ordinary not-found page, so the admin pages never show for it; the
// server already answered the HTML with 404 too. These loaders live here,
// not in lib/loaders.ts, so they stay out of the app build.
async function adminProbe(
  key: string,
): Promise<AdminSession | "signed-out" | Unavailable> {
  try {
    return await loaderAdminApi(key).me();
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
// The admin layout: the admin session, or back to the admin sign-in page.
export async function adminLoader(
  key: string,
): Promise<{ session: AdminSession } | Unavailable> {
  const probe = await adminProbe(key);
  if (probe === "signed-out") throw redirect(adminRoutes.login(key));
  if ("unavailable" in probe) return probe;
  return { session: probe };
}
