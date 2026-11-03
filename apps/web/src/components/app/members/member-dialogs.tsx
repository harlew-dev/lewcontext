import { useState, type FormEvent } from "react";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { ErrorNotice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Button } from "@/components/ui/button";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { useApi } from "@/lib/api";
import { useAction } from "@/lib/use-action";
import { useWorkspace } from "@/lib/workspace";
import { can, type Role, type Workspace } from "@/types/workspaces";
import { roleLabels } from "@/components/app/workspaces/role-badge";
// Changing a member's role and removing them, from the Members list's ⋯ menu
// and from a member's own page.

// Who the actions are for: the list's member or a member's page.
export type ManagedMember = { id: string; name: string; role: Role };
// What the viewer may do to a member: never themselves (that is Leave
// workspace), and only roles they may manage (the workspace's
// assignableRoles), each with its permission.
export function manageable(
  workspace: Workspace,
  member: { role: Role | "" },
  you: boolean,
) {
  const ok =
    !you &&
    member.role !== "" &&
    workspace.assignableRoles.includes(member.role);
  return {
    canChange: ok && can(workspace, "workspace.member.update"),
    canRemove: ok && can(workspace, "workspace.member.destroy"),
  };
}
type MemberDialogProps = {
  open: boolean;
  member: ManagedMember;
  onClose: () => void;
  onDone: (message: string) => void;
};
// Offers only the roles you may give (the workspace's assignableRoles).
export function RoleDialog({
  open,
  member,
  onClose,
  onDone,
}: MemberDialogProps) {
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
export function RemoveDialog({
  open,
  member,
  onClose,
  onDone,
}: MemberDialogProps) {
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
