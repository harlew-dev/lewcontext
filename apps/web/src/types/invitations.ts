import type { Role } from "./workspaces";
// The roles an invitation may give: never owner (invite as admin, then
// promote), so a link can't grant ownership.
export type InvitedRole = Exclude<Role, "owner">;
// A workspace's invitation as owners and admins see it (GET …/invitations).
// `sentAt` is null while the email is being sent; `invitedBy` is null once
// that account is gone. Expired ones stay listed for 30 days.
export type Invitation = {
  id: string;
  email: string;
  role: InvitedRole;
  invitedBy: { id: string; name: string } | null;
  sentAt: string | null;
  createdAt: string;
  expiresAt: string;
};
export type InvitationInput = { email: string; role: InvitedRole };
// What an invitation link shows before it's accepted
// (POST /api/invitations/check).
export type InvitationPreview = {
  workspace: { name: string };
  invitedBy: { name: string; email: string } | null;
  email: string;
  role: InvitedRole;
  expiresAt: string;
};
// Whether an invitation's 7 days are up, at `now`.
export function expired(invitation: Invitation, now = Date.now()): boolean {
  return Date.parse(invitation.expiresAt) <= now;
}
