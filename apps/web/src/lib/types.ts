import type { Page } from "./pagination";
import type { Workspace } from "@/types/workspaces";
export type User = {
  id: string;
  name: string;
  email: string;
  createdAt: string;
};
export type Session = {
  user: User;
  csrfToken: string;
  expiresAt: string;
  signedInAt: string;
};
// Data the server rendered for one API path, or why it couldn't.
export type Initial<T> = {
  path: string;
  data: T | null;
  error: string | null;
};
export type SessionProps = { session: Session };
// The workspace a signed-in page shows (the one in its URL, or the newest on
// pages outside a workspace) and the first page of the member's workspaces.
export type WorkspaceProps = {
  workspace: Workspace;
  workspaces: Initial<Page<Workspace>>;
};
// Props of a guest page: the session when the visitor is signed in.
export type GuestProps = { session: Session | null };
// Props of a signed-in page: the session, its workspaces and its own
// server-rendered data, or `unavailable` when the API can't be reached.
export type AppPageProps<P = object> =
  (SessionProps & WorkspaceProps & P) | { unavailable: true };
