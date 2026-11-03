import { EventLog } from "@/components/app/events/event-log";
import { Notice } from "@/components/ui/feedback";
import { Subheading } from "@/components/ui/heading";
import { Text } from "@/components/ui/text";
import { paths } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";
// Workspace settings → Logs: the changes made in this workspace, newest
// first, for owners and admins (workspace.event.index). Its filters and
// cursor are in the URL; see EventLog. Nothing here is recorded.
export default function EventIndex() {
  const workspace = useWorkspace();
  return can(workspace, "workspace.event.index") ? (
    <Logs key={workspace.id} />
  ) : (
    <Notice variant="error">
      Only owners and admins can see this workspace’s logs.
    </Notice>
  );
}
function Logs() {
  const workspace = useWorkspace();
  const members = useResource(
    paths.members(workspace.id).index({ perPage: 100 }),
  );
  const projects = useResource(
    paths.projects(workspace.id).index({ perPage: 100, sort: "name" }),
  );
  return (
    <>
      <div className="mb-6">
        <Subheading>Logs</Subheading>
        <Text className="mt-1">
          Changes made in this workspace. Times are UTC and changes are kept for
          90 days.
        </Text>
      </div>
      <EventLog
        label="Logs"
        pathFor={(query) => paths.events(workspace.id).index(query)}
        links={{
          workspace: workspace.id,
          members: can(workspace, "workspace.member.show"),
          events: can(workspace, "workspace.event.show"),
        }}
        choices={{
          people: (members.data?.data ?? []).map(({ id, name }) => ({
            id,
            name,
          })),
          projects: (projects.data?.data ?? []).map(({ id, name }) => ({
            id,
            name,
          })),
        }}
      />
    </>
  );
}
