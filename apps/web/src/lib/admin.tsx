import { createContext, useContext, type ReactNode } from "react";
import type { AdminSession } from "@/types/admin";
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
// The key, for building admin page links (routes.admin.*).
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
