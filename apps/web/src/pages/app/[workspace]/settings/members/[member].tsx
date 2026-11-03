import { Fragment, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useSWRConfig } from "swr";
import { EllipsisHorizontalIcon } from "@heroicons/react/16/solid";
import { KeyList } from "@/components/app/api-keys/key-list";
import { EventLog } from "@/components/app/events/event-log";
import {
  RemoveDialog,
  RoleDialog,
  manageable,
} from "@/components/app/members/member-dialogs";
import { RoleBadge } from "@/components/app/workspaces/role-badge";
import { Badge } from "@/components/ui/badge";
import { BackLink } from "@/components/ui/back-link";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { PageTabs, useTab } from "@/components/ui/page-tabs";
import { toast } from "@/components/ui/toast";
import { paths } from "@/lib/api";
import { formatDate } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useOpenCount } from "@/lib/use-open-count";
import { useResource } from "@/lib/use-resource";
import { useRefreshWorkspaces, useWorkspace } from "@/lib/workspace";
import type { MemberDetail } from "@/types/members";
import { can } from "@/types/workspaces";
// One member: Overview, their personal API keys here and the changes they
// made or were the subject of, with Change role and Remove from workspace in
// a ⋯ menu. Owners and admins only (workspace.member.show). Someone who left
// still has a page while they appear in the workspace's log: their history
// is the point of the audit, so it stays readable, with no actions.
const tabs = [
  { id: "overview", label: "Overview" },
  { id: "api-keys", label: "API Keys" },
  { id: "logs", label: "Logs" },
] as const;
export default function MemberShow() {
  const workspace = useWorkspace();
  const id = parsePublicId("usr", useParams().member);
  if (!can(workspace, "workspace.member.show"))
    return (
      <Notice variant="error">
        Only owners and admins can see this workspace’s members.
      </Notice>
    );
  return <Member key={`${workspace.id}-${id}`} id={id ?? "missing"} />;
}
type Dialog = "role" | "remove" | null;
function Member({ id }: { id: string }) {
  const workspace = useWorkspace();
  const you = id === useSession().user.id;
  const navigate = useNavigate();
  const { mutate } = useSWRConfig();
  const refreshWorkspaces = useRefreshWorkspaces();
  const member = useResource(paths.members(workspace.id).show(id));
  const tab = useTab(tabs);
  const [dialog, setDialog] = useState<Dialog>(null);
  const opened = useOpenCount(dialog !== null);
  const back = routes.members.index(workspace.id);
  if (member.missing)
    return (
      <div className="mx-auto max-w-4xl">
        <BackLink href={back}>Members</BackLink>
        <EmptyState
          title="Member not found"
          description="They aren’t in this workspace, and haven’t appeared in its log."
        />
      </div>
    );
  const data = member.data;
  const { canChange, canRemove } = data
    ? manageable(workspace, data, you)
    : { canChange: false, canRemove: false };
  // Whatever the page shows is stale after a change; Remove leaves it.
  const refreshLists = () =>
    void mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        (paths.members(workspace.id).matches(key[0]) ||
          paths.events(workspace.id).matches(key[0])),
    );
  return (
    <div className="mx-auto max-w-4xl">
      <BackLink href={back}>Members</BackLink>
      {member.error && (
        <Notice variant="error" className="mt-4" onRetry={member.refresh}>
          {member.error}
        </Notice>
      )}
      {data ? (
        <>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              <Heading className="truncate">{data.name}</Heading>
              {data.current && data.role ? (
                <RoleBadge role={data.role} />
              ) : (
                <Badge>No longer a member</Badge>
              )}
              {you && <Badge>You</Badge>}
            </div>
            {(canChange || canRemove) && (
              <Dropdown>
                <DropdownButton plain aria-label={`Actions for ${data.name}`}>
                  <EllipsisHorizontalIcon />
                </DropdownButton>
                <DropdownMenu anchor="bottom end">
                  {canChange && (
                    <DropdownItem onClick={() => setDialog("role")}>
                      <DropdownLabel>Change role</DropdownLabel>
                    </DropdownItem>
                  )}
                  {canRemove && (
                    <DropdownItem
                      variant="danger"
                      onClick={() => setDialog("remove")}
                    >
                      <DropdownLabel>Remove from workspace</DropdownLabel>
                    </DropdownItem>
                  )}
                </DropdownMenu>
              </Dropdown>
            )}
          </div>
          <PageTabs label="Member" tabs={tabs} current={tab} />
          <div className="mt-8">
            {tab === "overview" && <Overview member={data} />}
            {tab === "api-keys" && (
              <KeyList
                workspace={workspace}
                kind="personal"
                owner={data.id}
                title="Personal API keys"
                allowCreate={false}
              />
            )}
            {tab === "logs" && <MemberLog member={data} />}
          </div>
          {data.current && data.role && (
            <>
              <RoleDialog
                key={`role-${opened}`}
                open={dialog === "role"}
                member={{ id: data.id, name: data.name, role: data.role }}
                onClose={() => setDialog(null)}
                onDone={(message) => {
                  toast.success(message);
                  setDialog(null);
                  member.refresh();
                  refreshLists();
                  refreshWorkspaces(workspace.id);
                }}
              />
              <RemoveDialog
                key={`remove-${opened}`}
                open={dialog === "remove"}
                member={{ id: data.id, name: data.name, role: data.role }}
                onClose={() => setDialog(null)}
                onDone={(message) => {
                  toast.success(message);
                  refreshLists();
                  refreshWorkspaces(workspace.id);
                  void navigate(back);
                }}
              />
            </>
          )}
        </>
      ) : (
        !member.error && (
          <div className="mt-8">
            <ListSkeleton label="Loading member" rows={4} />
          </div>
        )
      )}
    </div>
  );
}
function Overview({ member }: { member: MemberDetail }) {
  const rows: { label: string; value: string }[] = [
    { label: "Name", value: member.name },
    ...(member.email ? [{ label: "Email", value: member.email }] : []),
    {
      label: "Status",
      value:
        member.current && member.role
          ? `Member since ${member.addedAt ? formatDate(member.addedAt, "long") : "unknown"}`
          : "No longer a member",
    },
    ...(member.role
      ? [
          {
            label: "Role",
            value: member.role[0].toUpperCase() + member.role.slice(1),
          },
        ]
      : []),
    { label: "Personal API keys", value: String(member.keys) },
    { label: "Changes made", value: String(member.changes) },
  ];
  return (
    <DescriptionList>
      {rows.map((row) => (
        <Fragment key={row.label}>
          <DescriptionTerm>{row.label}</DescriptionTerm>
          <DescriptionDetails className="break-words">
            {row.value}
          </DescriptionDetails>
        </Fragment>
      ))}
    </DescriptionList>
  );
}
// What they did, and what was done to them (a role change, their removal).
function MemberLog({ member }: { member: MemberDetail }) {
  const workspace = useWorkspace();
  const projects = useResource(
    paths.projects(workspace.id).index({ perPage: 100, sort: "name" }),
  );
  return (
    <EventLog
      label="Member logs"
      pathFor={(query) =>
        paths.events(workspace.id).index({ ...query, account: member.id })
      }
      links={{
        workspace: workspace.id,
        members: can(workspace, "workspace.member.show"),
        events: can(workspace, "workspace.event.show"),
      }}
      choices={{
        people: null,
        projects: (projects.data?.data ?? []).map(({ id, name }) => ({
          id,
          name,
        })),
      }}
    />
  );
}
