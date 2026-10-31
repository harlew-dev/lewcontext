import useSWR from "swr";
import type { InvitationPreview } from "@/types/invitations";
import { ApiError, errorMessage, useApi } from "./api";
import { linkToken } from "./routes";
import { useOptionalSession } from "./session";
// What an invitation link holds, for its page and for login and register
// while their ?next= is one.
export type InvitationCheck =
  | { kind: "none" } // no invitation (not a well-formed link)
  | { kind: "checking" }
  | { kind: "live"; invitation: InvitationPreview }
  | { kind: "dead" } // expired, revoked, replaced or already accepted
  | { kind: "unreachable"; message: string; retry: () => void };
// Checks the invitation link with `token` (POST /api/invitations/check). The
// result is cached for the visit, so moving between login and register
// doesn't ask again, and isn't refreshed on focus: each check counts against
// sign-in's per-IP limit.
export function useInvitation(token: string | null): InvitationCheck {
  const api = useApi();
  const userId = useOptionalSession()?.user.id;
  const wellFormed = token !== null && linkToken.test(token);
  const { data, error, mutate, isValidating } = useSWR(
    wellFormed ? ["invitation-check", token, userId] : null,
    () => api.invitations().check(token as string),
    {
      revalidateOnFocus: false,
      revalidateOnReconnect: false,
      revalidateIfStale: false,
    },
  );
  if (token === null) return { kind: "none" };
  if (!wellFormed || deadLink(error)) return { kind: "dead" };
  if (data) return { kind: "live", invitation: data };
  if (error && !isValidating)
    return {
      kind: "unreachable",
      message: errorMessage(error),
      retry: () => void mutate(),
    };
  return { kind: "checking" };
}
// A link the API refused (422 with field "token").
export function deadLink(error: unknown): boolean {
  return (
    error instanceof ApiError && error.status === 422 && error.field === "token"
  );
}
