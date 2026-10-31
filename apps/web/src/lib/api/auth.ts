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
  passwordReset: () => "/api/auth/password-reset",
  checkPasswordReset: () => "/api/auth/password-reset/check",
};
export type ProfileInput = { name: string };
export type PasswordChange = { currentPassword: string; newPassword: string };
export type PasswordReset = { token: string; newPassword: string };
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
      http.send<Session>("PATCH", authRoutes.password(), {
        currentPassword,
        newPassword,
      }),
    // Asks for a reset link. The API answers the same whether or not an
    // account uses the email; the link arrives by email (in development, the
    // server's mailbox file).
    requestPasswordReset: (email: string) =>
      http.send<void>("POST", authRoutes.passwordReset(), { email }),
    // Checks a reset link still works; a dead one is an ApiError with field
    // "token". The token goes in the body, never in an API URL.
    checkPasswordReset: (token: string) =>
      http.send<void>("POST", authRoutes.checkPasswordReset(), { token }),
    // Sets a new password with a reset link. Every session ends, so nobody is
    // signed in afterwards; returns the account's email for the login form.
    resetPassword: ({ token, newPassword }: PasswordReset) =>
      http.send<{ email: string }>("PATCH", authRoutes.passwordReset(), {
        token,
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
