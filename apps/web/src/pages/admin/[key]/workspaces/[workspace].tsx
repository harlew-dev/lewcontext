import { useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import {
  AdminTableHead,
  type AdminColumn,
} from "@/components/app/admin/admin-list";
import { ProjectStatusBadge } from "@/components/app/projects/project-display";
import {
  indexQuery,
  projectParams,
  projectQuery,
} from "@/components/app/projects/project-query";
import { StatusField } from "@/components/app/projects/status-field";
import { RoleBadge } from "@/components/app/workspaces/role-badge";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Link } from "@/components/ui/link";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import {
  DangerAction,
  SettingsSection,
} from "@/components/ui/settings-section";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { toast } from "@/components/ui/toast";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { formatDate } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { url } from "@/lib/pagination";
import { adminRoutes } from "@/lib/admin-routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import type { AdminWorkspaceDetail } from "@/types/admin";
import type { Project, ProjectQuery, ProjectSortField } from "@/types/projects";
// One workspace: its counts, its members and its projects, and for
// operators Revoke pending invitations. Opening it is recorded in the audit
// trail (admin.workspace.viewed, and admin.project.listed for the
// projects).
export default function AdminWorkspaceShow() {
  const id = parsePublicId("wsp", useParams().workspace);
  if (id === null) return <WorkspaceNotFound />;
  return <WorkspaceContent key={id} id={id} />;
}
function WorkspaceContent({ id }: { id: string }) {
  const key = useAdminKey();
  const workspace = useAdminResource(adminPaths.workspaces.show(id));
  if (workspace.missing) return <WorkspaceNotFound />;
  return (
    <>
      <Link
        href={adminRoutes.workspaces.index(key)}
        className="inline-flex items-center gap-2 text-sm/6 text-zinc-500 dark:text-zinc-400"
      >
        <ChevronLeftIcon className="size-4 fill-zinc-400 dark:fill-zinc-500" />
        Workspaces
      </Link>
      {workspace.error && (
        <Notice variant="error" className="mt-6" onRetry={workspace.refresh}>
          {workspace.error}
        </Notice>
      )}
      {workspace.data ? (
        <WorkspaceDetails
          workspace={workspace.data}
          onChanged={workspace.refresh}
        />
      ) : (
        workspace.loading && <ListSkeleton label="Loading workspace" rows={1} />
      )}
    </>
  );
}
function WorkspaceDetails({
  workspace,
  onChanged,
}: {
  workspace: AdminWorkspaceDetail;
  onChanged: () => void;
}) {
  const key = useAdminKey();
  return (
    <>
      <div className="mt-4 flex flex-wrap items-end justify-between gap-4 lg:mt-8">
        <Heading className="min-w-0 truncate">{workspace.name}</Heading>
        <Button
          outline
          href={adminRoutes.events.index(key, {
            workspace: workspace.id,
            window: "all",
          })}
        >
          Activity
        </Button>
      </div>
      <div className="mt-12">
        <Subheading>Workspace</Subheading>
        <DescriptionList className="mt-4">
          <DescriptionTerm>ID</DescriptionTerm>
          <DescriptionDetails className="font-mono">
            {workspace.id}
          </DescriptionDetails>
          <DescriptionTerm>Created</DescriptionTerm>
          <DescriptionDetails>
            {formatDate(workspace.createdAt, "long")}
          </DescriptionDetails>
          <DescriptionTerm>Projects</DescriptionTerm>
          <DescriptionDetails>{workspace.projectCount}</DescriptionDetails>
          <DescriptionTerm>Members</DescriptionTerm>
          <DescriptionDetails>{workspace.memberCount}</DescriptionDetails>
          <DescriptionTerm>Pending invitations</DescriptionTerm>
          <DescriptionDetails>
            {workspace.pendingInvitations}
          </DescriptionDetails>
        </DescriptionList>
      </div>
      <Members workspace={workspace} />
      <Projects workspace={workspace} />
      <Actions workspace={workspace} onChanged={onChanged} />
    </>
  );
}
function Members({ workspace }: { workspace: AdminWorkspaceDetail }) {
  const key = useAdminKey();
  return (
    <div className="mt-12">
      <Subheading>Members</Subheading>
      <Table className="mt-4">
        <TableHead>
          <TableRow>
            <TableHeader>Name</TableHeader>
            <TableHeader className="max-md:hidden">Email</TableHeader>
            <TableHeader>Role</TableHeader>
            <TableHeader className="max-sm:hidden">Joined</TableHeader>
          </TableRow>
        </TableHead>
        <TableBody>
          {workspace.members.map((member) => (
            <TableRow
              key={member.id}
              href={adminRoutes.users.show(key, member.id)}
              title={`Open ${member.name}`}
            >
              <TableCell className="font-medium">{member.name}</TableCell>
              <TableCell className="text-zinc-500 max-md:hidden">
                {member.email}
              </TableCell>
              <TableCell>
                <RoleBadge role={member.role} />
              </TableCell>
              <TableCell className="text-zinc-500 max-sm:hidden">
                {formatDate(member.addedAt)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
const projectColumns: AdminColumn<ProjectSortField>[] = [
  { label: "Name", field: "name", first: "ascending" },
  { label: "Status", field: "status", first: "ascending" },
  { label: "Description", className: "max-lg:hidden" },
  {
    label: "Updated",
    field: "updated",
    first: "descending",
    className: "max-sm:hidden",
  },
];
// The workspace's projects, names and descriptions included, searched,
// filtered and sorted as the app's list is, in this page's URL. Each load is
// recorded (admin.project.listed). There's no project page, so rows don't
// link.
function Projects({ workspace }: { workspace: AdminWorkspaceDetail }) {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const query = projectQuery(search);
  const list = useAdminResource(
    adminPaths.projects(workspace.id).index(indexQuery(query)),
  );
  const pathFor = (q: ProjectQuery) =>
    url(adminRoutes.workspaces.show(key, workspace.id), projectParams(q));
  const go = (q: Partial<ProjectQuery>, replace = false) =>
    void navigate(pathFor({ ...query, page: 1, ...q }), {
      replace,
      preventScrollReset: true,
    });
  const filtered = query.q !== "" || query.status !== "all";
  return (
    <div className="mt-12">
      <Subheading>Projects</Subheading>
      <div className="mt-4 flex max-w-xl gap-4">
        <div className="flex-1">
          <SearchField
            label="Search projects"
            value={query.q}
            onSearch={(q) => go({ q }, true)}
          />
        </div>
        <div className="w-44">
          <StatusField
            value={query.status}
            onChange={(status) => go({ status })}
          />
        </div>
      </div>
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <PaginatedTable
        className="mt-4"
        page={list.data}
        loading={list.loading}
        label="Projects"
        head={
          <AdminTableHead
            columns={projectColumns}
            sort={query.sort}
            sortHref={(sort) => pathFor({ ...query, page: 1, sort })}
          />
        }
        row={(project) => <ProjectRow project={project} />}
        rowKey={(project) => project.id}
        href={(page) => pathFor({ ...query, page })}
        empty={
          <EmptyState
            title={filtered ? "No projects match" : "No projects"}
            description={
              filtered
                ? "Try another search or status."
                : "This workspace has no projects."
            }
          />
        }
      />
    </div>
  );
}
function ProjectRow({ project }: { project: Project }) {
  return (
    <TableRow>
      <TableCell className="font-medium">{project.name}</TableCell>
      <TableCell>
        <ProjectStatusBadge status={project.status} />
      </TableCell>
      <TableCell className="max-w-sm truncate text-zinc-500 max-lg:hidden">
        {project.description || "–"}
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {formatDate(project.updatedAt)}
      </TableCell>
    </TableRow>
  );
}
function WorkspaceNotFound() {
  const key = useAdminKey();
  return (
    <EmptyState
      title="This workspace has wandered off."
      description="It was deleted, or the link is mistyped."
      action={
        <Button href={adminRoutes.workspaces.index(key)}>
          Back to workspaces
        </Button>
      }
    />
  );
}

// Revoking every pending invitation stops an invitation-spam run without
// touching the workspace's data: operators only, behind a fresh code and a
// reason.
function Actions({
  workspace,
  onChanged,
}: {
  workspace: AdminWorkspaceDetail;
  onChanged: () => void;
}) {
  const api = useAdminApi();
  const [open, setOpen] = useState(false);
  const opened = useOpenCount(open);
  if (!adminCan(useAdminSession().role, "operator")) return null;
  const pending = workspace.pendingInvitations;
  return (
    <div className="mt-12">
      <SettingsSection
        title="Actions"
        description="Takes effect at once, needs a code from your authenticator and a reason, and is recorded with both."
        variant="danger"
      >
        <DangerAction
          title="Revoke pending invitations"
          description={
            pending === 0
              ? "This workspace has no pending invitations."
              : `Revokes all ${pending} so their links stop working. Members and projects stay as they are.`
          }
          action={
            <Button
              color="red"
              disabled={pending === 0}
              disabledReason="No pending invitations"
              onClick={() => setOpen(true)}
            >
              Revoke pending invitations
            </Button>
          }
        />
      </SettingsSection>
      <AdminActionDialog
        key={opened}
        open={open}
        onClose={() => setOpen(false)}
        title={`Revoke ${workspace.name}’s pending invitations?`}
        description={`All ${pending} pending invitations are revoked, and their links stop working. Its members can invite people again.`}
        confirm="Revoke invitations"
        variant="danger"
        action={(reason) =>
          api.workspaceInvitations(workspace.id).destroy(reason)
        }
        onDone={() => {
          toast.success(`${workspace.name}’s pending invitations are revoked.`);
          onChanged();
        }}
      />
    </div>
  );
}
