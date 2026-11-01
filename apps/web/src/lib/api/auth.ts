import type { Session, User } from "@/lib/types";
import { ApiError, type Http } from "./http";
import { path } from "./paths";
export type Credentials = { email: string; password: string };
export type Registration = Credentials & { name: string };
// Login and registration start a session and return its CSRF token.
export type SignedIn = { user: User; csrfToken: string };
// Registration with email verification on starts no account: a code goes to
// the email, and registerVerify finishes (the sign-up is in a cookie). The
// answer is the same whether or not the email already has an account.
export type VerificationRequired = {
  verificationRequired: true;
  email: string;
};
export function needsVerification(
  result: SignedIn | VerificationRequired,
): result is VerificationRequired {
  return "verificationRequired" in result;
}
// The API refused a signed-in request because the account must verify its
// email first (EMAIL_VERIFICATION_EXISTING=required): a 403 with
// verificationRequired.
export function verificationRequired(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    error.status === 403 &&
    error.details.verificationRequired === true
  );
}
// A sign-up waiting for its code, as its own browser sees it.
export type PendingRegistration = { email: string; expiresAt: string };
// Login with two-factor authentication on starts no session: the password
// was right, and the code goes to loginMfa (the challenge is in a cookie).
export type MfaRequired = { mfaRequired: true };
export function needsMfa(
  result: SignedIn | MfaRequired,
): result is MfaRequired {
  return "mfaRequired" in result;
}
// A started setup: the secret (base32) to type into an authenticator app,
// and the otpauth:// URI its QR code holds. Shown once.
export type MfaSetup = { secret: string; uri: string };
// Recovery codes as shown to the user, once: each signs in once.
export type RecoveryCodes = { recoveryCodes: string[] };
// The password and a second factor (an authenticator or recovery code), for
// turning MFA off and for new recovery codes.
export type MfaConfirmation = { password: string; code: string };
export const authRoutes = {
  me: () => path<Session>("/api/auth/me"),
  login: () => "/api/auth/login",
  loginMfa: () => "/api/auth/login/mfa",
  mfa: () => "/api/auth/mfa",
  mfaSetup: () => "/api/auth/mfa/setup",
  recoveryCodes: () => "/api/auth/mfa/recovery-codes",
  register: () => "/api/auth/register",
  registerCheck: () => "/api/auth/register/check",
  registerResend: () => "/api/auth/register/resend",
  registerVerify: () => "/api/auth/register/verify",
  emailVerification: () => "/api/auth/email-verification",
  logout: () => "/api/auth/logout",
  password: () => "/api/auth/password",
  verifyPassword: () => "/api/auth/password/verify",
  passwordReset: () => "/api/auth/password-reset",
  checkPasswordReset: () => "/api/auth/password-reset/check",
};
export type ProfileInput = { name: string };
// With MFA on a code confirms the change, otherwise the current password.
export type PasswordChange = { newPassword: string } & (
  { currentPassword: string } | { code: string }
);
export type PasswordReset = { token: string; newPassword: string };
// A workspace named in an error: one you're the only owner of.
export type WorkspaceSummary = { id: string; name: string };
// The workspaces in the API's 409 for deleting the account of a workspace's
// only owner, or null for any other error.
export function lastOwnerWorkspaces(error: unknown): WorkspaceSummary[] | null {
  if (!(error instanceof ApiError) || error.status !== 409) return null;
  const { workspaces } = error.details;
  if (!Array.isArray(workspaces)) return null;
  return workspaces.filter(
    (w): w is WorkspaceSummary =>
      typeof w?.id === "string" && typeof w?.name === "string",
  );
}
export function auth(http: Http) {
  return {
    me: () => http.get(authRoutes.me()),
    // Signs in, or with MFA on answers { mfaRequired } (needsMfa) and waits
    // for loginMfa.
    login: ({ email, password }: Credentials) =>
      http.send<SignedIn | MfaRequired>("POST", authRoutes.login(), {
        email,
        password,
      }),
    // Finishes a sign-in with an authenticator or recovery code. A wrong code
    // is an ApiError with field "code"; a 401 means the challenge ended (5
    // wrong codes, or 5 minutes) and the password is needed again.
    loginMfa: (code: string) =>
      http.send<SignedIn>("POST", authRoutes.loginMfa(), { code }),
    mfa: () => mfa(http),
    // Creates the account and signs in, or with email verification on
    // answers { verificationRequired } (needsVerification): the code goes to
    // the email, and registerVerify finishes.
    register: ({ name, email, password }: Registration) =>
      http.send<SignedIn | VerificationRequired>(
        "POST",
        authRoutes.register(),
        { name, email, password },
      ),
    // This browser's sign-up waiting for a code; a 401 means it ended.
    registerCheck: () =>
      http.send<PendingRegistration>("POST", authRoutes.registerCheck(), {}),
    // A new code for this browser's sign-up, replacing the last one.
    registerResend: () =>
      http.send<VerificationRequired>("POST", authRoutes.registerResend(), {}),
    // Finishes the sign-up with its code: the account is created and signed
    // in. A wrong code is an ApiError with field "code"; a 401 means the
    // sign-up ended (expired, or 5 wrong codes); a 409 that the email has
    // an account after all.
    registerVerify: (code: string) =>
      http.send<SignedIn>("POST", authRoutes.registerVerify(), { code }),
    emailVerification: () => emailVerification(http),
    logout: () => http.send<void>("POST", authRoutes.logout(), {}),
    // Deletes the account and signs it out. A wrong password is an ApiError
    // with field "password"; the only owner of a workspace gets a 409 whose
    // workspaces lastOwnerWorkspaces() reads.
    destroy: ({ password }: { password: string }) =>
      http.send<void>("DELETE", authRoutes.me(), { password }),
    // Saves the user's name; the email address can't change.
    updateProfile: ({ name }: ProfileInput) =>
      http.send<User>("PATCH", authRoutes.me(), { name }),
    // Checks the current password; failures are an ApiError with field "password".
    verifyPassword: (password: string) =>
      http.send<void>("POST", authRoutes.verifyPassword(), { password }),
    // Saves a new password. The API signs out every session and returns a new
    // one for this browser (new cookie and CSRF token), for useRenewSession().
    // A wrong code is an ApiError with field "code", a wrong password one with
    // field "currentPassword".
    changePassword: (change: PasswordChange) =>
      http.send<Session>("PATCH", authRoutes.password(), change),
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
// Verifying the signed-in account's email, as a singleton resource.
function emailVerification(http: Http) {
  return {
    // Emails a code to the account's address, replacing any earlier one.
    store: () => http.send<void>("POST", authRoutes.emailVerification(), {}),
    // Verifies the email with the code; returns the account. A wrong code is
    // an ApiError with field "code"; a 410 means the code ended (expired, or
    // 5 wrong codes) and a new one is needed.
    update: (code: string) =>
      http.send<{ user: User }>("PATCH", authRoutes.emailVerification(), {
        code,
      }),
  };
}
// Two-factor authentication, as the singleton resource of the account.
function mfa(http: Http) {
  return {
    // Checks the password and starts a setup, replacing a pending one.
    setup: (password: string) =>
      http.send<MfaSetup>("POST", authRoutes.mfaSetup(), { password }),
    // Turns MFA on with the first code from the app. Other sessions are
    // signed out. Returns the recovery codes, once.
    store: (code: string) =>
      http.send<RecoveryCodes>("POST", authRoutes.mfa(), { code }),
    // Turns MFA off. Wrong input is an ApiError with field "password" or "code".
    destroy: ({ password, code }: MfaConfirmation) =>
      http.send<void>("DELETE", authRoutes.mfa(), { password, code }),
    recoveryCodes: () => ({
      // Replaces every recovery code; returns the new ones, once.
      store: ({ password, code }: MfaConfirmation) =>
        http.send<RecoveryCodes>("POST", authRoutes.recoveryCodes(), {
          password,
          code,
        }),
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
