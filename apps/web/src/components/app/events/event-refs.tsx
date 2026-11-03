import { TextLink } from "@/components/ui/text";
import { routes } from "@/lib/routes";
// What a workspace's log links: the workspace whose pages to open, which of
// them the viewer may, and the signed-in account's ID. Null where there is no
// workspace to open them in (a personal key's own log).
export type EventLinks = {
  workspace: string;
  members: boolean;
  events: boolean;
  apiKeys: boolean;
  // A personal key's page is its owner's: the log links it only for them.
  viewer: string;
};
// An API key's public ID, linking to its page where the viewer may open it: a
// workspace key for an owner or admin, a personal key only for its owner.
// Otherwise the ID stays text.
export function ApiKeyLink({
  id,
  personal = false,
  owner,
  links,
}: {
  id: string;
  personal?: boolean;
  owner?: string;
  links: EventLinks | null;
}) {
  const href = links
    ? personal
      ? links.viewer === owner
        ? routes.userApiKeys.show(id)
        : null
      : links.apiKeys
        ? routes.workspaceApiKeys.show(links.workspace, id)
        : null
    : null;
  if (!href) return <span>{id}</span>;
  return <TextLink href={href}>{id}</TextLink>;
}
// A project's public ID, linking to its page: every member may open one.
export function ProjectLink({
  id,
  links,
}: {
  id: string;
  links: EventLinks | null;
}) {
  if (!links) return <span>{id}</span>;
  return (
    <TextLink href={routes.projects.show(links.workspace, id)}>{id}</TextLink>
  );
}
// A member's public ID, linking to their page where the viewer may open it.
export function MemberIdLink({
  id,
  links,
}: {
  id: string;
  links: EventLinks | null;
}) {
  if (!links?.members) return <span>{id}</span>;
  return (
    <TextLink href={routes.members.show(links.workspace, id)}>{id}</TextLink>
  );
}
