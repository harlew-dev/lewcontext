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
