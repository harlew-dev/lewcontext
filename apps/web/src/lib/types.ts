export type User = {
  id: string;
  name: string;
  email: string;
  createdAt: string;
  // When two-factor authentication was turned on, or null while it's off.
  mfaEnabledAt: string | null;
  // When the account proved it owns its email, or null if it never has
  // (made while email verification was off).
  emailVerifiedAt: string | null;
};
// The server's email verification setting (EMAIL_VERIFICATION and
// EMAIL_VERIFICATION_EXISTING): off, optional (unverified accounts may
// verify from Account settings) or required (they must, before anything else).
export type EmailVerificationMode = "off" | "optional" | "required";
export type Session = {
  user: User;
  csrfToken: string;
  expiresAt: string;
  signedInAt: string;
  emailVerification: EmailVerificationMode;
};
// The account must verify its email before it can do anything else.
export function mustVerify(session: Session): boolean {
  return (
    session.emailVerification === "required" && !session.user.emailVerifiedAt
  );
}
