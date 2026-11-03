import { Fragment } from "react";
import {
  EllipsisHorizontalIcon,
  NoSymbolIcon,
  PencilSquareIcon,
} from "@heroicons/react/16/solid";
import { EventLog } from "@/components/app/events/event-log";
import { RequestId } from "@/components/app/events/event-details";
import { MemberLink } from "@/components/app/members/member-link";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { PageTabs, useTab } from "@/components/ui/page-tabs";
import { Text } from "@/components/ui/text";
import { paths } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { useSession } from "@/lib/session";
import { useResource } from "@/lib/use-resource";
import { apiKeyActive, scopeNames, type ApiKey } from "@/types/api-keys";
import { can, type Workspace } from "@/types/workspaces";
import { useKeyActions } from "./key-actions";
import { KeyStatus } from "./key-display";
// One API key's page, for a workspace's owners and admins (`workspace`) or
// for the account's own personal key (none): Overview and Logs, with Edit and
// Revoke in a ⋯ menu where they apply. Never a secret: the plaintext exists
// only in the creation response.
const tabs = [
  { id: "overview", label: "Overview" },
  { id: "logs", label: "Logs" },
] as const;
export function KeyPage({
  id,
  workspace,
}: {
  id: string;
  workspace?: Workspace;
}) {
  const key = useResource(paths.apiKeys(workspace?.id).show(id));
  const tab = useTab(tabs);
  const actions = useKeyActions(workspace, () => void key.refresh());
  if (key.missing)
    return (
      <div className="mx-auto max-w-4xl">
        <EmptyState
          title="API key not found"
          description="It may belong to someone else, or be older than the 90 days ended keys are kept."
        />
      </div>
    );
  const data = key.data;
  // Workspace keys are edited by owners and admins; a member's personal key
  // only by its owner, from their account.
  const editable =
    data && apiKeyActive(data) && (!workspace || data.kind === "workspace");
  return (
    <div className="mx-auto max-w-4xl">
      {key.error && (
        <Notice variant="error" className="mt-4" onRetry={key.refresh}>
          {key.error}
        </Notice>
      )}
      {data ? (
        <>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              <Heading className="truncate">{data.name}</Heading>
              <KeyStatus apiKey={data} />
            </div>
            {apiKeyActive(data) && (
              <Dropdown>
                <DropdownButton plain aria-label={`Actions for ${data.name}`}>
                  <EllipsisHorizontalIcon data-slot="icon" />
                </DropdownButton>
                <DropdownMenu anchor="bottom end">
                  {editable && (
                    <DropdownItem onClick={() => actions.edit(data)}>
                      <PencilSquareIcon />
                      Edit
                    </DropdownItem>
                  )}
                  <DropdownItem
                    variant="danger"
                    onClick={() => actions.revoke(data)}
                  >
                    <NoSymbolIcon />
                    Revoke
                  </DropdownItem>
                </DropdownMenu>
              </Dropdown>
            )}
          </div>
          <Text className="mt-1">
            {data.kind === "personal" ? "Personal" : "Workspace"} API key in{" "}
            {data.workspace.name}.
          </Text>
          <PageTabs label="API key" tabs={tabs} current={tab} />
          <div className="mt-8">
            {tab === "overview" ? (
              <Overview apiKey={data} workspace={workspace} />
            ) : (
              <KeyLog apiKey={data} workspace={workspace} />
            )}
          </div>
        </>
      ) : (
        !key.error && (
          <div className="mt-8">
            <ListSkeleton label="Loading API key" rows={4} />
          </div>
        )
      )}
      {actions.dialogs}
    </div>
  );
}
function Overview({
  apiKey: key,
  workspace,
}: {
  apiKey: ApiKey;
  workspace?: Workspace;
}) {
  const person = key.owner ?? key.createdBy;
  const effective = scopeNames(key.effectiveScopes);
  const rows: { label: string; value: React.ReactNode }[] = [
    { label: "Key ID", value: <RequestId id={key.id} /> },
    {
      label: key.owner ? "Owner" : "Created by",
      value: (
        <MemberLink
          workspace={
            workspace && can(workspace, "workspace.member.show")
              ? workspace.id
              : null
          }
          member={person}
        />
      ),
    },
    {
      label: "Project access",
      value:
        key.projectAccess === "all"
          ? "All projects"
          : key.projects.length > 0
            ? key.projects.map((p) => p.name).join(", ")
            : "No selected projects remain",
    },
    { label: "Actions", value: scopeNames(key.scopes).join(", ") || "None" },
    ...(key.kind === "personal"
      ? [
          {
            label: "Allowed now",
            value:
              effective.join(", ") || "None under the owner’s current role",
          },
        ]
      : []),
    { label: "Created", value: formatDateTime(key.createdAt) },
    {
      label: "Expires",
      value: key.expiresAt ? formatDateTime(key.expiresAt) : "Never",
    },
    {
      label: "Last used",
      value: key.lastUsedAt ? formatDateTime(key.lastUsedAt) : "Never",
    },
    ...(key.revokedAt
      ? [{ label: "Revoked", value: formatDateTime(key.revokedAt) }]
      : []),
  ];
  return (
    <>
      {key.kind === "personal" &&
        key.scopes.some((scope) => !key.effectiveScopes.includes(scope)) && (
          <Notice variant="info" className="mb-6">
            Some actions are unavailable under the owner’s current role.
          </Notice>
        )}
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
    </>
  );
}
// The key's own events: when it was created, changed or revoked, and the
// changes it made. What it only reads isn't recorded; Last used shows that.
function KeyLog({
  apiKey: key,
  workspace,
}: {
  apiKey: ApiKey;
  workspace?: Workspace;
}) {
  const session = useSession();
  return (
    <>
      <Text className="mb-6">
        When this key was created, changed or revoked, and the changes it made.
        Reads aren’t recorded: Last used shows when it was last used at all.
      </Text>
      <EventLog
        label="API key logs"
        pathFor={(query) =>
          workspace
            ? paths.events(workspace.id).index({ ...query, key: key.id })
            : paths.apiKeyEvents(key.id).index(query)
        }
        links={
          workspace
            ? {
                workspace: workspace.id,
                members: can(workspace, "workspace.member.show"),
                events: can(workspace, "workspace.event.show"),
                apiKeys: can(workspace, "workspace.api_key.show"),
                viewer: session.user.id,
              }
            : null
        }
        choices={{ people: null, projects: null }}
      />
    </>
  );
}
