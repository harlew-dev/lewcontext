export type ApiKey = {
  id: string;
  kind: "workspace" | "personal";
  name: string;
  workspace: { id: string; name: string };
  owner: { id: string; name: string } | null;
  createdBy: { id: string; name: string } | null;
  projectAccess: "all" | "selected";
  projects: { id: string; name: string }[];
  scopes: string[];
  effectiveScopes: string[];
  createdAt: string;
  expiresAt: string | null;
  lastUsedAt: string | null;
  revokedAt: string | null;
};
export type ApiKeyInput = Pick<
  ApiKey,
  "name" | "projectAccess" | "scopes" | "expiresAt"
> & {
  projects: string[];
  password?: string;
  code?: string;
};
export type ApiKeyCreated = { key: ApiKey; secret: string };
export function apiKeyActive(key: ApiKey) {
  return (
    !key.revokedAt && (!key.expiresAt || Date.parse(key.expiresAt) > Date.now())
  );
}
