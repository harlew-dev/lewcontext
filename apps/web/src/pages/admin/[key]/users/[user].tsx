import { useState } from "react";
import { useParams } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import {
  AdminBadge,
  EmailBadge,
  MfaBadge,
  SuspendedBadge,
} from "@/components/app/admin/admin-list";
import { Checkbox } from "@/components/ui/checkbox";
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
import { Text, TextLink } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { formatDate, formatDateTime } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { adminRoutes } from "@/lib/admin-routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import type { AdminUserDetail } from "@/types/admin";
// One account: its sign-in state and its workspaces, and for operators the
// account-and-access actions (plans/features/admin/actions.md), each
// behind a fresh code and a reason. Opening it is recorded in the audit
// trail (admin.user.viewed); so is each reload after an action.
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
        <UserDetails user={user.data} onChanged={user.refresh} />
      ) : (
        user.loading && <ListSkeleton label="Loading user" rows={1} />
      )}
    </>
  );
}
function UserDetails({
  user,
  onChanged,
}: {
  user: AdminUserDetail;
  onChanged: () => void;
}) {
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
            {user.suspended && <SuspendedBadge />}
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
      {user.suspension && (
        <Notice variant="warning" className="mt-8">
          Suspended {formatDateTime(user.suspension.suspendedAt)} by{" "}
          {user.suspension.suspendedBy?.name ?? "the command line"}: “
          {user.suspension.reason}”. It can’t sign in, and gets no reset link;
          its workspaces work for everyone else.
        </Notice>
      )}
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
      <Actions user={user} onChanged={onChanged} />
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

// The account-and-access actions, for operators only (the API refuses a
// viewer anyway), never on an admin's account (yours included): only the
// command line acts on an admin, apart from signing one out on the Admins
// page.
function Actions({
  user,
  onChanged,
}: {
  user: AdminUserDetail;
  onChanged: () => void;
}) {
  const key = useAdminKey();
  if (!adminCan(useAdminSession().role, "operator")) return null;
  if (user.admin)
    return (
      <div className="mt-12">
        <Subheading>Actions</Subheading>
        <Text className="mt-4">
          This account is an admin: suspending it or resetting its two-factor
          authentication is only done from the command line. To sign it out, use
          the <TextLink href={adminRoutes.admins.index(key)}>Admins</TextLink>{" "}
          page.
        </Text>
      </div>
    );
  return (
    <div className="mt-12">
      <SettingsSection
        title="Actions"
        description="Each takes effect at once, needs a code from your authenticator and a reason, and is recorded with both."
        variant="danger"
      >
        <SignOutAction user={user} onChanged={onChanged} />
        {user.mfaEnabled && (
          <ResetMfaAction user={user} onChanged={onChanged} />
        )}
        {user.suspended ? (
          <UnsuspendAction user={user} onChanged={onChanged} />
        ) : (
          <SuspendAction user={user} onChanged={onChanged} />
        )}
      </SettingsSection>
    </div>
  );
}
type ActionProps = { user: AdminUserDetail; onChanged: () => void };
// Opening state for an action's dialog: each opening starts empty.
function useActionDialog() {
  const [open, setOpen] = useState(false);
  const opened = useOpenCount(open);
  return {
    open,
    opened,
    show: () => setOpen(true),
    close: () => setOpen(false),
  };
}
function SignOutAction({ user, onChanged }: ActionProps) {
  const api = useAdminApi();
  const dialog = useActionDialog();
  return (
    <>
      <DangerAction
        title="Sign out everywhere"
        description={`Ends every session of this account (${user.sessionCount} now). They can sign in again with their password.`}
        action={
          <Button color="red" onClick={dialog.show}>
            Sign out everywhere
          </Button>
        }
      />
      <AdminActionDialog
        key={dialog.opened}
        open={dialog.open}
        onClose={dialog.close}
        title={`Sign ${user.name} out everywhere?`}
        description="Every session of this account ends at once, on every device. Nothing else changes: they can sign in again with their password."
        confirm="Sign out everywhere"
        variant="danger"
        action={(reason) => api.userSessions(user.id).destroy(reason)}
        onDone={() => {
          toast.success(`${user.name} is signed out everywhere.`);
          onChanged();
        }}
      />
    </>
  );
}
// Turning MFA off is a way around someone's second factor: the dialog asks
// to confirm who is asking, outside the app, as well as why.
function ResetMfaAction({ user, onChanged }: ActionProps) {
  const api = useAdminApi();
  const dialog = useActionDialog();
  const [confirmed, setConfirmed] = useState(false);
  return (
    <>
      <DangerAction
        title="Reset two-factor authentication"
        description="For someone who lost their authenticator and recovery codes: turns it off and signs them out, so the password alone signs in."
        action={
          <Button
            color="red"
            onClick={() => {
              setConfirmed(false);
              dialog.show();
            }}
          >
            Reset two-factor authentication
          </Button>
        }
      />
      <AdminActionDialog
        key={dialog.opened}
        open={dialog.open}
        onClose={dialog.close}
        title={`Reset ${user.name}’s two-factor authentication?`}
        description="Their authenticator and recovery codes stop working and every session ends. Until they set it up again, their password alone signs in."
        confirm="Reset two-factor authentication"
        variant="danger"
        ready={confirmed}
        action={(reason) => api.userMfa(user.id).destroy({ reason, confirmed })}
        onDone={() => {
          toast.success(`Two-factor authentication is off for ${user.name}.`);
          onChanged();
        }}
      >
        <Checkbox
          name="confirmed"
          checked={confirmed}
          onChange={setConfirmed}
          description="On a call, or from the address on the account: never only from a message that asks for it."
        >
          I’ve confirmed who is asking, outside the app
        </Checkbox>
      </AdminActionDialog>
    </>
  );
}
function SuspendAction({ user, onChanged }: ActionProps) {
  const api = useAdminApi();
  const dialog = useActionDialog();
  const [revokeInvitations, setRevokeInvitations] = useState(true);
  return (
    <>
      <DangerAction
        title="Suspend account"
        description="Signs them out and refuses their sign-ins until unsuspended. Their workspaces keep working for everyone else."
        action={
          <Button
            color="red"
            onClick={() => {
              setRevokeInvitations(true);
              dialog.show();
            }}
          >
            Suspend account
          </Button>
        }
      />
      <AdminActionDialog
        key={dialog.opened}
        open={dialog.open}
        onClose={dialog.close}
        title={`Suspend ${user.name}?`}
        description="Every session ends at once. Their sign-ins are refused, with a message saying the account is suspended, and they get no reset link. Their workspaces and projects stay as they are."
        confirm="Suspend account"
        variant="danger"
        action={(reason) =>
          api.userSuspension(user.id).store({ reason, revokeInvitations })
        }
        onDone={() => {
          toast.success(`${user.name} is suspended.`);
          onChanged();
        }}
      >
        <Checkbox
          name="revokeInvitations"
          checked={revokeInvitations}
          onChange={setRevokeInvitations}
          description="Their links stop working at once."
        >
          Also revoke invitations they’ve sent
        </Checkbox>
      </AdminActionDialog>
    </>
  );
}
// Not destructive: an outline button.
function UnsuspendAction({ user, onChanged }: ActionProps) {
  const api = useAdminApi();
  const dialog = useActionDialog();
  return (
    <>
      <DangerAction
        title="Unsuspend account"
        description="Lets them sign in again. Invitations revoked when they were suspended stay revoked."
        action={
          <Button outline onClick={dialog.show}>
            Unsuspend account
          </Button>
        }
      />
      <AdminActionDialog
        key={dialog.opened}
        open={dialog.open}
        onClose={dialog.close}
        title={`Unsuspend ${user.name}?`}
        description="They can sign in again with their password (and code), and ask for a reset link."
        confirm="Unsuspend account"
        action={(reason) => api.userSuspension(user.id).destroy(reason)}
        onDone={() => {
          toast.success(`${user.name} can sign in again.`);
          onChanged();
        }}
      />
    </>
  );
}
