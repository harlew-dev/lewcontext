import type { Session, User } from "@/lib/types";
import type { Http } from "./http";
import { path } from "./paths";
export type Credentials = { email: string; password: string };
export type Registration = Credentials & { name: string };
// Login and registration start a session and return its CSRF token.
export type SignedIn = { user: User; csrfToken: string };
export const authRoutes = {
  me: () => path<Session>("/api/auth/me"),
  login: () => "/api/auth/login",
  register: () => "/api/auth/register",
  logout: () => "/api/auth/logout",
};
export function auth(http: Http) {
  return {
    me: () => http.get(authRoutes.me()),
    login: ({ email, password }: Credentials) =>
      http.send<SignedIn>("POST", authRoutes.login(), { email, password }),
    register: ({ name, email, password }: Registration) =>
      http.send<SignedIn>("POST", authRoutes.register(), {
        name,
        email,
        password,
      }),
    logout: () => http.send<void>("POST", authRoutes.logout(), {}),
  };
}
export const sessionRoutes = {
  destroyOthers: () => "/api/auth/sessions",
};
export function sessions(http: Http) {
  return {
    // Signs out every session except this one.
    destroyOthers: () =>
      http.send<void>("DELETE", sessionRoutes.destroyOthers()),
  };
}
