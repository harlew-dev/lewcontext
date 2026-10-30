import { createContext, useContext, type ReactNode } from "react";
import type { Session } from "./types";
// The visitor's session for the page being rendered. _app provides it from
// the page's props, so on the server it belongs to that one request.
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
// The signed-in session. Only for UI on signed-in pages, where appProps
// guarantees one; guest pages use useIsGuest() or useOptionalSession().
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
