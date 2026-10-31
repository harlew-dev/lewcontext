import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useSWRConfig } from "swr";
import { EllipsisHorizontalIcon } from "@heroicons/react/16/solid";
import { RoleBadge, roleLabels } from "@/components/app/workspaces/role-badge";
import { SettingsTabs } from "@/components/app/workspaces/settings-tabs";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Divider } from "@/components/ui/divider";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ErrorNotice, Notice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading, Subheading } from "@/components/ui/heading";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import { TableCell, TableHeader, TableRow } from "@/components/ui/table";
import { Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { paths, useApi } from "@/lib/api";
import { formatDate, initials } from "@/lib/format";
import { cleanSearch, page, param, url } from "@/lib/pagination";
import { routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useAction } from "@/lib/use-action";
import { useResource } from "@/lib/use-resource";
import { useRefreshWorkspaces, useWorkspace } from "@/lib/workspace";
import type { Member, MemberQuery } from "@/types/members";
import { can, type Role } from "@/types/workspaces";
// Workspace settings → Members. Only owners and admins may list members; for
// anyone else the page explains that without asking the API.
export default function WorkspaceMembers() {
  const workspace = useWorkspace();
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Workspace settings</Heading>
      <SettingsTabs current="members" />
      <Divider className="my-10 mt-6" />
      {can(workspace, "workspace.member.index") ? (
        <MemberIndex key={workspace.id} />
      ) : (
        <Notice variant="error">
          Only owners and admins can see this workspace’s members.
        </Notice>
      )}
    </div>
  );
}
// The page state in the URL (?q=&page=); values the API would reject fall back.
function memberQuery(search: URLSearchParams): MemberQuery {
  return { page: page(search), q: cleanSearch(param(search, "q")) };
}
function membersPath(workspace: string, { q, page }: MemberQuery) {
  return url(routes.members(workspace), { q, page });
}
// A change to one member: picking a new role, or removing them.
type MemberAction = { kind: "role" | "remove"; member: Member };
function MemberIndex() {
  const [search] = useSearchParams();
  const goTo = useNavigate();
  const workspace = useWorkspace();
  const query = memberQuery(search);
  const list = useResource(paths.members(workspace.id).index(query));
  const refreshMembers = useRefreshMembers();
  const refreshWorkspaces = useRefreshWorkspaces();
  const [action, setAction] = useState<MemberAction | null>(null);
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
          <Text className="mt-1">
            {list.data
              ? memberCount(list.data.pages.items, Boolean(query.q))
              : "Everyone with access to this workspace."}
          </Text>
        </div>
        <div className="w-full sm:max-w-xs">
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
            key={`role-${shown.member.id}`}
            open={action?.kind === "role"}
            member={shown.member}
            onClose={() => setAction(null)}
            onDone={done}
          />
          <RemoveDialog
            key={`remove-${shown.member.id}`}
            open={action?.kind === "remove"}
            member={shown.member}
            onClose={() => setAction(null)}
            onDone={done}
          />
        </>
      )}
    </>
  );
}
function memberCount(items: number, filtered: boolean) {
  const members = items === 1 ? "member" : "members";
  return filtered ? `${items} matching ${members}` : `${items} ${members}`;
}
// Revalidates every cached member list of this workspace (every page and
// search). SWR keys are [path, userId].
function useRefreshMembers() {
  const { mutate } = useSWRConfig();
  const members = paths.members(useWorkspace().id);
  return () =>
    void mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        members.matches(key[0]),
    );
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
  const remove = useAction(() => api.members(workspace.id).destroy(member.id));
  async function confirm() {
    if ((await remove.run()).ok)
      onDone(`${member.name} was removed from ${workspace.name}.`);
  }
  const close = () => !remove.busy && onClose();
  return (
    <Dialog open={open} onClose={close} size="md">
      <DialogTitle>
        Remove {member.name} from {workspace.name}?
      </DialogTitle>
      <DialogDescription>
        They lose access at once. Their projects stay.
      </DialogDescription>
      <ErrorNotice error={remove.error} className="mt-6" />
      <DialogActions>
        <Button plain onClick={close} disabled={remove.loading}>
          Cancel
        </Button>
        <Button color="red" loading={remove.loading} onClick={confirm}>
          Remove
        </Button>
      </DialogActions>
    </Dialog>
  );
}
