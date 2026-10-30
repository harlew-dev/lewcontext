export type User = {
  id: number;
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
// Props of a signed-in page: the session plus its own server-rendered data,
// or `unavailable` when the session service can't be reached.
export type AppPageProps<P = object> =
  (SessionProps & P) | { unavailable: true };
