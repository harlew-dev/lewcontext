import { Badge } from "@/components/ui/badge";
import {
  apiKeyStatus,
  scopeNames,
  type ApiKey,
  type ApiKeyStatus,
} from "@/types/api-keys";
// How a key reads in lists and on its page: its status as a badge, and its
// access as one grouped badge.
const statusLabels: Record<ApiKeyStatus, string> = {
  active: "Active",
  expired: "Expired",
  revoked: "Revoked",
};
export function KeyStatus({ apiKey }: { apiKey: ApiKey }) {
  const status = apiKeyStatus(apiKey);
  return (
    <Badge color={status === "active" ? "green" : "zinc"}>
      {statusLabels[status]}
    </Badge>
  );
}
// "Selected projects: List, View, Create": the projects it reaches and what
// it may do there. A personal key's actions are what it was saved with; the
// full text is the title when the badge is shortened.
export function accessText(key: ApiKey): string {
  const where =
    key.projectAccess === "all"
      ? "All projects"
      : key.projects.length > 0
        ? "Selected projects"
        : "No selected projects remain";
  const actions = scopeNames(key.scopes);
  return actions.length > 0 &&
    (key.projectAccess === "all" || key.projects.length > 0)
    ? `${where}: ${actions.join(", ")}`
    : where;
}
export function KeyAccess({ apiKey }: { apiKey: ApiKey }) {
  const text = accessText(apiKey);
  return (
    <span title={text} className="block max-w-full">
      <Badge className="max-w-full">
        <span className="truncate">{text}</span>
      </Badge>
    </span>
  );
}
