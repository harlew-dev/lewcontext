import {
  createContext,
  useCallback,
  useContext,
  useState,
  type ReactNode,
} from "react";
import type { Session } from "./types";
// The visitor's session. The signed-in layout (pages/app/layout.tsx) provides
// it from its route loader; guest pages have none.
const SessionContext = createContext<Session | null>(null);
type Renew = (session: Session) => void;
const RenewContext = createContext<Renew | null>(null);
export function SessionProvider({
  session,
  children,
}: {
  session: Session | null;
  children: ReactNode;
}) {
  // A session the API rotated while this page was open (a password change)
  // replaces the loaded one, until the loader provides a different session.
  const [renewed, setRenewed] = useState<{
    from: Session | null;
    to: Session;
  } | null>(null);
  const current = renewed?.from === session ? renewed.to : session;
  const renew = useCallback<Renew>(
    (to) => setRenewed({ from: session, to }),
    [session],
  );
  return (
    <SessionContext.Provider value={current}>
      <RenewContext.Provider value={renew}>{children}</RenewContext.Provider>
    </SessionContext.Provider>
  );
}
// Replaces the signed-in session after the API rotated it, so later requests
// send the new CSRF token.
export function useRenewSession(): Renew {
  const renew = useContext(RenewContext);
  if (!renew) throw new Error("useRenewSession() needs a SessionProvider");
  return renew;
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
