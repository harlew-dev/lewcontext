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
export const statuses = ["In progress", "Planned", "Completed"] as const;
export const colors = ["violet", "blue", "amber", "emerald", "rose"] as const;
export type Project = {
  id: number;
  name: string;
  description: string;
  status: (typeof statuses)[number];
  color: (typeof colors)[number];
  createdAt: string;
  updatedAt: string;
};
export type ProjectInput = Pick<
  Project,
  "name" | "description" | "status" | "color"
>;
export type WorkspaceProps = {
  session: Session;
  initialProjects: Project[] | null;
  initialError: string | null;
};
export type WorkspacePageProps = WorkspaceProps | { unavailable: true };
