import { useParams } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import {
  AdminBadge,
  EmailBadge,
  MfaBadge,
} from "@/components/app/admin/admin-list";
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
import { Text } from "@/components/ui/text";
import { adminPaths } from "@/lib/api/admin";
import { useAdminKey } from "@/lib/admin";
import { formatDate, formatDateTime } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { adminRoutes } from "@/lib/admin-routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import type { AdminUserDetail } from "@/types/admin";
// One account: its sign-in state and its workspaces. Opening it is recorded
// in the audit trail (admin.user.viewed).
export default function AdminUserShow() {
  const id = parsePublicId("usr", useParams().user);
  if (id === null) return <UserNotFound />;
  return <UserContent key={id} id={id} />;
}
function UserContent({ id }: { id: string }) {
  const key = useAdminKey();
  const user = useAdminResource(adminPaths.users.show(id));
  if (user.missing) return <UserNotFound />;
  return (
    <>
      <Link
        href={adminRoutes.users.index(key)}
        className="inline-flex items-center gap-2 text-sm/6 text-zinc-500 dark:text-zinc-400"
      >
        <ChevronLeftIcon className="size-4 fill-zinc-400 dark:fill-zinc-500" />
        Users
      </Link>
      {user.error && (
        <Notice variant="error" className="mt-6" onRetry={user.refresh}>
          {user.error}
        </Notice>
      )}
      {user.data ? (
        <UserDetails user={user.data} />
      ) : (
        user.loading && <ListSkeleton label="Loading user" rows={1} />
      )}
    </>
  );
}
function UserDetails({ user }: { user: AdminUserDetail }) {
  const key = useAdminKey();
  return (
    <>
      <div className="mt-4 flex flex-wrap items-end justify-between gap-4 lg:mt-8">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <Heading className="truncate">{user.name}</Heading>
            <MfaBadge enabled={user.mfaEnabled} />
            <EmailBadge verifiedAt={user.emailVerifiedAt} />
            {user.admin && <AdminBadge />}
          </div>
          <Text className="mt-1">{user.email}</Text>
        </div>
        <Button
          outline
          href={adminRoutes.events.index(key, {
            account: user.id,
            window: "all",
          })}
        >
          Activity
        </Button>
      </div>
      <div className="mt-12">
        <Subheading>Account</Subheading>
        <DescriptionList className="mt-4">
          <DescriptionTerm>ID</DescriptionTerm>
          <DescriptionDetails className="font-mono">
            {user.id}
          </DescriptionDetails>
          <DescriptionTerm>Joined</DescriptionTerm>
          <DescriptionDetails>
            {formatDate(user.createdAt, "long")}
          </DescriptionDetails>
          <DescriptionTerm>Last sign-in</DescriptionTerm>
          <DescriptionDetails>
            {user.lastSignedInAt
              ? formatDateTime(user.lastSignedInAt)
              : "Never"}
          </DescriptionDetails>
          <DescriptionTerm>Two-factor authentication</DescriptionTerm>
          <DescriptionDetails>
            {user.mfaEnabled ? "On" : "Off"}
          </DescriptionDetails>
          <DescriptionTerm>Signed-in sessions</DescriptionTerm>
          <DescriptionDetails>{user.sessionCount}</DescriptionDetails>
        </DescriptionList>
      </div>
      <Memberships user={user} />
    </>
  );
}
function Memberships({ user }: { user: AdminUserDetail }) {
  const key = useAdminKey();
  return (
    <div className="mt-12">
      <Subheading>Workspaces</Subheading>
      {user.memberships.length === 0 ? (
        <Text className="mt-4">Not in any workspace.</Text>
      ) : (
        <Table className="mt-4">
          <TableHead>
            <TableRow>
              <TableHeader>Workspace</TableHeader>
              <TableHeader>Role</TableHeader>
              <TableHeader className="max-sm:hidden">Joined</TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {user.memberships.map(({ workspace, role, addedAt }) => (
              <TableRow
                key={workspace.id}
                href={adminRoutes.workspaces.show(key, workspace.id)}
                title={`Open ${workspace.name}`}
              >
                <TableCell className="font-medium">{workspace.name}</TableCell>
                <TableCell>
                  <RoleBadge role={role} />
                </TableCell>
                <TableCell className="text-zinc-500 max-sm:hidden">
                  {formatDate(addedAt)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
function UserNotFound() {
  const key = useAdminKey();
  return (
    <EmptyState
      title="This user has wandered off."
      description="The account was deleted, or the link is mistyped."
      action={
        <Button href={adminRoutes.users.index(key)}>Back to users</Button>
      }
    />
  );
}
