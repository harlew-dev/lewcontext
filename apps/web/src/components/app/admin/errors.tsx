import { Badge } from "@/components/ui/badge";
import type { ErrorGroup, Release } from "@/types/admin";
import { formatEventTime } from "./event-filters";
// What the Errors page and one group's page share: a group's source in
// words (a browser's is user-reported: its text is whatever that browser
// sent, shown as text and never as a link), and whether it's new in the
// release now running.
export const sourceLabels: Record<ErrorGroup["source"], string> = {
  request: "Request",
  panic: "Panic",
  worker: "Event worker",
  background: "Background job",
  client: "User-reported",
};
// The release now running, in words.
export function releaseText(release: Release | null): string {
  if (!release) return "No server started in the last 7 days.";
  const version = release.version || "an unknown version";
  return `The current release, ${version}, started ${formatEventTime(release.startedAt)} UTC. Groups first seen since are marked New.`;
}
// A group first seen since the current release started: did it break?
export function newInRelease(group: ErrorGroup, release: Release | null) {
  return release !== null && group.firstSeenAt >= release.startedAt;
}
// The badges beside a group: its source (a browser's in words that say
// whose text it is), new in this release, resolved.
export function GroupBadges({
  group,
  release,
}: {
  group: ErrorGroup;
  release: Release | null;
}) {
  return (
    <span className="flex flex-wrap items-center gap-2">
      <Badge color={group.source === "client" ? "purple" : "zinc"}>
        {sourceLabels[group.source]}
      </Badge>
      {newInRelease(group, release) && <Badge color="pink">New</Badge>}
      {group.resolvedAt && <Badge color="zinc">Resolved</Badge>}
    </span>
  );
}
