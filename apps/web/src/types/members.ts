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
