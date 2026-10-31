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
  password: () => "/api/auth/password",
  verifyPassword: () => "/api/auth/password/verify",
};
export type ProfileInput = { name: string };
export type PasswordChange = { currentPassword: string; newPassword: string };
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
    // Saves the user's name; the email address can't change.
    updateProfile: ({ name }: ProfileInput) =>
      http.send<User>("PATCH", authRoutes.me(), { name }),
    // Checks the current password; failures are an ApiError with field "password".
    verifyPassword: (password: string) =>
      http.send<void>("POST", authRoutes.verifyPassword(), { password }),
    // Saves a new password. The API signs out every session and returns a new
    // one for this browser (new cookie and CSRF token), for useRenewSession().
    changePassword: ({ currentPassword, newPassword }: PasswordChange) =>
      http.send<Session>("PUT", authRoutes.password(), {
        currentPassword,
        newPassword,
      }),
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
