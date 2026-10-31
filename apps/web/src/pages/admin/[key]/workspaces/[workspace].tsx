import { useParams } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { paths } from "@/lib/api";
import { useAdminKey } from "@/lib/admin";
import { formatDate } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { routes } from "@/lib/routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import type { AdminWorkspaceDetail } from "@/types/admin";
// One workspace: its counts and its members. Opening it is recorded in the
// audit trail (admin.workspace.viewed).
export default function AdminWorkspaceShow() {
  const id = parsePublicId("wsp", useParams().workspace);
  if (id === null) return <WorkspaceNotFound />;
  return <WorkspaceContent key={id} id={id} />;
}
function WorkspaceContent({ id }: { id: string }) {
  const key = useAdminKey();
  const workspace = useAdminResource(paths.admin.workspaces.show(id));
  if (workspace.missing) return <WorkspaceNotFound />;
  return (
    <>
      <Link
        href={routes.admin.workspaces.index(key)}
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
        <WorkspaceDetails workspace={workspace.data} />
      ) : (
        workspace.loading && <ListSkeleton label="Loading workspace" rows={1} />
      )}
    </>
  );
}
function WorkspaceDetails({ workspace }: { workspace: AdminWorkspaceDetail }) {
  return (
    <>
      <Heading className="mt-4 truncate lg:mt-8">{workspace.name}</Heading>
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
        </DescriptionList>
      </div>
      <Members workspace={workspace} />
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
              href={routes.admin.users.show(key, member.id)}
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
function WorkspaceNotFound() {
  const key = useAdminKey();
  return (
    <EmptyState
      title="This workspace has wandered off."
      description="It was deleted, or the link is mistyped."
      action={
        <Button href={routes.admin.workspaces.index(key)}>
          Back to workspaces
        </Button>
      }
    />
  );
}
