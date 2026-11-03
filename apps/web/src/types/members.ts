import type { Role } from "./workspaces";
// A workspace member as owners and admins see them (GET …/members). `id` is
// the user's public ID.
export type Member = {
  id: string;
  name: string;
  email: string;
  role: Role;
  addedAt: string;
};
// The members page state in its URL (?q=&page=). Members always sort by role,
// then name; there is no sort parameter.
export type MemberQuery = { page: number; q: string };
// One member's page (GET …/members/{member}). `current` is false for someone
// who left but still appears in the workspace's log: only their name is
// known, with no email, role or date.
export type MemberDetail = {
  id: string;
  name: string;
  email?: string;
  role: Role | "";
  addedAt: string | null;
  current: boolean;
  // Live personal keys they hold here, and changes they made.
  keys: number;
  changes: number;
};
