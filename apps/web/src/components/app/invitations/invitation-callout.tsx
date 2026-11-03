import { Notice } from "@/components/ui/feedback";
import type { InvitationCheck } from "@/lib/use-invitation";
import type { InvitedRole } from "@/types/invitations";
// "Acme has invited you to join.", above the invitation page and the login and
// register forms while their ?next= is an invitation link. Every value is
// plain React text inside fixed wording, never markup or a link, and the
// inviter's email is shown so a misleading workspace name can be judged.
// `ending` says what to do next on this page.
export function InvitationCallout({
  check,
  ending,
  dead,
}: {
  check: InvitationCheck;
  ending: string;
  // What an expired or revoked link says here.
  dead: string;
}) {
  switch (check.kind) {
    case "checking":
      return (
        <div
          role="status"
          aria-label="Checking your invitation"
          className="h-20 animate-pulse rounded-lg bg-zinc-950/5 dark:bg-white/5"
        />
      );
    case "live": {
      const { workspace, invitedBy, role } = check.invitation;
      return (
        <Notice variant="info">
          <strong className="font-semibold">{workspace.name}</strong> has
          invited you to join.
          <span className="mt-1 block">
            {invitedBy
              ? `${invitedBy.name} (${invitedBy.email}) invited you as ${withArticle(role)}.`
              : `You were invited as ${withArticle(role)}.`}{" "}
            {ending}
          </span>
        </Notice>
      );
    }
    case "dead":
      return <Notice variant="error">{dead}</Notice>;
    case "unreachable":
      return (
        <Notice variant="error" onRetry={check.retry}>
          {check.message}
        </Notice>
      );
    case "none":
      return null;
  }
}
// "a member", "an admin".
export function withArticle(role: InvitedRole) {
  return role === "admin" ? "an admin" : `a ${role}`;
}
