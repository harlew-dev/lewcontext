import type { Session, User } from "@/lib/types";
// A signed-in session for tests. Override what a test checks:
//   testSession({ csrfToken: "csrf-7", user: { id: "usr_00000000000007" } })
export function testSession(
  overrides: Partial<Omit<Session, "user">> & { user?: Partial<User> } = {},
): Session {
  const { user, ...rest } = overrides;
  return {
    user: {
      id: "usr_00000000000001",
      name: "Ada Lovelace",
      email: "ada@lewcontext.test",
      createdAt: "2026-10-30T00:00:00Z",
      mfaEnabledAt: null,
      emailVerifiedAt: "2026-10-30T00:00:00Z",
      policies: null,
      ...user,
    },
    csrfToken: "csrf",
    expiresAt: "",
    signedInAt: "2026-10-30T00:00:00Z",
    emailVerification: "optional",
    ...rest,
  };
}
