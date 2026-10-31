export type User = {
  id: string;
  name: string;
  email: string;
  createdAt: string;
  // When two-factor authentication was turned on, or null while it's off.
  mfaEnabledAt: string | null;
};
export type Session = {
  user: User;
  csrfToken: string;
  expiresAt: string;
  signedInAt: string;
};
