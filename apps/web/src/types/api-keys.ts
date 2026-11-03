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
// The actions a key may take, in words. The API's scope names are
// project.<action> after its routes; people read List, View, Create, Update
// and Delete. In this order wherever they are listed.
export const scopeLabels: Record<string, string> = {
  "project.index": "List",
  "project.show": "View",
  "project.store": "Create",
  "project.update": "Update",
  "project.destroy": "Delete",
};
// A key's actions as words, in the order above; an unknown scope shows as it is.
export function scopeNames(scopes: string[]): string[] {
  const known = Object.keys(scopeLabels).filter((s) => scopes.includes(s));
  const other = scopes.filter((s) => !(s in scopeLabels));
  return [...known, ...other].map((s) => scopeLabels[s] ?? s);
}
export type ApiKeyStatus = "active" | "expired" | "revoked";
// Revoked wins over expired: revoking an expired key is no longer possible,
// but a key revoked before it expired reads as revoked.
export function apiKeyStatus(key: ApiKey): ApiKeyStatus {
  if (key.revokedAt) return "revoked";
  return apiKeyActive(key) ? "active" : "expired";
}
