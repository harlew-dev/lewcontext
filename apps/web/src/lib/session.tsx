import { createContext, useContext, type ReactNode } from "react";
import type { Session } from "./types";
// The visitor's session. The signed-in layout (pages/app/layout.tsx) provides
// it from its route loader; guest pages have none.
const SessionContext = createContext<Session | null>(null);
export function SessionProvider({
  session,
  children,
}: {
  session: Session | null;
  children: ReactNode;
}) {
  return (
    <SessionContext.Provider value={session}>
      {children}
    </SessionContext.Provider>
  );
}
// The signed-in session. Only for UI on signed-in pages, where the layout's
// loader guarantees one; guest pages use useIsGuest() or useOptionalSession().
export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error("useSession() needs a signed-in page");
  return session;
}
// For guest pages, which may be visited signed in or out.
export function useIsGuest(): boolean {
  return useContext(SessionContext) === null;
}
// For guest pages that need the user, and hooks that work on every page
// (useApi, useResource): the session, or null when signed out.
export function useOptionalSession(): Session | null {
  return useContext(SessionContext);
}
