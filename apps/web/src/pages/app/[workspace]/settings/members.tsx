import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useSWRConfig } from "swr";
import { EllipsisHorizontalIcon } from "@heroicons/react/16/solid";
import { RoleBadge, roleLabels } from "@/components/app/workspaces/role-badge";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ErrorNotice, Notice } from "@/components/ui/feedback";
import { ErrorMessage, Field, Label } from "@/components/ui/fieldset";
import { Subheading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { ApiError, errorMessage, paths, useApi } from "@/lib/api";
import { formatDate, initials } from "@/lib/format";
import { cleanSearch, page, param, url } from "@/lib/pagination";
import { routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useAction } from "@/lib/use-action";
import { useOpenCount } from "@/lib/use-open-count";
import { useResource } from "@/lib/use-resource";
import { useRefreshWorkspaces, useWorkspace } from "@/lib/workspace";
import {
  expired,
  type Invitation,
  type InvitedRole,
} from "@/types/invitations";
import type { Member, MemberQuery } from "@/types/members";
import { can, type Role } from "@/types/workspaces";
// Workspace settings → Members. Only owners and admins may list members; for
// anyone else the page explains that without asking the API.
export default function MemberIndex() {
  const workspace = useWorkspace();
  return can(workspace, "workspace.member.index") ? (
    <MemberList key={workspace.id} />
  ) : (
    <Notice variant="error">
      Only owners and admins can see this workspace’s members.
    </Notice>
  );
}
// The page state in the URL (?q=&page=); values the API would reject fall back.
function memberQuery(search: URLSearchParams): MemberQuery {
  return { page: page(search), q: cleanSearch(param(search, "q")) };
}
function membersPath(workspace: string, { q, page }: MemberQuery) {
  return url(routes.members.index(workspace), { q, page });
}
// A change to one member: picking a new role, or removing them.
type MemberAction = { kind: "role" | "remove"; member: Member };
function MemberList() {
  const [search] = useSearchParams();
  const goTo = useNavigate();
  const workspace = useWorkspace();
  const query = memberQuery(search);
  const list = useResource(paths.members(workspace.id).index(query));
  const refreshMembers = useRefreshMembers();
  const refreshWorkspaces = useRefreshWorkspaces();
  const [action, setAction] = useState<MemberAction | null>(null);
  const [inviting, setInviting] = useState(false);
  // Each opening of a dialog starts afresh.
  const invites = useOpenCount(inviting);
  const actions = useOpenCount(action !== null);
  const refreshInvitations = useRefreshInvitations();
  // The last action stays rendered while its dialog animates out.
  const [shown, setShown] = useState<MemberAction | null>(null);
  if (action && action !== shown) setShown(action);
  function done(message: string) {
    toast.success(message);
    setAction(null);
    refreshMembers();
    // Whether you may leave depends on who else is an owner.
    refreshWorkspaces(workspace.id);
  }
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <Subheading>Members</Subheading>
          <Text className="mt-1">Everyone with access to this workspace.</Text>
        </div>
        <div className="flex w-full items-end gap-3 sm:w-auto">
          {can(workspace, "workspace.invitation.store") && (
            <Button className="shrink-0" onClick={() => setInviting(true)}>
              Invite
            </Button>
          )}
          <div className="w-full sm:w-64">
            <SearchField
              label="Search members"
              value={query.q}
              onSearch={(q) =>
                void goTo(membersPath(workspace.id, { q, page: 1 }), {
                  replace: true,
                  preventScrollReset: true,
                })
              }
            />
          </div>
        </div>
      </div>
      {can(workspace, "workspace.invitation.index") && <PendingInvitations />}
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <PaginatedTable
        className="mt-8"
        page={list.data}
        loading={list.loading}
        label="Members"
        head={<MemberTableHead />}
        row={(member) => <MemberRow member={member} onAction={setAction} />}
        rowKey={(member) => member.id}
        href={(page) => membersPath(workspace.id, { ...query, page })}
        empty={
          <EmptyState
            title="No matching members"
            description="Try another name or email address."
          />
        }
      />
      {shown && (
        <>
          <RoleDialog
            key={`role-${actions}`}
            open={action?.kind === "role"}
            member={shown.member}
            onClose={() => setAction(null)}
            onDone={done}
          />
          <RemoveDialog
            key={`remove-${actions}`}
            open={action?.kind === "remove"}
            member={shown.member}
            onClose={() => setAction(null)}
            onDone={done}
          />
        </>
      )}
      <InviteDialog
        key={invites}
        open={inviting}
        onClose={() => setInviting(false)}
        onDone={(email) => {
          toast.success(`Invitation sent to ${email}.`);
          setInviting(false);
          refreshInvitations();
        }}
      />
    </>
  );
}
// Revalidates every cached list whose path `matches` (every page and
// search). SWR keys are [path, userId].
function useRefreshLists(matches: (key: string) => boolean) {
  const { mutate } = useSWRConfig();
  return () =>
    void mutate(
      (key) =>
        Array.isArray(key) && typeof key[0] === "string" && matches(key[0]),
    );
}
function useRefreshMembers() {
  return useRefreshLists(paths.members(useWorkspace().id).matches);
}
function useRefreshInvitations() {
  return useRefreshLists(paths.invitations(useWorkspace().id).matches);
}
function MemberTableHead() {
  return (
    <TableRow>
      <TableHeader>Member</TableHeader>
      <TableHeader>Role</TableHeader>
      <TableHeader className="max-sm:hidden">Added</TableHeader>
      <TableHeader>
        <span className="sr-only">Actions</span>
      </TableHeader>
    </TableRow>
  );
}
function MemberRow({
  member,
  onAction,
}: {
  member: Member;
  onAction: (action: MemberAction) => void;
}) {
  const workspace = useWorkspace();
  const you = member.id === useSession().user.id;
  // Never yourself (that's Leave workspace), and only roles you may manage.
  const manageable = !you && workspace.assignableRoles.includes(member.role);
  const canChange = manageable && can(workspace, "workspace.member.update");
  const canRemove = manageable && can(workspace, "workspace.member.destroy");
  return (
    <TableRow>
      <TableCell>
        <div className="flex max-w-xs items-center gap-3 sm:max-w-md">
          <Avatar
            initials={initials(member.name)}
            className="size-8 bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300"
          />
          <span className="min-w-0">
            <span className="flex items-center gap-2">
              <span className="truncate font-medium">{member.name}</span>
              {you && <Badge>You</Badge>}
            </span>
            <span className="block truncate text-zinc-500 dark:text-zinc-400">
              {member.email}
            </span>
          </span>
        </div>
      </TableCell>
      <TableCell>
        <RoleBadge role={member.role} />
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {formatDate(member.addedAt)}
      </TableCell>
      <TableCell className="w-0 text-right">
        {(canChange || canRemove) && (
          <Dropdown>
            <DropdownButton plain aria-label={`Actions for ${member.name}`}>
              <EllipsisHorizontalIcon />
            </DropdownButton>
            <DropdownMenu anchor="bottom end">
              {canChange && (
                <DropdownItem
                  onClick={() => onAction({ kind: "role", member })}
                >
                  <DropdownLabel>Change role</DropdownLabel>
                </DropdownItem>
              )}
              {canRemove && (
                <DropdownItem
                  variant="danger"
                  onClick={() => onAction({ kind: "remove", member })}
                >
                  <DropdownLabel>Remove from workspace</DropdownLabel>
                </DropdownItem>
              )}
            </DropdownMenu>
          </Dropdown>
        )}
      </TableCell>
    </TableRow>
  );
}
type MemberDialogProps = {
  open: boolean;
  member: Member;
  onClose: () => void;
  onDone: (message: string) => void;
};
// Offers only the roles you may give (the workspace's assignableRoles).
function RoleDialog({ open, member, onClose, onDone }: MemberDialogProps) {
  const api = useApi();
  const workspace = useWorkspace();
  const [role, setRole] = useState<Role>(member.role);
  const save = useAction((role: Role) =>
    api.members(workspace.id).update(member.id, { role }),
  );
  async function submit(event: FormEvent) {
    event.preventDefault();
    if ((await save.run(role)).ok)
      onDone(
        `${member.name} is now ${article(role)} ${roleLabels[role].toLowerCase()}.`,
      );
  }
  const close = () => !save.busy && onClose();
  return (
    <Dialog open={open} onClose={close} size="md">
      <form onSubmit={submit}>
        <DialogTitle>Change {member.name}’s role</DialogTitle>
        <DialogDescription>
          Their access changes as soon as you save.
        </DialogDescription>
        <DialogBody>
          <ErrorNotice error={save.error} className="mb-6" />
          <Field disabled={save.loading}>
            <Label>Role</Label>
            <Listbox value={role} onChange={setRole}>
              {workspace.assignableRoles.map((option) => (
                <ListboxOption key={option} value={option}>
                  <ListboxLabel>{roleLabels[option]}</ListboxLabel>
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
        </DialogBody>
        <DialogActions>
          <Button plain onClick={close} disabled={save.loading}>
            Cancel
          </Button>
          <Button
            type="submit"
            loading={save.loading}
            disabled={role === member.role}
            disabledReason="No changes made"
          >
            Save role
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
function article(role: Role) {
  return role === "owner" || role === "admin" ? "an" : "a";
}
function RemoveDialog({ open, member, onClose, onDone }: MemberDialogProps) {
  const api = useApi();
  const workspace = useWorkspace();
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title={`Remove ${member.name} from ${workspace.name}?`}
      description="They lose access at once. Their projects stay."
      confirm="Remove"
      variant="danger"
      onConfirm={() => api.members(workspace.id).destroy(member.id)}
      onConfirmed={() =>
        onDone(`${member.name} was removed from ${workspace.name}.`)
      }
    />
  );
}
// Invitations not yet accepted, above the members, while there are any. A
// row reads "Sending…" until the email has gone, so the list polls while one
// does; expired ones stay listed (for 30 days) so whoever sent one can see why
// it wasn't used.
function PendingInvitations() {
  const workspace = useWorkspace();
  const list = useResource(
    paths.invitations(workspace.id).index({ perPage: 100 }),
    undefined,
    { refreshInterval: (page) => (page?.data.some(sending) ? 3000 : 0) },
  );
  const refresh = useRefreshInvitations();
  const [revoking, setRevoking] = useState<Invitation | null>(null);
  const revokes = useOpenCount(revoking !== null);
  // The last one stays rendered while its dialog animates out.
  const [shown, setShown] = useState<Invitation | null>(null);
  if (revoking && revoking !== shown) setShown(revoking);
  if (list.error && !list.data)
    return (
      <Notice variant="error" className="mt-6" onRetry={list.refresh}>
        {list.error}
      </Notice>
    );
  if (!list.data || list.data.data.length === 0) return null;
  return (
    <section aria-labelledby="pending-invitations" className="mt-8">
      <Subheading id="pending-invitations">Pending invitations</Subheading>
      <Text className="mt-1">
        Each link works for 7 days, and only for the email it was sent to.
      </Text>
      {list.error && (
        <Notice variant="error" className="mt-4" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <Table className="mt-4">
        <TableHead>
          <TableRow>
            <TableHeader>Email</TableHeader>
            <TableHeader>Role</TableHeader>
            <TableHeader className="max-sm:hidden">Invited by</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader>
              <span className="sr-only">Actions</span>
            </TableHeader>
          </TableRow>
        </TableHead>
        <TableBody>
          {list.data.data.map((invitation) => (
            <InvitationRow
              key={invitation.id}
              invitation={invitation}
              onRevoke={setRevoking}
            />
          ))}
        </TableBody>
      </Table>
      {shown && (
        <RevokeDialog
          key={revokes}
          open={revoking !== null}
          invitation={shown}
          onClose={() => setRevoking(null)}
          onDone={() => {
            toast.success(`The invitation to ${shown.email} was revoked.`);
            setRevoking(null);
            refresh();
          }}
        />
      )}
    </section>
  );
}
// Still being emailed: not sent yet, and not expired.
function sending(invitation: Invitation) {
  return invitation.sentAt === null && !expired(invitation);
}
function InvitationRow({
  invitation,
  onRevoke,
}: {
  invitation: Invitation;
  onRevoke: (invitation: Invitation) => void;
}) {
  const workspace = useWorkspace();
  // Only invitations to a role you may give, as for removing a member.
  const canRevoke =
    can(workspace, "workspace.invitation.destroy") &&
    workspace.assignableRoles.includes(invitation.role);
  return (
    <TableRow>
      <TableCell className="max-w-48 truncate font-medium sm:max-w-xs">
        {invitation.email}
      </TableCell>
      <TableCell>
        <RoleBadge role={invitation.role} />
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {invitation.invitedBy?.name ?? "A former member"}
      </TableCell>
      <TableCell className="text-zinc-500">
        {expired(invitation) ? (
          <Badge>Expired</Badge>
        ) : invitation.sentAt === null ? (
          "Sending…"
        ) : (
          `Sent ${formatDate(invitation.sentAt)}`
        )}
      </TableCell>
      <TableCell className="w-0 text-right">
        {canRevoke && (
          <Dropdown>
            <DropdownButton
              plain
              aria-label={`Actions for the invitation to ${invitation.email}`}
            >
              <EllipsisHorizontalIcon />
            </DropdownButton>
            <DropdownMenu anchor="bottom end">
              <DropdownItem
                variant="danger"
                onClick={() => onRevoke(invitation)}
              >
                <DropdownLabel>Revoke invitation</DropdownLabel>
              </DropdownItem>
            </DropdownMenu>
          </Dropdown>
        )}
      </TableCell>
    </TableRow>
  );
}
function RevokeDialog({
  open,
  invitation,
  onClose,
  onDone,
}: {
  open: boolean;
  invitation: Invitation;
  onClose: () => void;
  onDone: () => void;
}) {
  const api = useApi();
  const workspace = useWorkspace();
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title={`Revoke the invitation to ${invitation.email}?`}
      description="The link in their email stops working. You can invite them again later."
      confirm="Revoke invitation"
      variant="danger"
      onConfirm={() => api.invitations(workspace.id).destroy(invitation.id)}
      onConfirmed={onDone}
    />
  );
}
// Invite by email, as one of the roles you may give (never owner: invite as
// admin, then promote). The email goes out a moment later.
function InviteDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  onDone: (email: string) => void;
}) {
  const api = useApi();
  const workspace = useWorkspace();
  const roles = workspace.assignableRoles.filter(
    (role): role is InvitedRole => role !== "owner",
  );
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<InvitedRole>(
    roles.includes("member") ? "member" : (roles[0] ?? "viewer"),
  );
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const send = useAction(
    (email: string) => api.invitations(workspace.id).store({ email, role }),
    {
      // A refused email or role shows under its field.
      onError: (e) => {
        if (
          e instanceof ApiError &&
          (e.field === "email" || e.field === "role")
        ) {
          setFieldErrors({ [e.field]: errorMessage(e) });
          return true;
        }
        return false;
      },
    },
  );
  async function submit(event: FormEvent) {
    event.preventDefault();
    setFieldErrors({});
    const address = email.trim();
    if ((await send.run(address)).ok) onDone(address);
  }
  const close = () => !send.busy && onClose();
  return (
    <Dialog open={open} onClose={close} size="md">
      <form onSubmit={submit}>
        <DialogTitle>Invite someone to {workspace.name}</DialogTitle>
        <DialogDescription>
          They’ll get an email with a link. Only that email address can accept
          it, within 7 days.
        </DialogDescription>
        <DialogBody className="space-y-6">
          <ErrorNotice error={send.error} />
          <Field disabled={send.loading}>
            <Label>Email</Label>
            <Input
              type="email"
              name="email"
              autoComplete="off"
              required
              maxLength={254}
              value={email}
              invalid={Boolean(fieldErrors.email)}
              onChange={(e) => setEmail(e.currentTarget.value)}
            />
            {fieldErrors.email && (
              <ErrorMessage role="alert">{fieldErrors.email}</ErrorMessage>
            )}
          </Field>
          <Field disabled={send.loading}>
            <Label>Role</Label>
            <Listbox value={role} onChange={setRole}>
              {roles.map((option) => (
                <ListboxOption key={option} value={option}>
                  <ListboxLabel>{roleLabels[option]}</ListboxLabel>
                </ListboxOption>
              ))}
            </Listbox>
            {fieldErrors.role && (
              <ErrorMessage role="alert">{fieldErrors.role}</ErrorMessage>
            )}
          </Field>
        </DialogBody>
        <DialogActions>
          <Button plain onClick={close} disabled={send.loading}>
            Cancel
          </Button>
          <Button type="submit" loading={send.loading}>
            Send invitation
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
